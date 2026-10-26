package doris

import (
	"net/http"

	"connectrpc.com/connect/v2"
	"connectrpc.com/connect/v2/connecthttp"
	"connectrpc.com/connect/v2/connectproto"
	"github.com/rs/cors"
	"libs.altipla.consulting/routing"
)

// ConnectHub helps mounting Connect APIs to their correct endpoints.
type ConnectHub struct {
	r    *Router
	cors []string
}

// NewConnectHub creates a new hub prepared to mount Connect APIs.
func NewConnectHub(r *Router, opts ...ConnectHubOption) *ConnectHub {
	hub := &ConnectHub{
		r:    r,
		cors: []string{"https://studio.buf.build"},
	}
	for _, opt := range opts {
		opt(hub)
	}
	return hub
}

// MountFn registers one or more Connect services on a server.
type MountFn func(server *connect.Server)

// Mount registers and serves a new API on its own Connect server.
// The standard Doris interceptors always run before the supplied interceptors,
// which apply only to the services registered by fn.
func (hub *ConnectHub) Mount(fn MountFn, interceptors ...connect.ServerInterceptor) {
	server := connect.NewServer(append(ServerInterceptors(), interceptors...)...)
	fn(server)

	adapter := routerAdapter(hub.r.PathPrefixHandlerHTTP)
	if len(hub.cors) > 0 {
		cnf := cors.Options{
			AllowedOrigins: hub.cors,
			AllowedMethods: []string{http.MethodPost, http.MethodOptions},
			AllowedHeaders: []string{
				// Generic headers of any request.
				"authorization",
				"content-type",

				// Connect specific headers.
				"connect-timeout-ms",
				"connect-protocol-version",

				// Added by iPhone Safari.
				"user-agent",
			},
			MaxAge: 300,
		}
		c := cors.New(cnf)
		adapter = func(pattern string, handler http.Handler) {
			hub.r.PathPrefixHandlerHTTP(pattern, c.Handler(handler))
		}
	}

	jsonCodec := connectproto.NewJSONCodec()
	jsonCodec.MarshalOptions.EmitUnpopulated = true

	connecthttp.Mount(adapter, server,
		connecthttp.WithCodecs(connectproto.NewBinaryCodec(), jsonCodec),
		connecthttp.WithReadMaxBytes(0),
	)
}

type routerAdapter func(pattern string, handler http.Handler)

func (a routerAdapter) Handle(pattern string, handler http.Handler) {
	a(pattern, handler)
}

// ConnectHubOption configures the Connect hub.
type ConnectHubOption func(cnf *ConnectHub)

// WithCORS configures the domains authorized to access the API.
// The standard studio.buf.build is always authorized by default.
func WithCORS(domains ...string) ConnectHubOption {
	return func(cnf *ConnectHub) {
		cnf.cors = append(cnf.cors, domains...)
	}
}

// Deprecated: Use NewConnectHub instead.
type RegisterFn func() (pattern string, handler http.Handler)

// Deprecated: Use NewConnectHub instead.
func Connect(r *routing.Router, fn RegisterFn) {
	pattern, handler := fn()
	r.PathPrefixHandler(pattern, routing.NewHandlerFromHTTP(handler))
}

// Deprecated: Use NewConnectHub instead.
func ConnectCORS(origins []string) cors.Options {
	return cors.Options{
		AllowedOrigins: origins,
		AllowedMethods: []string{http.MethodPost, http.MethodOptions},
		AllowedHeaders: []string{"authorization", "content-type"},
		MaxAge:         300,
	}
}

// Deprecated: Use NewConnectHub instead.
func ConnectOptions(interceptors ...connect.ServerInterceptor) []connect.ServerInterceptor {
	return nil
}
