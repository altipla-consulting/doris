package doris

import (
	"context"
	"net/http"
	"time"

	"libs.altipla.consulting/routing"
)

type Router struct {
	*routing.Server
}

// PathPrefixHandlerHTTP registers a new HTTP handler for all the routes under the specified prefix.
func (r *Router) PathPrefixHandlerHTTP(path string, handler http.Handler) {
	r.PathPrefixHandler(path, routing.NewHandlerFromHTTP(handler))
}

// Handle sends all request to the standard HTTP handler.
func (r *Router) Handle(handler http.Handler) {
	r.PathPrefixHandlerHTTP("", timeoutHandler(handler))
}

func timeoutHandler(handler http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		ctx, cancel := context.WithTimeout(ctx, 29*time.Second)
		defer cancel()

		r = r.WithContext(ctx)

		handler.ServeHTTP(w, r)
	})
}
