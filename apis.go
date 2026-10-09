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
	r            *Router
	cors         []string
	interceptors []connect.ServerInterceptor
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

// MountOption configures a single Mount call.
type MountOption func(cnf *mountConfig)

type mountConfig struct {
	interceptors []connect.ServerInterceptor
}

// WithInterceptors adds server interceptors for this Mount only.
// They run after the hub interceptors from WithInterceptors.
func WithInterceptors(interceptors ...connect.ServerInterceptor) MountOption {
	return func(cnf *mountConfig) {
		cnf.interceptors = append(cnf.interceptors, interceptors...)
	}
}

// Mount a new API.
func (hub *ConnectHub) Mount(fn MountFn, opts ...MountOption) {
	cnf := new(mountConfig)
	for _, opt := range opts {
		opt(cnf)
	}

	interceptors := append(ServerInterceptors(), hub.interceptors...)
	interceptors = append(interceptors, cnf.interceptors...)
	server := connect.NewServer(interceptors...)
	fn(server)

	var wrap func(http.Handler) http.Handler
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
		wrap = c.Handler
	}

	jsonCodec := connectproto.NewJSONCodec()
	jsonCodec.MarshalOptions.EmitUnpopulated = true

	connecthttp.Mount(&routerAdapter{hub: hub, wrap: wrap}, server,
		connecthttp.WithCodecs(connectproto.NewBinaryCodec(), jsonCodec),
		connecthttp.WithReadMaxBytes(0),
	)
}

type routerAdapter struct {
	hub  *ConnectHub
	wrap func(http.Handler) http.Handler
}

func (a *routerAdapter) Handle(pattern string, handler http.Handler) {
	if a.wrap != nil {
		handler = a.wrap(handler)
	}
	a.hub.r.PathPrefixHandlerHTTP(pattern, handler)
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
