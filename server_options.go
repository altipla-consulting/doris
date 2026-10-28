package doris

import "net"

// Option of a server.
type Option func(s *Server, sp *ServerPort, internal bool)

// WithPort changes the default port of the application. If the env variable
// PORT is defined it will override anything configured here.
func WithPort(port string) Option {
	return func(s *Server, sp *ServerPort, internal bool) {
		sp.port = port
	}
}

// WithListener configures the listener to use for the web server. It is useful to
// serve in custom configurations like a Unix socket or Tailscale.
func WithListener(listener net.Listener) Option {
	return func(s *Server, sp *ServerPort, internal bool) {
		sp.listener = listener
	}
}

// WithInternal apply the options to the internal server with metrics and health checks.
// For example it can be used to change the port of the internal server.
// It only makes sense if enabled at the server level, not in any individual server port.
func WithInternal(opts ...Option) Option {
	return func(s *Server, sp *ServerPort, internal bool) {
		if s == nil {
			panic("WithInternal can only be used at the server level")
		}
		if !internal {
			return
		}
		for _, opt := range opts {
			opt(nil, sp, false) // false avoids infinite recursion.
		}
	}
}
