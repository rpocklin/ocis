package svc

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"path"
	"time"

	cs3rpc "github.com/cs3org/go-cs3apis/cs3/rpc/v1beta1"
	storageprovider "github.com/cs3org/go-cs3apis/cs3/storage/provider/v1beta1"
	types "github.com/cs3org/go-cs3apis/cs3/types/v1beta1"
	"github.com/go-chi/render"
	libregraph "github.com/owncloud/libre-graph-api-go"
	"github.com/owncloud/ocis/graph/pkg/service/v0/errorcode"
)

// GetRootDriveChildren implements the Service interface.
func (g Graph) GetRootDriveChildren(w http.ResponseWriter, r *http.Request) {
	g.logger.Info().Msg("Calling GetRootDriveChildren")
	ctx := r.Context()

	client := g.GetGatewayClient()

	res, err := client.GetHome(ctx, &storageprovider.GetHomeRequest{})
	switch {
	case err != nil:
		g.logger.Error().Err(err).Msg("error sending get home grpc request")
		errorcode.ServiceNotAvailable.Render(w, r, http.StatusInternalServerError, err.Error())
		return
	case res.Status.Code != cs3rpc.Code_CODE_OK:
		if res.Status.Code == cs3rpc.Code_CODE_NOT_FOUND {
			errorcode.ItemNotFound.Render(w, r, http.StatusNotFound, res.Status.Message)
			return
		}
		g.logger.Error().Err(err).Msg("error sending get home grpc request")
		errorcode.GeneralException.Render(w, r, http.StatusInternalServerError, res.Status.Message)
		return
	}

	lRes, err := client.ListContainer(ctx, &storageprovider.ListContainerRequest{
		Ref: &storageprovider.Reference{
			Path: res.Path,
		},
	})
	switch {
	case err != nil:
		g.logger.Error().Err(err).Msg("error sending list container grpc request")
		errorcode.ServiceNotAvailable.Render(w, r, http.StatusInternalServerError, err.Error())
		return
	case res.Status.Code != cs3rpc.Code_CODE_OK:
		if res.Status.Code == cs3rpc.Code_CODE_NOT_FOUND {
			errorcode.ItemNotFound.Render(w, r, http.StatusNotFound, res.Status.Message)
			return
		}
		if res.Status.Code == cs3rpc.Code_CODE_PERMISSION_DENIED {
			// TODO check if we should return 404 to not disclose existing items
			errorcode.AccessDenied.Render(w, r, http.StatusForbidden, res.Status.Message)
			return
		}
		g.logger.Error().Err(err).Msg("error sending list container grpc request")
		errorcode.GeneralException.Render(w, r, http.StatusInternalServerError, res.Status.Message)
		return
	}

	files, err := formatDriveItems(lRes.Infos)
	if err != nil {
		g.logger.Error().Err(err).Msg("error encoding response as json")
		errorcode.GeneralException.Render(w, r, http.StatusInternalServerError, err.Error())
		return
	}

	render.Status(r, http.StatusOK)
	render.JSON(w, r, &listResponse{Value: files})
}

func (g Graph) getDriveItem(ctx context.Context, root *storageprovider.ResourceId) (*libregraph.DriveItem, error) {
	client := g.GetGatewayClient()

	ref := &storageprovider.Reference{
		ResourceId: root,
	}
	res, err := client.Stat(ctx, &storageprovider.StatRequest{Ref: ref})
	if err != nil {
		return nil, err
	}
	if res.Status.Code != cs3rpc.Code_CODE_OK {
		return nil, fmt.Errorf("could not stat %s: %s", ref, res.Status.Message)
	}
	return cs3ResourceToDriveItem(res.Info)
}

func formatDriveItems(mds []*storageprovider.ResourceInfo) ([]*libregraph.DriveItem, error) {
	responses := make([]*libregraph.DriveItem, 0, len(mds))
	for i := range mds {
		res, err := cs3ResourceToDriveItem(mds[i])
		if err != nil {
			return nil, err
		}
		responses = append(responses, res)
	}

	return responses, nil
}

