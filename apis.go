package doris

import (
	"net/http"

	"github.com/bufbuild/connect-go"
	"github.com/rs/cors"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"libs.altipla.consulting/errors"
	"libs.altipla.consulting/routing"
)

// ConnectHub helps mounting Connect APIs to their correct endpoints.
type ConnectHub struct {
	r            *routing.Router
	cors         []string
	interceptors []connect.Interceptor
}

// NewConnectHub creates a new hub prepared to mount Connect APIs.
func NewConnectHub(r *routing.Router, opts ...ConnectHubOption) *ConnectHub {
	hub := &ConnectHub{
		r:    r,
		cors: []string{"https://studio.buf.build"},
	}
	for _, opt := range opts {
		opt(hub)
	}
	return hub
}

// MountFn should be implemented by a global function in the API package to
// register itself.
type MountFn func(opts ...connect.HandlerOption) (string, http.Handler)

// Mount a new API.
func (hub *ConnectHub) Mount(fn MountFn) {
	pattern, handler := fn(hub.opts()...)
	if len(hub.cors) > 0 {
		cnf := cors.Options{
			AllowedOrigins: hub.cors,
			AllowedMethods: []string{http.MethodPost, http.MethodOptions},
			AllowedHeaders: []string{"authorization", "content-type", "connect-timeout-ms"},
			MaxAge:         300,
		}
		handler = cors.New(cnf).Handler(handler)
	}
	hub.r.PathPrefixHandler(pattern, routing.NewHandlerFromHTTP(handler))
}

func (hub *ConnectHub) opts() []connect.HandlerOption {
	return []connect.HandlerOption{
		connect.WithInterceptors(ServerInterceptors()...),
		connect.WithInterceptors(hub.interceptors...),
		connect.WithCodec(new(codecJSON)),
	}
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

// WithInterceptors configures the interceptors to configure in the APIs.
func WithInterceptors(interceptors ...connect.Interceptor) ConnectHubOption {
	return func(cnf *ConnectHub) {
		cnf.interceptors = append(cnf.interceptors, interceptors...)
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
func ConnectOptions(interceptors ...connect.Interceptor) []connect.HandlerOption {
	return []connect.HandlerOption{}
}

type codecJSON struct{}

func (c *codecJSON) Name() string {
	return "json"
}

func (c *codecJSON) Marshal(message any) ([]byte, error) {
	msg, ok := message.(proto.Message)
	if !ok {
		return nil, errors.Errorf("%T doesn't implement proto.Message", message)
	}
	m := protojson.MarshalOptions{
		EmitUnpopulated: true,
	}
	return m.Marshal(msg)
}

func (c *codecJSON) Unmarshal(binary []byte, message any) error {
	msg, ok := message.(proto.Message)
	if !ok {
		return errors.Errorf("%T doesn't implement proto.Message", message)
	}
	return protojson.Unmarshal(binary, msg)
}
