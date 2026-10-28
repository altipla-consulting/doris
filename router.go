package doris

import (
	"context"
	"net/http"
	"time"

	"github.com/altipla-consulting/errors"
	"github.com/altipla-consulting/sentry"
	"github.com/altipla-consulting/telemetry"
	"github.com/gorilla/mux"
)

type Router struct {
	*mux.Router
}

// Get registers a GET route, which also handles HEAD requests.
func (r *Router) Get(path string, handler HandlerError) {
	r.Handle(path, Handler(handler)).Methods(http.MethodGet, http.MethodHead)
}

// Post registers a POST route.
func (r *Router) Post(path string, handler HandlerError) {
	r.Handle(path, Handler(handler)).Methods(http.MethodPost)
}

// Put registers a PUT route.
func (r *Router) Put(path string, handler HandlerError) {
	r.Handle(path, Handler(handler)).Methods(http.MethodPut)
}

// Patch registers a PATCH route.
func (r *Router) Patch(path string, handler HandlerError) {
	r.Handle(path, Handler(handler)).Methods(http.MethodPatch)
}

// Delete registers a DELETE route.
func (r *Router) Delete(path string, handler HandlerError) {
	r.Handle(path, Handler(handler)).Methods(http.MethodDelete)
}

// Options registers an OPTIONS route.
func (r *Router) Options(path string, handler HandlerError) {
	r.Handle(path, Handler(handler)).Methods(http.MethodOptions)
}

// Head registers a HEAD route.
func (r *Router) Head(path string, handler HandlerError) {
	r.Handle(path, Handler(handler)).Methods(http.MethodHead)
}

// Handler registers an application handler as the fallback for routes not handled by Doris.
func (r *Router) Handler(handler http.Handler) {
	r.NewRoute().Handler(handler)
}

// Subrouter creates an unrestricted route group that inherits the server middleware.
func (r *Router) Subrouter() *mux.Router {
	return r.NewRoute().Subrouter()
}

func stdMiddlewares(handler http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Expose Gorilla route parameters through the standard request API.
		for name, value := range mux.Vars(r) {
			r.SetPathValue(name, value)
		}

		// Timeout.
		ctx := r.Context()
		ctx, cancel := context.WithTimeout(ctx, 29*time.Second)
		defer cancel()
		r = r.WithContext(ctx)

		// Sentry body logging.
		r = sentry.WithRequest(r)

		// Request access from handler contexts.
		request := new(requestContext)
		r = r.WithContext(context.WithValue(r.Context(), requestKey{}, request))
		request.request = r

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
