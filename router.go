package doris

import (
	"context"
	"net/http"
	"time"

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

// Handle sends all request to the standard HTTP handler.
func (r *Router) Handle(handler http.Handler) {
	r.PathPrefixHandlerHTTP("", stdMiddlewares(handler))
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
