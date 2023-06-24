package doris

import (
	"fmt" // revive:disable-line:imports-blacklist
	"net"
	"net/http"
	"strings"

	"github.com/VictoriaMetrics/metrics"
	"github.com/altipla-consulting/env"
	"github.com/altipla-consulting/errors"
	"libs.altipla.consulting/routing"
)

func healthHandler(w http.ResponseWriter, r *http.Request) error {
	fmt.Fprintf(w, "%v %v is ok\n", env.ServiceName(), env.Version())
	return nil
}

func metricsHandler(w http.ResponseWriter, r *http.Request) error {
	metrics.WritePrometheus(w, true)
	return nil
}

func isClosingError(err error) bool {
	return errors.Is(err, http.ErrServerClosed) || errors.Is(err, net.ErrClosed) || strings.Contains(err.Error(), "use of closed network connection")
}

// Option of a server.
type Option func(sp *ServerPort)

// WithRoutingOptions configures web server options.
func WithRoutingOptions(opts ...routing.ServerOption) Option {
	return func(sp *ServerPort) {
		sp.http = append(sp.http, opts...)
	}
}

// WithProfiler enables the Google Cloud Profiler for the whole application.
// It only makes sense if enabled at the server level, not in any individual server port.
func WithProfiler() Option {
	return func(sp *ServerPort) {
		sp.profiler = true
	}
}

// WithPort changes the default port of the application. If the env variable
// PORT is defined it will override anything configured here.
func WithPort(port string) Option {
	return func(sp *ServerPort) {
		sp.port = port
	}
}

// WithListener configures the listener to use for the web server. It is useful to
// serve in custom configurations like a Unix socket or Tailscale.
func WithListener(listener net.Listener) Option {
	return func(sp *ServerPort) {
		sp.listener = listener
	}
}
