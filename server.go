package doris

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"cloud.google.com/go/profiler"
	"github.com/sethvargo/go-signalcontext"
	log "github.com/sirupsen/logrus"
	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"
	"libs.altipla.consulting/env"
	"libs.altipla.consulting/errors"
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

	return &Server{
		Server:   routing.NewServer(cnf.http...),
		internal: routing.NewServer(routing.WithLogrus()),
		cnf:      cnf,
		ctx:      ctx,
		cancel:   cancel,
		port:     cnf.port,
		listener: cnf.listener,
	}
}

func (server *Server) Context() context.Context {
	return server.ctx
}

func (server *Server) finalPort() string {
	if port := os.Getenv("PORT"); port != "" {
		return port
	}
	return server.port
}

func healthHandler(w http.ResponseWriter, r *http.Request) error {
	fmt.Fprintf(w, "%s is ok\n", env.ServiceName())
	return nil
}

func (server *Server) Serve() {
	signalctx, done := signalcontext.OnInterrupt()
	defer done()

	if os.Getenv("SENTRY_DSN") != "" {
		log.WithField("dsn", os.Getenv("SENTRY_DSN")).Info("Sentry enabled")
	}

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

	web := &http.Server{
		Addr:    ":" + server.finalPort(),
		Handler: h2c.NewHandler(server, new(http2.Server)),
	}
	internal := &http.Server{
		Addr:    ":8000",
		Handler: server.internal,
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

	log.WithFields(log.Fields{
		"port":          server.finalPort(),
		"internal-port": "8000",
		"version":       env.Version(),
		"name":          env.ServiceName(),
	}).Info("Instance initialized successfully!")

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()

		<-signalctx.Done()

		log.Info("Shutting down")

		server.cancel()

		shutdownctx, done := context.WithTimeout(context.Background(), 25*time.Second)
		defer done()

		_ = internal.Shutdown(shutdownctx)
		_ = web.Shutdown(shutdownctx)
	}()
	wg.Wait()
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
