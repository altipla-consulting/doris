package doris_test

import (
	"context"
	"net/http"
	"testing"
	"time"

	"connectrpc.com/connect/v2"
	"connectrpc.com/connect/v2/connecthttp"
	"connectrpc.com/grpchealth/v2"
	"github.com/altipla-consulting/errors"
	"github.com/altipla-consulting/telemetry"
	"github.com/altipla-consulting/telemetry/logging"
	"github.com/stretchr/testify/require"

	"github.com/altipla-consulting/doris"
)

func init() {
	telemetry.Configure(logging.Debug())
}

func requireConnectError(t *testing.T, err error, code connect.Code, message string) {
	t.Helper()
	require.Error(t, err)
	var connecterr *connect.Error
	require.True(t, errors.As(err, &connecterr), "expected *connect.Error, got %T", err)
	require.Equal(t, code, connecterr.Code())
	require.Equal(t, message, connecterr.Message())
}

func newHealthClient(baseURL string) *grpchealth.Client {
	return grpchealth.NewClient(connect.NewClient(
		connecthttp.NewTransport(http.DefaultClient, baseURL),
	))
}

type successChecker struct{}

func (successChecker) Check(context.Context, *grpchealth.CheckRequest) (*grpchealth.CheckResponse, error) {
	return &grpchealth.CheckResponse{Status: grpchealth.StatusServing}, nil
}

func TestMount(t *testing.T) {
	r := doris.NewServer(doris.WithPort("25000"))

	hub := doris.NewConnectHub(r.Router)
	hub.Mount(func(s *connect.Server) {
		grpchealth.Register(s, successChecker{})
	})

	go r.Serve()
	t.Cleanup(r.Close)

	time.Sleep(1 * time.Second)
	client := newHealthClient("http://localhost:25000")
	status, err := client.Check(context.Background(), new(grpchealth.CheckRequest))
	require.NoError(t, err)
	require.Equal(t, grpchealth.StatusServing, status.Status)
}

type panicChecker struct{}

func (panicChecker) Check(context.Context, *grpchealth.CheckRequest) (*grpchealth.CheckResponse, error) {
	panic("health check example error")
}

func TestServicePanic(t *testing.T) {
	r := doris.NewServer(doris.WithPort("25001"))
	hub := doris.NewConnectHub(r.Router)
	hub.Mount(func(s *connect.Server) {
		grpchealth.Register(s, panicChecker{})
	})
	go r.Serve()
	t.Cleanup(r.Close)

	time.Sleep(1 * time.Second)
	client := newHealthClient("http://localhost:25001")
	_, err := client.Check(context.Background(), new(grpchealth.CheckRequest))
	requireConnectError(t, err, connect.CodeInternal, "internal server error")
}

type errorChecker struct {
	err error
}

func (c errorChecker) Check(context.Context, *grpchealth.CheckRequest) (*grpchealth.CheckResponse, error) {
	return nil, errors.Trace(c.err)
}

func TestServiceInternalError(t *testing.T) {
	r := doris.NewServer(doris.WithPort("25002"))
	hub := doris.NewConnectHub(r.Router)
	hub.Mount(func(s *connect.Server) {
		grpchealth.Register(s, errorChecker{
			err: errors.New("health check example error"),
		})
	})
	go r.Serve()
	t.Cleanup(r.Close)

	time.Sleep(1 * time.Second)
	client := newHealthClient("http://localhost:25002")
	_, err := client.Check(context.Background(), new(grpchealth.CheckRequest))
	requireConnectError(t, err, connect.CodeInternal, "internal server error")
}

func TestServiceKnownConnectError(t *testing.T) {
	r := doris.NewServer(doris.WithPort("25003"))
	hub := doris.NewConnectHub(r.Router)
	hub.Mount(func(s *connect.Server) {
		grpchealth.Register(s, errorChecker{
			err: doris.Errorf(connect.CodeNotFound, "health check example error"),
		})
	})
	go r.Serve()
	t.Cleanup(r.Close)

	time.Sleep(1 * time.Second)
	client := newHealthClient("http://localhost:25003")
	_, err := client.Check(context.Background(), new(grpchealth.CheckRequest))
	requireConnectError(t, err, connect.CodeNotFound, "health check example error")
}
