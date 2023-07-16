package doris

import (
	"context"
	"fmt"
	stdlog "log" // revive:disable-line:imports-blacklist
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"cloud.google.com/go/profiler"
	"github.com/altipla-consulting/env"
	"github.com/altipla-consulting/errors"
	log "github.com/sirupsen/logrus"
	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"
	"golang.org/x/sync/errgroup"
	"libs.altipla.consulting/routing"
)

// Server is the root server of the application.
type Server struct {
	*ServerPort
	ctx    context.Context
	cancel context.CancelFunc
	grp    *errgroup.Group

	ports []*ServerPort
}

// NewServer creates a new root server in the default port. It won't start until
// you call Serve() on it.
func NewServer(opts ...Option) *Server {
	ctx, cancel := context.WithCancel(context.Background())
	grp, ctx := errgroup.WithContext(ctx)

	server := &Server{
		ctx:    ctx,
		cancel: cancel,
		grp:    grp,
	}
	server.ServerPort = newServerPort(server, opts, false)

	// Register an internal port for health checks and metrics.
	// It should be first to shutdown it first too and disconnect live connections
	// as soon as possible when restarting the app.
	internal := newServerPort(server, opts, true)
	internal.Get("/metrics", metricsHandler)
	server.ports = append(server.ports, internal)

	// Register the first default port of the server.
	server.ports = append(server.ports, server.ServerPort)

	return server
}

// Context returns a context that will be canceled when the server is stopped.
func (server *Server) Context() context.Context {
	return server.ctx
}

// GoBackground runs a background goroutine that will be canceled when the server is stopped.
// The function should return when the context is canceled.
// If the function returns an error, the server will be stopped prematurely.
// The server will not exit until all background goroutines have finished.
func (server *Server) GoBackground(fn func(ctx context.Context) error) {
	server.grp.Go(func() error {
		if err := fn(server.ctx); err != nil {
			return fmt.Errorf("background task failed: %w", err)
		}
		return nil
	})
}

// Register a new child server in a different port.
func (server *Server) RegisterPort(port string, opts ...Option) *ServerPort {
	sp := newServerPort(nil, append(opts, WithPort(port)), false)
	server.ports = append(server.ports, sp)
	return sp
}

// Serve starts the server and blocks until it is stopped with a signal.
func (server *Server) Serve() {
	if server.profiler {
		log.Info("Stackdriver Profiler enabled")

		cnf := profiler.Config{
			Service:        env.ServiceName(),
			ServiceVersion: env.Version(),
		}
		if err := profiler.Start(cnf); err != nil {
			log.Fatalf("failed to configure profiler: %s", err)
		}
	}

	for _, sp := range server.ports {
		sp.serve(server.grp)
	}

	if os.Getenv("SENTRY_DSN") != "" {
		log.WithField("dsn", os.Getenv("SENTRY_DSN")).Info("Sentry enabled")
	}

	fields := log.Fields{}
	for i, sp := range server.ports {
		fields[fmt.Sprintf("listen.%d", i)] = sp.port
	}
	log.
		WithFields(fields).
		WithFields(log.Fields{
			"version": env.Version(),
			"name":    env.ServiceName(),
		}).Info("Instance initialized successfully!")

	signalctx, done := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer done()

	select {
	case <-signalctx.Done():
	case <-server.ctx.Done():
	}

	log.Info("Shutting down")
	server.cancel()

	shutdownctx, done := context.WithTimeout(context.Background(), 25*time.Second)
	defer done()
	for _, sp := range server.ports {
		sp.shutdown(shutdownctx)
	}

	if err := server.grp.Wait(); err != nil {
		log.WithFields(errors.LogFields(err)).Fatal("Error starting the server")
	}
}

// ServerPort is a child server in a different custom port.
type ServerPort struct {
	*routing.Server

	// Configurations from options passed when initializing the port.
	http     []routing.ServerOption
	profiler bool
	listener net.Listener
	port     string

	// Internal initialization when serving to shutdown it down afterwards.
	web *http.Server
}

func newServerPort(s *Server, opts []Option, internal bool) *ServerPort {
	sp := &ServerPort{
		http: []routing.ServerOption{
			routing.WithLogrus(),
			routing.WithSentry(os.Getenv("SENTRY_DSN")),
		},
		port: "8080",
	}
	for _, opt := range opts {
		opt(s, sp, internal)
	}

	sp.Server = routing.NewServer(sp.http...)

	sp.Get("/health", healthHandler)

	return sp
}

func (sp *ServerPort) serve(grp *errgroup.Group) {
	w := log.WithFields(log.Fields{
		"stdlib": "http",
		"port":   sp.port,
	}).Writer()
	defer w.Close()

	sp.web = &http.Server{
		Addr:     ":" + sp.port,
		Handler:  h2c.NewHandler(sp, new(http2.Server)),
		ErrorLog: stdlog.New(w, "", 0),
	}

	grp.Go(func() error {
		if sp.listener != nil {
			if err := sp.web.Serve(sp.listener); err != nil && !isClosingError(err) {
				return errors.Errorf("failed to serve: %w", err)
			}
		} else {
			if err := sp.web.ListenAndServe(); err != nil && !isClosingError(err) {
				return errors.Errorf("failed to serve: %w", err)
			}
		}
		return nil
	})
}

func (sp *ServerPort) shutdown(ctx context.Context) {
	_ = sp.web.Shutdown(ctx)
	_ = sp.web.Close()
}