func cs3TimestampToTime(t *types.Timestamp) time.Time {
	return time.Unix(int64(t.Seconds), int64(t.Nanos))
}

func cs3ResourceToDriveItem(res *storageprovider.ResourceInfo) (*libregraph.DriveItem, error) {
	size := new(int64)
	*size = int64(res.Size) // TODO lurking overflow: make size of libregraph drive item use uint64

	driveItem := &libregraph.DriveItem{
		Id:   &res.Id.OpaqueId,
		Size: size,
	}

	if name := path.Base(res.Path); name != "" {
		driveItem.Name = &name
	}
	if res.Etag != "" {
		driveItem.ETag = &res.Etag
	}
	if res.Mtime != nil {
		lastModified := cs3TimestampToTime(res.Mtime)
		driveItem.LastModifiedDateTime = &lastModified
	}
	if res.Type == storageprovider.ResourceType_RESOURCE_TYPE_FILE && res.MimeType != "" {
		// We cannot use a libregraph.File here because the openapi codegenerator autodetects 'File' as a go type ...
		driveItem.File = &libregraph.OpenGraphFile{
			MimeType: &res.MimeType,
		}
	}
	if res.Type == storageprovider.ResourceType_RESOURCE_TYPE_CONTAINER {
		driveItem.Folder = &libregraph.Folder{}
	}
	return driveItem, nil
}

func (g Graph) getPathForDriveItem(ctx context.Context, ID *storageprovider.ResourceId) (*string, error) {
	client := g.GetGatewayClient()
	var path *string
	res, err := client.GetPath(ctx, &storageprovider.GetPathRequest{ResourceId: ID})
	if err != nil {
		return nil, err
	}
	if res.Status.Code != cs3rpc.Code_CODE_OK {
		return nil, fmt.Errorf("could not stat %s: %s", ID, res.Status.Message)
	}
	path = &res.Path
	return path, err
}

// GetExtendedSpaceProperties reads properties from the opaque and transforms them into driveItems
func (g Graph) GetExtendedSpaceProperties(ctx context.Context, baseURL *url.URL, space *storageprovider.StorageSpace) []libregraph.DriveItem {
	var spaceItems []libregraph.DriveItem
	if space.Opaque == nil {
		return nil
	}
	metadata := space.Opaque.Map
	names := [2]string{SpaceImageSpecialFolderName, ReadmeSpecialFolderName}

	for _, itemName := range names {
		if itemID, ok := metadata[itemName]; ok {
			spaceItem := g.getSpecialDriveItem(ctx, string(itemID.Value), itemName, baseURL, space)
			if spaceItem != nil {
				spaceItems = append(spaceItems, *spaceItem)
			}
		}
	}
	return spaceItems
}

func (g Graph) getSpecialDriveItem(ctx context.Context, itemID string, itemName string, baseURL *url.URL, space *storageprovider.StorageSpace) *libregraph.DriveItem {
	var spaceItem *libregraph.DriveItem
	if itemID == "" {
		return nil
	}

	spaceItem, err := g.getDriveItem(ctx, &storageprovider.ResourceId{StorageId: space.Root.StorageId, OpaqueId: itemID})
	if err != nil {
		g.logger.Error().Err(err).Str("ID", itemID).Msg("Could not get readme Item")
		return nil
	}
	itemPath, err := g.getPathForDriveItem(ctx, &storageprovider.ResourceId{StorageId: space.Root.StorageId, OpaqueId: itemID})
	if err != nil {
		g.logger.Error().Err(err).Str("ID", itemID).Msg("Could not get readme path")
		return nil
	}
	spaceItem.SpecialFolder = &libregraph.SpecialFolder{Name: libregraph.PtrString(itemName)}
	spaceItem.WebDavUrl = libregraph.PtrString(baseURL.String() + path.Join(space.Root.OpaqueId, *itemPath))

	return spaceItem
}
