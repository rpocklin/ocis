package svc

import (
	"encoding/xml"
	"net/http"
	"path/filepath"
	"strings"

	gatewayv1beta1 "github.com/cs3org/go-cs3apis/cs3/gateway/v1beta1"
	userv1beta1 "github.com/cs3org/go-cs3apis/cs3/identity/user/v1beta1"
	rpcv1beta1 "github.com/cs3org/go-cs3apis/cs3/rpc/v1beta1"
	"github.com/cs3org/reva/pkg/rgrpc/todo/pool"
	"github.com/cs3org/reva/pkg/storage/utils/templates"
	"github.com/go-chi/render"
	merrors "go-micro.dev/v4/errors"
	"google.golang.org/grpc/metadata"

	"github.com/owncloud/ocis/ocis-pkg/log"
	"github.com/owncloud/ocis/ocis-pkg/service/grpc"

	"github.com/go-chi/chi/v5"
	thumbnailsmsg "github.com/owncloud/ocis/protogen/gen/ocis/messages/thumbnails/v0"
	thumbnailssvc "github.com/owncloud/ocis/protogen/gen/ocis/services/thumbnails/v0"
	"github.com/owncloud/ocis/webdav/pkg/config"
	"github.com/owncloud/ocis/webdav/pkg/dav/requests"
)

const (
	TokenHeader = "X-Access-Token"
)

var (
	codesEnum = map[int]string{
		http.StatusBadRequest:       "Sabre\\DAV\\Exception\\BadRequest",
		http.StatusUnauthorized:     "Sabre\\DAV\\Exception\\NotAuthenticated",
		http.StatusNotFound:         "Sabre\\DAV\\Exception\\NotFound",
		http.StatusMethodNotAllowed: "Sabre\\DAV\\Exception\\MethodNotAllowed",
	}
)

// Service defines the extension handlers.
type Service interface {
	ServeHTTP(http.ResponseWriter, *http.Request)
	Thumbnail(http.ResponseWriter, *http.Request)
}

// NewService returns a service implementation for Service.
func NewService(opts ...Option) (Service, error) {
	options := newOptions(opts...)
	conf := options.Config

	m := chi.NewMux()
	m.Use(options.Middleware...)

	gwc, err := pool.GetGatewayServiceClient(conf.RevaGateway)
	if err != nil {
		return nil, err
	}

	svc := Webdav{
		config:           conf,
		log:              options.Logger,
		mux:              m,
		thumbnailsClient: thumbnailssvc.NewThumbnailService("com.owncloud.api.thumbnails", grpc.DefaultClient),
		revaClient:       gwc,
	}

	m.Route(options.Config.HTTP.Root, func(r chi.Router) {
		r.Get("/remote.php/dav/files/{user}/*", svc.Thumbnail)
		r.Get("/remote.php/dav/public-files/{token}/*", svc.PublicThumbnail)
		r.Head("/remote.php/dav/public-files/{token}/*", svc.PublicThumbnailHead)
	})

	return svc, nil
}

// Webdav defines implements the business logic for Service.
type Webdav struct {
	config           *config.Config
	log              log.Logger
	mux              *chi.Mux
	thumbnailsClient thumbnailssvc.ThumbnailService
	revaClient       gatewayv1beta1.GatewayAPIClient
}

// ServeHTTP implements the Service interface.
func (g Webdav) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	g.mux.ServeHTTP(w, r)
}

