package doris

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect/v2"
	"connectrpc.com/connect/v2/connecthttp"
	"connectrpc.com/grpchealth/v2"
	"github.com/altipla-consulting/errors"
	"github.com/altipla-consulting/telemetry"
	"github.com/altipla-consulting/telemetry/logging"
	"github.com/rs/cors"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/emptypb"

	pb "github.com/altipla-consulting/doris/internal/testdata"
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
	r := NewServer(WithPort("25000"))

	MountAPI(r, func(s *connect.Server) {
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

func TestMountWithCORSSubrouter(t *testing.T) {
	server := NewServer()
	sub := server.Subrouter()
	sub.Use(cors.New(ConnectCORS([]string{"https://example.com"})).Handler)
	MountAPI(sub, func(s *connect.Server) {
		grpchealth.Register(s, successServer{})
	})
	server.HandleFunc("/other", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusAccepted)
	})

	httpServer := httptest.NewServer(server)
	t.Cleanup(httpServer.Close)

	procedure := "/grpc.health.v1.Health/Check"
	request, err := http.NewRequest(http.MethodOptions, httpServer.URL+procedure, nil)
	require.NoError(t, err)
	request.Header.Set("Origin", "https://example.com")
	request.Header.Set("Access-Control-Request-Method", http.MethodPost)
	request.Header.Set("Access-Control-Request-Headers", "connect-protocol-version, content-type")
	response, err := httpServer.Client().Do(request)
	require.NoError(t, err)
	defer response.Body.Close()
	require.Equal(t, http.StatusNoContent, response.StatusCode)
	require.Equal(t, "https://example.com", response.Header.Get("Access-Control-Allow-Origin"))
	require.Contains(t, response.Header.Get("Access-Control-Allow-Methods"), http.MethodPost)
	require.Contains(t, strings.ToLower(response.Header.Get("Access-Control-Allow-Headers")), "connect-protocol-version")

	ctx, info := connect.NewClientContext(context.Background())
	info.RequestHeader().Set("Origin", "https://example.com")
	client := grpchealth.NewClient(connect.NewClient(connecthttp.NewTransport(httpServer.Client(), httpServer.URL)))
	status, err := client.Check(ctx, new(grpchealth.CheckRequest))
	require.NoError(t, err)
	require.Equal(t, grpchealth.StatusServing, status.Status)
	require.Equal(t, "https://example.com", info.ResponseHeader().Get("Access-Control-Allow-Origin"))

	siblingRequest, err := http.NewRequest(http.MethodGet, httpServer.URL+"/other", nil)
	require.NoError(t, err)
	siblingRequest.Header.Set("Origin", "https://example.com")
	siblingResponse, err := httpServer.Client().Do(siblingRequest)
	require.NoError(t, err)
	defer siblingResponse.Body.Close()
	require.Equal(t, http.StatusAccepted, siblingResponse.StatusCode)
	require.Empty(t, siblingResponse.Header.Get("Access-Control-Allow-Origin"))
}

type panicChecker struct{}

func (panicChecker) Check(context.Context, *grpchealth.CheckRequest) (*grpchealth.CheckResponse, error) {
	panic("health check example error")
}

func TestServicePanic(t *testing.T) {
	r := NewServer(WithPort("25000"))
	MountAPI(r, func(s *connect.Server) {
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
	r := NewServer(WithPort("25000"))
	MountAPI(r, func(s *connect.Server) {
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
	r := NewServer(WithPort("25000"))
	MountAPI(r, func(s *connect.Server) {
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

func registerAuthenticatedServer(router *Router) {
	MountAPI(router, func(s *connect.Server) {
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
	r := NewServer(WithPort("25000"))
	registerAuthenticatedServer(r.Router)
	MountAPI(r, func(s *connect.Server) {
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

func TestTrimStrings(t *testing.T) {
	in := &pb.TrimTest{
		Direct: "  direct\n\t",
		Sub: &pb.Message{
			Child:         "  child",
			RepeatedChild: []string{"  repeated child1", "repeated child2  "},
		},
		Map: map[string]string{
			"  key": "  value",
		},
		List: []string{"  item"},
		Mapsub: map[string]*pb.Message{
			"  key": {
				Child: "  mapsub child",
			},
		},
		Oneof1: &pb.TrimTest_OneofDirect1{
			OneofDirect1: "  oneof_direct1",
		},
		Oneof2: &pb.TrimTest_OneofSub2{
			OneofSub2: &pb.Message{
				Child: "  oneof_child",
			},
		},
		RepeatedMessage: []*pb.Message{
			{Child: "  repeated1"},
			{Child: "repeated2  "},
		},
		Enum: pb.TrimTest_TEST_FOO,
	}
	out := trimMessage(in.ProtoReflect()).Interface().(*pb.TrimTest)
	require.Equal(t, out.Direct, "direct")
	require.Equal(t, out.Sub.Child, "child")
	require.Equal(t, out.Sub.RepeatedChild, []string{"repeated child1", "repeated child2"})
	require.Equal(t, out.Map, map[string]string{"key": "value"})
	require.Nil(t, out.Empty)
	require.Equal(t, out.List, []string{"item"})
	require.Contains(t, out.Mapsub, "key")
	require.Equal(t, out.Mapsub["key"].Child, "mapsub child")
	require.Len(t, out.Mapsub, 1)
	require.Equal(t, out.GetOneofDirect1(), "oneof_direct1")
	require.Equal(t, out.GetOneofSub2().Child, "oneof_child")
	require.Len(t, out.RepeatedMessage, 2)
	require.Equal(t, out.RepeatedMessage[0].Child, "repeated1")
	require.Equal(t, out.RepeatedMessage[1].Child, "repeated2")
	require.Equal(t, out.Enum, pb.TrimTest_TEST_FOO)
}
