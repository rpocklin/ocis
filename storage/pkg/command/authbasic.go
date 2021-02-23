package command

import (
	"context"
	"os"
	"os/signal"
	"time"

	"github.com/cs3org/reva/pkg/auth"
	"github.com/cs3org/reva/pkg/auth/manager/registry"
	"github.com/micro/cli/v2"
	"github.com/oklog/run"
	"github.com/owncloud/ocis/storage/pkg/config"
	"github.com/owncloud/ocis/storage/pkg/flagset"
	"github.com/owncloud/ocis/storage/pkg/server/debug"
	"github.com/owncloud/ocis/storage/pkg/server/grpc"
	svc "github.com/owncloud/ocis/storage/pkg/service/v0"

	// register reva drivers
	_ "github.com/cs3org/reva/pkg/auth/manager/loader"
)

// AuthBasic is the entrypoint for the auth-basic command.
func AuthBasic(cfg *config.Config) *cli.Command {
	return &cli.Command{
		Name:  "auth-basic",
		Usage: "Start authprovider for basic auth",
		Flags: flagset.AuthBasicWithConfig(cfg),
		Action: func(c *cli.Context) error {
			logger := NewLogger(cfg)

			// TODO add tracing

			var (
				gr          = run.Group{}
				ctx, cancel = context.WithCancel(context.Background())
				//mtrcs       = metrics.New()
			)

			defer cancel()

			// first initialize a service implementation
			config := map[string]map[string]interface{}{
				"demo": {},
				"json": {
					"users": cfg.Reva.AuthProvider.JSON,
				},
				"ldap": {
					"hostname":      cfg.Reva.LDAP.Hostname,
					"port":          cfg.Reva.LDAP.Port,
					"base_dn":       cfg.Reva.LDAP.BaseDN,
					"loginfilter":   cfg.Reva.LDAP.LoginFilter,
					"bind_username": cfg.Reva.LDAP.BindDN,
					"bind_password": cfg.Reva.LDAP.BindPassword,
					"idp":           cfg.Reva.LDAP.IDP,
					"gatewaysvc":    cfg.Reva.Gateway.Endpoint,
					"schema": map[string]interface{}{
						"dn":          "dn",
						"uid":         cfg.Reva.LDAP.UserSchema.UID,
						"mail":        cfg.Reva.LDAP.UserSchema.Mail,
						"displayName": cfg.Reva.LDAP.UserSchema.DisplayName,
						"cn":          cfg.Reva.LDAP.UserSchema.CN,
					},
				},
				// TODO rest?
			}
			var authmgr auth.Manager
			var err error
			if f, ok := registry.NewFuncs[cfg.Reva.AuthProvider.Driver]; ok {
				authmgr, err = f(config[cfg.Reva.AuthProvider.Driver])
				if err != nil {
					logger.Fatal().Err(err).Str("driver", cfg.Reva.AuthProvider.Driver).Msg("could not initialize auth manager")
				}
			} else {
				logger.Fatal().Str("driver", cfg.Reva.AuthProvider.Driver).Msg("unknown auth manager")
			}
			handler, err := svc.NewAuthProvider(
				svc.Logger(logger),
				svc.Config(cfg),
				svc.AuthManager(authmgr),
			)
			if err != nil {
				logger.Fatal().Err(err).Msg("could not initialize service handler")
			}

			// configure and run the grpc server
			{

				service := grpc.NewAuthProvider(
					grpc.Logger(logger),
					grpc.Context(ctx),
					grpc.Config(cfg),
					grpc.Namespace(cfg.Reva.AuthProvider.Namespace),
					grpc.Name(cfg.Reva.AuthProvider.Name),
					grpc.Metadata(map[string]string{"type": "basic"}),
					// grpc.Metrics(metrics), // TODO metrics are part of the ocis-pkg grpc service, right?
					//grpc.Flags(flagset.RootWithConfig(config.New())),
					grpc.AuthProviderHandler(handler),
				)

				gr.Add(func() error {
					return service.Run()
				}, func(_ error) {
					cancel()
				})
			}

			// configure and run a http debug server for /readyz, /healthz, /metrics and /debug
			{
				server, err := debug.Server(
					debug.Logger(logger),
					debug.Config(cfg),
					debug.Name(c.Command.Name+"-debug"),
					debug.Addr(cfg.Reva.AuthBasic.DebugAddr),
				)

				if err != nil {
					logger.Info().
						Err(err).
						Str("transport", "debug").
						Msg("Failed to initialize server")

					return err
				}

				gr.Add(func() error {
					return server.ListenAndServe()
				}, func(_ error) {
					ctx, timeout := context.WithTimeout(ctx, 5*time.Second)

					defer timeout()
					defer cancel()

					if err := server.Shutdown(ctx); err != nil {
						logger.Info().
							Err(err).
							Str("transport", "debug").
							Msg("Failed to shutdown server")
					} else {
						logger.Info().
							Str("transport", "debug").
							Msg("Shutting down server")
					}
				})
			}

			// capture cli interrupts
			{
				stop := make(chan os.Signal, 1)

				gr.Add(func() error {
					signal.Notify(stop, os.Interrupt)

					<-stop

					return nil
				}, func(err error) {
					close(stop)
					cancel()
				})
			}

			// finally, bring it all up
			return gr.Run()
		},
	}
}