// Thumbnail implements the Service interface.
func (g Webdav) Thumbnail(w http.ResponseWriter, r *http.Request) {
	tr, err := requests.ParseThumbnailRequest(r)
	if err != nil {
		g.log.Error().Err(err).Msg("could not create Request")
		renderError(w, r, errBadRequest(err.Error()))
		return
	}

	t := r.Header.Get(TokenHeader)
	ctx := metadata.AppendToOutgoingContext(r.Context(), TokenHeader, t)
	userRes, err := g.revaClient.GetUserByClaim(ctx, &userv1beta1.GetUserByClaimRequest{
		Claim: "username",
		Value: tr.Username,
	})
	if err != nil || userRes.Status.Code != rpcv1beta1.Code_CODE_OK {
		g.log.Error().Err(err).Msg("could not get user")
		renderError(w, r, errInternalError("could not get user"))
		return
	}

	fullPath := filepath.Join(templates.WithUser(userRes.User, g.config.WebdavNamespace), tr.Filepath)
	rsp, err := g.thumbnailsClient.GetThumbnail(r.Context(), &thumbnailssvc.GetThumbnailRequest{
		Filepath:      strings.TrimLeft(tr.Filepath, "/"),
		ThumbnailType: extensionToThumbnailType(strings.TrimLeft(tr.Extension, ".")),
		Width:         tr.Width,
		Height:        tr.Height,
		Source: &thumbnailssvc.GetThumbnailRequest_Cs3Source{
			Cs3Source: &thumbnailsmsg.CS3Source{
				Path:          fullPath,
				Authorization: t,
			},
		},
	})
	if err != nil {
		e := merrors.Parse(err.Error())
		switch e.Code {
		case http.StatusNotFound:
			// StatusNotFound is expected for unsupported files
			renderError(w, r, errNotFound(notFoundMsg(tr.Filename)))
			return
		case http.StatusBadRequest:
			renderError(w, r, errBadRequest(err.Error()))
		default:
			renderError(w, r, errInternalError(err.Error()))
		}
		g.log.Error().Err(err).Msg("could not get thumbnail")
		return
	}

	if len(rsp.Thumbnail) == 0 {
		renderError(w, r, errNotFound(""))
		return
	}

	g.mustRender(w, r, newThumbnailResponse(rsp))
}

func (g Webdav) PublicThumbnail(w http.ResponseWriter, r *http.Request) {
	tr, err := requests.ParseThumbnailRequest(r)
	if err != nil {
		g.log.Error().Err(err).Msg("could not create Request")
		renderError(w, r, errBadRequest(err.Error()))
		return
	}

	rsp, err := g.thumbnailsClient.GetThumbnail(r.Context(), &thumbnailssvc.GetThumbnailRequest{
		Filepath:      strings.TrimLeft(tr.Filepath, "/"),
		ThumbnailType: extensionToThumbnailType(strings.TrimLeft(tr.Extension, ".")),
		Width:         tr.Width,
		Height:        tr.Height,
		Source: &thumbnailssvc.GetThumbnailRequest_WebdavSource{
			WebdavSource: &thumbnailsmsg.WebdavSource{
				Url:             g.config.OcisPublicURL + r.URL.RequestURI(),
				IsPublicLink:    true,
				PublicLinkToken: tr.PublicLinkToken,
			},
		},
	})
	if err != nil {
		e := merrors.Parse(err.Error())
		switch e.Code {
		case http.StatusNotFound:
			// StatusNotFound is expected for unsupported files
			renderError(w, r, errNotFound(notFoundMsg(tr.Filename)))
			return
		case http.StatusBadRequest:
			renderError(w, r, errBadRequest(err.Error()))
		default:
			renderError(w, r, errInternalError(err.Error()))
		}
		g.log.Error().Err(err).Msg("could not get thumbnail")
		return
	}

	if len(rsp.Thumbnail) == 0 {
		renderError(w, r, errNotFound(""))
		return
	}

	g.mustRender(w, r, newThumbnailResponse(rsp))
}

