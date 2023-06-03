package doris

import (
	"context"
	"fmt"
	stdlog "log" // revive:disable-line:imports-blacklist
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"cloud.google.com/go/profiler"
	"github.com/VictoriaMetrics/metrics"
	"github.com/altipla-consulting/env"
	"github.com/altipla-consulting/errors"
	log "github.com/sirupsen/logrus"
	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"
	"golang.org/x/sync/errgroup"
	"libs.altipla.consulting/routing"
)

type Server struct {
	*routing.Server
	internal *routing.Server
	cnf      *config
	ctx      context.Context
	cancel   context.CancelFunc
	port     string
	listener net.Listener
	grp      *errgroup.Group
	grpctx   context.Context
}

func NewServer(opts ...Option) *Server {
	cnf := &config{
		http: []routing.ServerOption{
			routing.WithLogrus(),
			routing.WithSentry(os.Getenv("SENTRY_DSN")),
		},
		port: "8080",
	}
	for _, opt := range opts {
		opt(cnf)
	}

	ctx, cancel := context.WithCancel(context.Background())

	grp, grpctx := errgroup.WithContext(ctx)

	return &Server{
		Server:   routing.NewServer(cnf.http...),
		internal: routing.NewServer(routing.WithLogrus()),
		cnf:      cnf,
		ctx:      ctx,
		grp:      grp,
		grpctx:   grpctx,
		cancel:   cancel,
		port:     cnf.port,
		listener: cnf.listener,
	}
}

// Context returns a context that will be canceled when the server is stopped.
func (server *Server) Context() context.Context {
	return server.ctx
}

func (server *Server) finalPort() string {
	if port := os.Getenv("PORT"); port != "" {
		return port
	}
	return server.port
}

// GoBackground runs a background goroutine that will be canceled when the server is stopped.
// The function should return when the context is canceled.
// If the function returns an error, the server will be stopped prematurely.
// The server will not exit until all background goroutines have finished.
func (server *Server) GoBackground(fn func(ctx context.Context) error) {
	server.grp.Go(func() error {
		if err := fn(server.grpctx); err != nil {
			return fmt.Errorf("background task failed: %w", err)
		}
		return nil
	})
}

// Internal returns a router to register private endpoints.
func (server *Server) Internal() *routing.Router {
	return server.internal.Router
}

func healthHandler(w http.ResponseWriter, r *http.Request) error {
	fmt.Fprintf(w, "%v %v is ok\n", env.ServiceName(), env.Version())
	return nil
}

func metricsHandler(w http.ResponseWriter, r *http.Request) error {
	metrics.WritePrometheus(w, true)
	return nil
}

func (server *Server) Serve() {
	signalctx, done := signal.NotifyContext(server.ctx, syscall.SIGINT, syscall.SIGTERM)
	defer done()

	if server.cnf.profiler {
		log.Info("Stackdriver Profiler enabled")

		cnf := profiler.Config{
			Service:        env.ServiceName(),
			ServiceVersion: env.Version(),
		}
		if err := profiler.Start(cnf); err != nil {
			log.Fatalf("failed to configure profiler: %s", err)
		}
	}

	server.Get("/health", healthHandler)
	server.internal.Get("/health", healthHandler)
	server.internal.Get("/metrics", metricsHandler)

	wserver := log.WithFields(log.Fields{
		"stdlib": "http",
		"server": "internal",
	}).Writer()
	defer wserver.Close()
	web := &http.Server{
		Addr:     ":" + server.finalPort(),
		Handler:  h2c.NewHandler(server, new(http2.Server)),
		ErrorLog: stdlog.New(wserver, "", 0),
	}

	winternal := log.WithFields(log.Fields{
		"stdlib": "http",
		"server": "internal",
	}).Writer()
	defer winternal.Close()
	internal := &http.Server{
		Addr:     ":8000",
		Handler:  server.internal,
		ErrorLog: stdlog.New(winternal, "", 0),
	}

	go func() {
		if server.listener != nil {
			if err := web.Serve(server.listener); err != nil && !isClosingError(err) {
				log.Fatalf("failed to serve: %s", err)
			}
		} else {
			if err := web.ListenAndServe(); err != nil && !isClosingError(err) {
				log.Fatalf("failed to serve: %s", err)
			}
		}
	}()
	go func() {
		if server.listener != nil {
			if err := internal.Serve(server.listener); err != nil && !isClosingError(err) {
				log.Fatalf("failed to serve internal: %s", err)
			}
		} else {
			if err := internal.ListenAndServe(); err != nil && !isClosingError(err) {
				log.Fatalf("failed to serve internal: %s", err)
			}
		}
	}()

	if os.Getenv("SENTRY_DSN") != "" {
		log.WithField("dsn", os.Getenv("SENTRY_DSN")).Info("Sentry enabled")
	}
	log.WithFields(log.Fields{
		"port":          server.finalPort(),
		"internal-port": "8000",
		"version":       env.Version(),
		"name":          env.ServiceName(),
	}).Info("Instance initialized successfully!")

	<-signalctx.Done()
	log.Info("Shutting down")

	server.cancel()

	shutdownctx, done := context.WithTimeout(context.Background(), 25*time.Second)
	defer done()
	_ = internal.Shutdown(shutdownctx)
	_ = internal.Close()
	_ = web.Shutdown(shutdownctx)
	_ = web.Close()

	server.grp.Wait()
}

func isClosingError(err error) bool {
	return errors.Is(err, http.ErrServerClosed) || errors.Is(err, net.ErrClosed) || strings.Contains(err.Error(), "use of closed network connection")
}

// Option of a server.
type Option func(cnf *config)

type config struct {
	http     []routing.ServerOption
	profiler bool
	cors     []string
	port     string
	listener net.Listener
}

// WithRoutingOptions configures web server options.
func WithRoutingOptions(opts ...routing.ServerOption) Option {
	return func(cnf *config) {
		cnf.http = append(cnf.http, opts...)
	}
}

// WithProfiler enables the Google Cloud Profiler for the application.
func WithProfiler() Option {
	return func(cnf *config) {
		cnf.profiler = true
	}
}

// WithCustomPort changes the default port of the application. If the env variable
// PORT is defined it will override anything configured here.
func WithCustomPort(port string) Option {
	return func(cnf *config) {
		cnf.port = port
	}
}

// WithListener configures the listener to use for the web server. It is useful to
// serve in custom configurations like a Unix socket or Tailscale.
func WithListener(listener net.Listener) Option {
	return func(cnf *config) {
		cnf.listener = listener
	}
}
