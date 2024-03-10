package doris

import (
	"net/http"

	"libs.altipla.consulting/routing"
)

type Router struct {
	*routing.Server
}

// PathPrefixHandlerHTTP registers a new HTTP handler for all the routes under the specified prefix.
func (r *Router) PathPrefixHandlerHTTP(path string, handler http.Handler) {
	r.PathPrefixHandler(path, routing.NewHandlerFromHTTP(handler))
}