func (g Webdav) PublicThumbnailHead(w http.ResponseWriter, r *http.Request) {
	tr, err := requests.ParseThumbnailRequest(r)
	if err != nil {
		g.log.Error().Err(err).Msg("could not create Request")
		renderError(w, r, errBadRequest(err.Error()))
		return
	}

	rsp, err := g.thumbnailsClient.GetThumbnail(r.Context(), &thumbnailssvc.GetThumbnailRequest{
		Filepath:      strings.TrimLeft(tr.Filepath, "/"),
		ThumbnailType: extensionToThumbnailType(strings.TrimLeft(tr.Extension, ".")),
		Width:         tr.Width,
		Height:        tr.Height,
		Source: &thumbnailssvc.GetThumbnailRequest_WebdavSource{
			WebdavSource: &thumbnailsmsg.WebdavSource{
				Url:             g.config.OcisPublicURL + r.URL.RequestURI(),
				IsPublicLink:    true,
				PublicLinkToken: tr.PublicLinkToken,
			},
		},
	})
	if err != nil {
		e := merrors.Parse(err.Error())
		switch e.Code {
		case http.StatusNotFound:
			// StatusNotFound is expected for unsupported files
			renderError(w, r, errNotFound(notFoundMsg(tr.Filename)))
			return
		case http.StatusBadRequest:
			renderError(w, r, errBadRequest(err.Error()))
		default:
			renderError(w, r, errInternalError(err.Error()))
		}
		g.log.Error().Err(err).Msg("could not get thumbnail")
		return
	}

	if len(rsp.Thumbnail) == 0 {
		renderError(w, r, errNotFound(""))
		return
	}

	w.WriteHeader(http.StatusOK)
}

func extensionToThumbnailType(ext string) thumbnailssvc.GetThumbnailRequest_ThumbnailType {
	switch strings.ToUpper(ext) {
	case "GIF", "PNG":
		return thumbnailssvc.GetThumbnailRequest_PNG
	default:
		return thumbnailssvc.GetThumbnailRequest_JPG
	}
}

func (g Webdav) mustRender(w http.ResponseWriter, r *http.Request, renderer render.Renderer) {
	if err := render.Render(w, r, renderer); err != nil {
		g.log.Err(err).Msg("failed to write response")
	}
}

// http://www.webdav.org/specs/rfc4918.html#ELEMENT_error
type errResponse struct {
	HTTPStatusCode int      `json:"-" xml:"-"`
	XMLName        xml.Name `xml:"d:error"`
	Xmlnsd         string   `xml:"xmlns:d,attr"`
	Xmlnss         string   `xml:"xmlns:s,attr"`
	Exception      string   `xml:"s:exception"`
	Message        string   `xml:"s:message"`
	InnerXML       []byte   `xml:",innerxml"`
}

func newErrResponse(statusCode int, msg string) *errResponse {
	rsp := &errResponse{
		HTTPStatusCode: statusCode,
		Xmlnsd:         "DAV",
		Xmlnss:         "http://sabredav.org/ns",
		Exception:      codesEnum[statusCode],
	}
	if msg != "" {
		rsp.Message = msg
	}
	return rsp
}

func errInternalError(msg string) *errResponse {
	return newErrResponse(http.StatusInternalServerError, msg)
}

func errBadRequest(msg string) *errResponse {
	return newErrResponse(http.StatusBadRequest, msg)
}

func errNotFound(msg string) *errResponse {
	return newErrResponse(http.StatusNotFound, msg)
}

type thumbnailResponse struct {
	contentType string
	thumbnail   []byte
}

func (t *thumbnailResponse) Render(w http.ResponseWriter, _ *http.Request) error {
	w.WriteHeader(http.StatusOK)
	w.Header().Set("Content-Type", t.contentType)
	_, err := w.Write(t.thumbnail)
	return err
}

func newThumbnailResponse(rsp *thumbnailssvc.GetThumbnailResponse) *thumbnailResponse {
	return &thumbnailResponse{
		contentType: rsp.Mimetype,
		thumbnail:   rsp.Thumbnail,
	}
}

func renderError(w http.ResponseWriter, r *http.Request, err *errResponse) {
	render.Status(r, err.HTTPStatusCode)
	render.XML(w, r, err)
}

func notFoundMsg(name string) string {
	return "File with name " + name + " could not be located"
}
