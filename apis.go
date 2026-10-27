package doris

import (
	"net/http"

	"connectrpc.com/connect/v2"
	"connectrpc.com/connect/v2/connecthttp"
	"connectrpc.com/connect/v2/connectproto"
	"github.com/rs/cors"
)

// ConnectHub helps mounting Connect APIs to their correct endpoints.
type ConnectHub struct {
	r *Router
}

// NewConnectHub creates a new hub prepared to mount Connect APIs.
func NewConnectHub(r *Router) *ConnectHub {
	return &ConnectHub{r: r}
}

// MountFn registers one or more Connect services on a server.
type MountFn func(server *connect.Server)

// Mount registers and serves a new API on its own Connect server.
// The standard Doris interceptors always run before the supplied interceptors,
// which apply only to the services registered by fn.
func (hub *ConnectHub) Mount(fn MountFn, interceptors ...connect.ServerInterceptor) {
	server := connect.NewServer(append(ServerInterceptors(), interceptors...)...)
	fn(server)

	jsonCodec := connectproto.NewJSONCodec()
	jsonCodec.MarshalOptions.EmitUnpopulated = true

	connecthttp.Mount(hub.r, server,
		connecthttp.WithCodecs(connectproto.NewBinaryCodec(), jsonCodec),
		connecthttp.WithReadMaxBytes(0),
	)
}

// ConnectCORS configures the standard CORS options for Connect APIs.
func ConnectCORS(origins []string) cors.Options {
	return cors.Options{
		AllowedOrigins: origins,
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
}
