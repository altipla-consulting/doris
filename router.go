package doris

import (
	"context"
	"net/http"
	"time"

	"connectrpc.com/connect/v2"
	"connectrpc.com/connect/v2/connecthttp"
	"connectrpc.com/connect/v2/connectproto"
	"github.com/altipla-consulting/errors"
	"github.com/altipla-consulting/sentry"
	"github.com/altipla-consulting/telemetry"
	"libs.altipla.consulting/routing"
)

type Router struct {
	*routing.Server
}

// PathPrefixHandlerHTTP registers a new HTTP handler for all the routes under the specified prefix.
func (r *Router) PathPrefixHandlerHTTP(path string, handler http.Handler) {
	r.PathPrefixHandler(path, routing.NewHandlerFromHTTP(stdMiddlewares(handler)))
}

// Handler sends all request to the standard HTTP handler.
func (r *Router) Handler(handler http.Handler) {
	r.PathPrefixHandlerHTTP("", stdMiddlewares(handler))
}

// Handle implements the standard http interface.
func (r *Router) Handle(pattern string, handler http.Handler) {
	r.PathPrefixHandler(pattern, routing.NewHandlerFromHTTP(stdMiddlewares(handler)))
}

// MountFn registers one or more Connect services on a server.
type MountFn func(server *connect.Server)

// Mount registers and serves a new API on its own Connect server.
// The standard Doris interceptors always run before the supplied interceptors,
// which apply only to the services registered by fn.
func (r *Router) MountAPI(fn MountFn, interceptors ...connect.ServerInterceptor) {
	server := connect.NewServer(append(ServerInterceptors(), interceptors...)...)
	fn(server)

	jsonCodec := connectproto.NewJSONCodec()
	jsonCodec.MarshalOptions.EmitUnpopulated = true

	connecthttp.Mount(r, server,
		connecthttp.WithCodecs(connectproto.NewBinaryCodec(), jsonCodec),
		connecthttp.WithReadMaxBytes(0),
	)
}

func stdMiddlewares(handler http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Timeout.
		ctx := r.Context()
		ctx, cancel := context.WithTimeout(ctx, 29*time.Second)
		defer cancel()
		r = r.WithContext(ctx)

		// Sentry body logging.
		r = sentry.WithRequest(r)

		// Recover panics.
		defer func() {
			if rec := errors.Recover(recover()); rec != nil {
				Error(w, http.StatusInternalServerError)
				telemetry.ReportError(r.Context(), rec)
			}
		}()

		handler.ServeHTTP(w, r)
	})
}
