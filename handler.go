package doris

import (
	"context"
	"fmt"
	"html/template"
	"log/slog"
	"net/http"
	"time"

	"github.com/altipla-consulting/env"
	"github.com/altipla-consulting/errors"
	"github.com/altipla-consulting/telemetry"
)

type HandlerError func(w http.ResponseWriter, r *http.Request) error

func Handler(handler HandlerError) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer telemetry.ReportPanics(r.Context())

		ctx := r.Context()
		ctx, cancel := context.WithTimeout(ctx, 29*time.Second)
		defer cancel()

		r = r.WithContext(ctx)

		if err := handler(w, r); err != nil {
			if errors.Is(ctx.Err(), context.Canceled) {
				Error(w, http.StatusRequestTimeout)
				return
			}

			slog.Error("Handler failed",
				slog.String("error", err.Error()),
				slog.String("details", errors.Details(err)),
				slog.String("url", r.URL.String()))
			telemetry.ReportError(r.Context(), err)

			if errors.Is(ctx.Err(), context.DeadlineExceeded) {
				Error(w, http.StatusGatewayTimeout)
				return
			}

			if env.IsLocal() {
				w.WriteHeader(http.StatusInternalServerError)
				fmt.Fprintln(w, errors.Stack(err))
				return
			}

			Error(w, http.StatusInternalServerError)
		}
	})
}

func Error(w http.ResponseWriter, status int) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)

	tmpl, err := template.New("error").Parse(errorTemplate)
	if err != nil {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		slog.Error("Cannot parse error template", slog.String("error", err.Error()))
	}
	if err := tmpl.Execute(w, status); err != nil {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		slog.Error("Cannot execute error template", slog.String("error", err.Error()))
	}
}
