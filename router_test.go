package doris

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"connectrpc.com/connect/v2"
	"connectrpc.com/connect/v2/connecthttp"
	"connectrpc.com/grpchealth/v2"
	"github.com/altipla-consulting/errors"
	"github.com/stretchr/testify/require"
)

func TestRouterPatternsAndFallback(t *testing.T) {
	server := NewServer()
	server.Get("/items/{id}", func(w http.ResponseWriter, r *http.Request) error {
		request := RequestFromContext(r.Context())
		require.Same(t, r, request)
		require.Equal(t, "42", request.PathValue("id"))
		deadline, ok := r.Context().Deadline()
		require.True(t, ok)
		require.WithinDuration(t, time.Now().Add(29*time.Second), deadline, time.Second)
		w.WriteHeader(http.StatusAccepted)
		return nil
	})
	server.HandleFunc("/exact", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	server.Handler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.NotNil(t, RequestFromContext(r.Context()))
		w.WriteHeader(http.StatusTeapot)
	}))

	cases := []struct {
		method string
		path   string
		status int
	}{
		{
			method: http.MethodGet,
			path:   "/items/42",
			status: http.StatusAccepted,
		},
		{
			method: http.MethodHead,
			path:   "/items/42",
			status: http.StatusAccepted,
		},
		{
			method: http.MethodPost,
			path:   "/items/42",
			status: http.StatusTeapot,
		},
		{
			method: http.MethodGet,
			path:   "/exact",
			status: http.StatusNoContent,
		},
		{
			method: http.MethodGet,
			path:   "/exact/child",
			status: http.StatusTeapot,
		},
		{
			method: http.MethodGet,
			path:   "/exactly",
			status: http.StatusTeapot,
		},
		{
			method: http.MethodGet,
			path:   "/health",
			status: http.StatusOK,
		},
		{
			method: http.MethodHead,
			path:   "/health",
			status: http.StatusOK,
		},
		{
			method: http.MethodGet,
			path:   "/other",
			status: http.StatusTeapot,
		},
	}
	for _, tc := range cases {
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			response := httptest.NewRecorder()
			server.ServeHTTP(response, httptest.NewRequest(tc.method, tc.path, nil))
			require.Equal(t, tc.status, response.Code)
		})
	}
}

func TestRouterSubrouterPathValues(t *testing.T) {
	server := NewServer()
	sub := server.Host("{tenant}.example.com").PathPrefix("/projects/{project}").Subrouter()
	sub.HandleFunc("/items/{id:[0-9]+}", func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "acme", r.PathValue("tenant"))
		require.Equal(t, "demo", r.PathValue("project"))
		require.Equal(t, "42", r.PathValue("id"))
		require.Empty(t, r.PathValue("missing"))
		require.Same(t, r, RequestFromContext(r.Context()))
		require.Equal(t, "42", RequestFromContext(r.Context()).PathValue("id"))
		w.WriteHeader(http.StatusNoContent)
	})

	request := httptest.NewRequest(http.MethodGet, "http://acme.example.com/projects/demo/items/42", nil)
	request.SetPathValue("existing", "preserved")
	sub.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			require.Equal(t, "demo", r.PathValue("project"))
			require.Equal(t, "preserved", r.PathValue("existing"))
			next.ServeHTTP(w, r)
		})
	})
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)
	require.Equal(t, http.StatusNoContent, response.Code)
}

func TestRouterConnectWithFallback(t *testing.T) {
	server := NewServer()
	MountAPI(server, func(s *connect.Server) {
		grpchealth.Register(s, successServer{})
	})
	server.Handler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	}))

	httpServer := httptest.NewServer(server)
	t.Cleanup(httpServer.Close)
	client := grpchealth.NewClient(connect.NewClient(connecthttp.NewTransport(httpServer.Client(), httpServer.URL)))
	status, err := client.Check(context.Background(), new(grpchealth.CheckRequest))
	require.NoError(t, err)
	require.Equal(t, grpchealth.StatusServing, status.Status)
}

func TestRouterRecoversPanics(t *testing.T) {
	server := NewServer()
	server.HandleFunc("/panic", func(w http.ResponseWriter, r *http.Request) {
		panic("handler failed")
	})
	server.Get("/panic-helper", func(w http.ResponseWriter, r *http.Request) error {
		panic("handler failed")
	})
	app := http.NewServeMux()
	app.Handle("/panic-fallback", Handler(func(w http.ResponseWriter, r *http.Request) error {
		require.Same(t, r, RequestFromContext(r.Context()))
		panic("handler failed")
	}))
	server.Handler(app)

	for _, path := range []string{"/panic", "/panic-helper", "/panic-fallback"} {
		t.Run(path, func(t *testing.T) {
			response := httptest.NewRecorder()
			server.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
			require.Equal(t, http.StatusInternalServerError, response.Code)
		})
	}
}

func TestRouterMethodHelpers(t *testing.T) {
	server := NewServer()
	methods := []struct {
		method   string
		register func(string, HandlerError)
	}{
		{http.MethodHead, server.Head},
		{http.MethodGet, server.Get},
		{http.MethodPost, server.Post},
		{http.MethodPut, server.Put},
		{http.MethodPatch, server.Patch},
		{http.MethodDelete, server.Delete},
		{http.MethodOptions, server.Options},
	}
	for _, method := range methods {
		method.register("/methods", func(w http.ResponseWriter, r *http.Request) error {
			w.Header().Set("X-Handler-Method", method.method)
			return errors.New("handler failed")
		})
	}
	for _, method := range methods {
		t.Run(method.method, func(t *testing.T) {
			response := httptest.NewRecorder()
			server.ServeHTTP(response, httptest.NewRequest(method.method, "/methods", nil))
			require.Equal(t, method.method, response.Header().Get("X-Handler-Method"))
			require.Equal(t, http.StatusInternalServerError, response.Code)
		})
	}
}
