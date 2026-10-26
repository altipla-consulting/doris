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
	"google.golang.org/protobuf/types/known/emptypb"

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

type successServer struct{}

func (successServer) Check(context.Context, *grpchealth.CheckRequest) (*grpchealth.CheckResponse, error) {
	return &grpchealth.CheckResponse{Status: grpchealth.StatusServing}, nil
}

func TestMount(t *testing.T) {
	r := doris.NewServer(doris.WithPort("25000"))

	hub := doris.NewConnectHub(r.Router)
	hub.Mount(func(s *connect.Server) {
		grpchealth.Register(s, successServer{})
	})

	go r.Serve()
	t.Cleanup(r.Close)

	time.Sleep(1 * time.Second)
	client := grpchealth.NewClient(connect.NewClient(connecthttp.NewTransport(http.DefaultClient, "http://localhost:25000")))
	status, err := client.Check(context.Background(), new(grpchealth.CheckRequest))
	require.NoError(t, err)
	require.Equal(t, grpchealth.StatusServing, status.Status)
}

type panicChecker struct{}

func (panicChecker) Check(context.Context, *grpchealth.CheckRequest) (*grpchealth.CheckResponse, error) {
	panic("health check example error")
}

func TestServicePanic(t *testing.T) {
	r := doris.NewServer(doris.WithPort("25000"))
	hub := doris.NewConnectHub(r.Router)
	hub.Mount(func(s *connect.Server) {
		grpchealth.Register(s, panicChecker{})
	})
	go r.Serve()
	t.Cleanup(r.Close)

	time.Sleep(1 * time.Second)
	client := grpchealth.NewClient(connect.NewClient(connecthttp.NewTransport(http.DefaultClient, "http://localhost:25000")))
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
	r := doris.NewServer(doris.WithPort("25000"))
	hub := doris.NewConnectHub(r.Router)
	hub.Mount(func(s *connect.Server) {
		grpchealth.Register(s, errorChecker{
			err: errors.New("health check example error"),
		})
	})
	go r.Serve()
	t.Cleanup(r.Close)

	time.Sleep(1 * time.Second)
	client := grpchealth.NewClient(connect.NewClient(connecthttp.NewTransport(http.DefaultClient, "http://localhost:25000")))
	_, err := client.Check(context.Background(), new(grpchealth.CheckRequest))
	requireConnectError(t, err, connect.CodeInternal, "internal server error")
}

func TestServiceKnownConnectError(t *testing.T) {
	r := doris.NewServer(doris.WithPort("25000"))
	hub := doris.NewConnectHub(r.Router)
	hub.Mount(func(s *connect.Server) {
		grpchealth.Register(s, errorChecker{
			err: connect.Errorf(connect.CodeNotFound, "health check example error"),
		})
	})
	go r.Serve()
	t.Cleanup(r.Close)

	time.Sleep(1 * time.Second)
	client := grpchealth.NewClient(connect.NewClient(connecthttp.NewTransport(http.DefaultClient, "http://localhost:25000")))
	_, err := client.Check(context.Background(), new(grpchealth.CheckRequest))
	requireConnectError(t, err, connect.CodeNotFound, "health check example error")
}

func registerAuthenticatedServer(hub *doris.ConnectHub) {
	hub.Mount(func(s *connect.Server) {
		grpchealth.Register(s, successServer{})
	}, func(next connect.ServerFunc) connect.ServerFunc {
		return func(ctx context.Context, spec connect.Spec, stream connect.ServerStream) error {
			info, _ := connect.CallInfoForServerContext(ctx)
			if info.RequestHeader().Get("Authorization") != "Bearer test-token" {
				return connect.NewError(connect.CodeUnauthenticated, "missing credentials")
			}
			return next(ctx, spec, stream)
		}
	})
}

func TestMountInterceptors(t *testing.T) {
	r := doris.NewServer(doris.WithPort("25000"))
	hub := doris.NewConnectHub(r.Router)
	registerAuthenticatedServer(hub)
	hub.Mount(func(s *connect.Server) {
		s.Register(connect.Method{
			Spec: connect.Spec{Procedure: "/doris.test.Public/Check"},
			Handler: func(context.Context, connect.Spec, connect.ServerStream) error {
				return connect.Errorf(connect.CodeNotFound, "public service reached")
			},
		})
	})
	go r.Serve()
	t.Cleanup(r.Close)

	time.Sleep(1 * time.Second)
	client := grpchealth.NewClient(connect.NewClient(connecthttp.NewTransport(http.DefaultClient, "http://localhost:25000")))
	_, err := client.Check(context.Background(), new(grpchealth.CheckRequest))
	requireConnectError(t, err, connect.CodeUnauthenticated, "missing credentials")

	publicClient := connect.NewClient(connecthttp.NewTransport(http.DefaultClient, "http://localhost:25000"))
	err = publicClient.CallUnary(context.Background(), connect.Spec{
		Procedure: "/doris.test.Public/Check",
	}, new(emptypb.Empty), new(emptypb.Empty))
	requireConnectError(t, err, connect.CodeNotFound, "public service reached")

	ctx, info := connect.NewClientContext(context.Background())
	info.RequestHeader().Set("Authorization", "Bearer test-token")
	status, err := client.Check(ctx, new(grpchealth.CheckRequest))
	require.NoError(t, err)
	require.Equal(t, grpchealth.StatusServing, status.Status)
}
