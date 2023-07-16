package doris

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"

	"github.com/VictoriaMetrics/metrics"
	"github.com/altipla-consulting/env"
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
