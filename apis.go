package doris

import (
	"net/http"
	"strings"

	"connectrpc.com/connect/v2"
	"connectrpc.com/connect/v2/connecthttp"
	"connectrpc.com/connect/v2/connectproto"
	"github.com/gorilla/mux"
	"github.com/rs/cors"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// MountFn registers one or more Connect services on a server.
type MountFn func(server *connect.Server)

type apiRoutes interface {
	Handle(path string, handler http.Handler) *mux.Route
	PathPrefix(path string) *mux.Route
}

// MountAPI registers API routes on an application router served by Doris.
// The standard Doris interceptors run before the supplied interceptors.
func MountAPI(router apiRoutes, fn MountFn, interceptors ...connect.ServerInterceptor) {
	server := connect.NewServer(append(ServerInterceptors(), interceptors...)...)
	fn(server)

	jsonCodec := connectproto.NewJSONCodec()
	jsonCodec.MarshalOptions.EmitUnpopulated = true

	connecthttp.Mount(apiRouter{router}, server,
		connecthttp.WithCodecs(connectproto.NewBinaryCodec(), jsonCodec),
		connecthttp.WithReadMaxBytes(0),
	)
}

type apiRouter struct {
	apiRoutes
}

// Handle registers exact procedures and service prefixes using Connect's mux semantics.
func (r apiRouter) Handle(pattern string, handler http.Handler) {
	if strings.HasSuffix(pattern, "/") {
		r.PathPrefix(pattern).Handler(handler)
		return
	}
	r.apiRoutes.Handle(pattern, handler)
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

func trimMessage(m protoreflect.Message) protoreflect.Message {
	m.Range(func(fd protoreflect.FieldDescriptor, value protoreflect.Value) bool {
		switch {
		case fd.Kind() == protoreflect.StringKind && fd.IsList():
			for list, i := value.List(), 0; i < list.Len(); i++ {
				list.Set(i, trimString(list.Get(i)))
			}

		case fd.Kind() == protoreflect.StringKind:
			m.Set(fd, trimString(value))

		// We need to trim the keys too, so we build a new map and then replace
		// the existing one once we ranged all the keys.
		case fd.Kind() == protoreflect.MessageKind && fd.IsMap() && fd.MapKey().Kind() == protoreflect.StringKind:
			trimmed := map[string]protoreflect.Value{}
			value.Map().Range(func(mk protoreflect.MapKey, v protoreflect.Value) bool {
				switch fd.MapValue().Kind() {
				case protoreflect.StringKind:
					v = trimString(v)
				case protoreflect.MessageKind:
					trimMessage(v.Message())
				}

				trimmed[trimString(mk.Value()).String()] = v
				value.Map().Clear(mk)
				return true
			})
			for k, sub := range trimmed {
				value.Map().Set(protoreflect.ValueOfString(k).MapKey(), sub)
			}

		case fd.Kind() == protoreflect.MessageKind && fd.IsMap():
			// Map has numeric keys, only process the values.
			value.Map().Range(func(mk protoreflect.MapKey, v protoreflect.Value) bool {
				switch fd.MapValue().Kind() {
				case protoreflect.StringKind:
					v.Map().Set(mk, trimString(v))
				case protoreflect.MessageKind:
					trimMessage(v.Message())
				}

				return true
			})

		case fd.Kind() == protoreflect.MessageKind && fd.IsList():
			for list, i := value.List(), 0; i < list.Len(); i++ {
				list.Set(i, protoreflect.ValueOfMessage(trimMessage(list.Get(i).Message())))
			}

		case fd.Kind() == protoreflect.MessageKind:
			trimMessage(value.Message())
		}

		return true
	})

	return m
}

func trimString(v protoreflect.Value) protoreflect.Value {
	return protoreflect.ValueOfString(strings.TrimSpace(v.String()))
}
