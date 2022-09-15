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

type RegisterFn func() (pattern string, handler http.Handler)

// Connect registers a new service in the router.
func Connect(r *routing.Router, fn RegisterFn) {
	pattern, handler := fn()
	r.PathPrefixHandler(pattern, routing.NewHandlerFromHTTP(handler))
}

// ConnectCORS returns a CORS configuration for the given domains with the
// optimal settings for a Connect API.
func ConnectCORS(origins []string) cors.Options {
	return cors.Options{
		AllowedOrigins: origins,
		AllowedMethods: []string{http.MethodPost, http.MethodOptions},
		AllowedHeaders: []string{"authorization", "content-type"},
		MaxAge:         300,
	}
}

// ConnectOptions returns the list of options to register a new serve, including
// middlewares, codecs, etc.
func ConnectOptions() []connect.HandlerOption {
	return []connect.HandlerOption{
		connect.WithInterceptors(ServerInterceptors()...),
		connect.WithCodec(new(codecJSON)),
	}
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
