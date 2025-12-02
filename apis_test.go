package doris_test

import (
	"context"
	"net/http"
	"testing"
	"time"

	"buf.build/gen/go/grpc/grpc/connectrpc/go/grpc/health/v1/healthv1connect"
	healthv1 "buf.build/gen/go/grpc/grpc/protocolbuffers/go/grpc/health/v1"
	"connectrpc.com/connect"
	"github.com/altipla-consulting/connecttest"
	"github.com/altipla-consulting/doris"
	"github.com/altipla-consulting/telemetry"
	"github.com/altipla-consulting/telemetry/logging"
	"github.com/stretchr/testify/require"
)

type successServer struct {
	healthv1connect.UnimplementedHealthHandler
}

func (server *successServer) Check(context.Context, *connect.Request[healthv1.HealthCheckRequest]) (*connect.Response[healthv1.HealthCheckResponse], error) {
	return connect.NewResponse(&healthv1.HealthCheckResponse{
		Status: healthv1.HealthCheckResponse_SERVING,
	}), nil
}

func TestMount(t *testing.T) {
	r := doris.NewServer(doris.WithPort("25000"))

	hub := doris.NewConnectHub(r.Router)
	hub.Mount(func(opts ...connect.HandlerOption) (string, http.Handler) {
		return healthv1connect.NewHealthHandler(&successServer{}, opts...)
	})

	go r.Serve()

	time.Sleep(1 * time.Second)
	client := healthv1connect.NewHealthClient(http.DefaultClient, "http://localhost:25000")
	status, err := client.Check(context.Background(), connect.NewRequest(&healthv1.HealthCheckRequest{}))
	require.NoError(t, err)
	require.Equal(t, healthv1.HealthCheckResponse_SERVING, status.Msg.Status)

	r.Close()
}

type panicServer struct {
	healthv1connect.UnimplementedHealthHandler
}

func (server *panicServer) Check(context.Context, *connect.Request[healthv1.HealthCheckRequest]) (*connect.Response[healthv1.HealthCheckResponse], error) {
	panic("health check example error")
}

func TestServicePanic(t *testing.T) {
	telemetry.Configure(logging.Debug())

	r := doris.NewServer(doris.WithPort("25000"))

	hub := doris.NewConnectHub(r.Router)
	hub.Mount(func(opts ...connect.HandlerOption) (string, http.Handler) {
		return healthv1connect.NewHealthHandler(&panicServer{}, opts...)
	})

	go r.Serve()

	time.Sleep(1 * time.Second)
	client := healthv1connect.NewHealthClient(http.DefaultClient, "http://localhost:25000")
	_, err := client.Check(context.Background(), connect.NewRequest(&healthv1.HealthCheckRequest{}))
	require.Error(t, err)

	connecttest.RequireError(t, err, connect.CodeInternal, "internal server error")

	r.Close()
}
