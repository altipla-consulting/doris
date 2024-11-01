package doris

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"connectrpc.com/connect"
	"github.com/altipla-consulting/env"
	"github.com/altipla-consulting/errors"
	"github.com/altipla-consulting/sentry"
	"github.com/altipla-consulting/telemetry"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

func ServerInterceptors() []connect.Interceptor {
	return []connect.Interceptor{
		serverOnlyInterceptor(),
		genericTimeoutInterceptor(),
		trimRequestsInterceptor(),
		sentryLoggerInterceptor(),
	}
}

func serverOnlyInterceptor() connect.Interceptor {
	return connect.UnaryInterceptorFunc(func(next connect.UnaryFunc) connect.UnaryFunc {
		return connect.UnaryFunc(func(ctx context.Context, in connect.AnyRequest) (connect.AnyResponse, error) {
			if in.Spec().IsClient {
				panic("do not configure server interceptors on a client instance")
			}
			return next(ctx, in)
		})
	})
}

func genericTimeoutInterceptor() connect.Interceptor {
	return connect.UnaryInterceptorFunc(func(next connect.UnaryFunc) connect.UnaryFunc {
		return connect.UnaryFunc(func(ctx context.Context, in connect.AnyRequest) (connect.AnyResponse, error) {
			ctx, cancel := context.WithTimeout(ctx, 29*time.Second)
			defer cancel()
			return next(ctx, in)
		})
	})
}

func trimRequestsInterceptor() connect.Interceptor {
	return connect.UnaryInterceptorFunc(func(next connect.UnaryFunc) connect.UnaryFunc {
		return connect.UnaryFunc(func(ctx context.Context, in connect.AnyRequest) (connect.AnyResponse, error) {
			trimMessage(in.Any().(proto.Message).ProtoReflect())
			return next(ctx, in)
		})
	})
}

func sentryLoggerInterceptor() connect.Interceptor {
	return connect.UnaryInterceptorFunc(func(next connect.UnaryFunc) connect.UnaryFunc {
		return connect.UnaryFunc(func(ctx context.Context, in connect.AnyRequest) (connect.AnyResponse, error) {
			defer telemetry.ReportPanics(ctx)

			// Build a simulated request for Sentry reports.
			body := strings.NewReader(protojson.Format(in.Any().(proto.Message)))
			u := url.URL{
				Scheme: "https",
				Host:   in.Header().Get("host"),
				Path:   in.Spec().Procedure,
			}
			r, err := http.NewRequestWithContext(ctx, in.HTTPMethod(), u.String(), body)
			if err != nil {
				return nil, connect.NewError(connect.CodeInternal, err)
			}
			r.RemoteAddr = in.Peer().Addr
			for k, v := range in.Header() {
				r.Header[k] = v
			}
			r = sentry.WithRequest(r)
			ctx = r.Context()

			reply, err := next(ctx, in)
			if err != nil {
				logError(ctx, in.Spec().Procedure, err)
			}

			if connect.IsWireError(err) {
				return reply, connect.NewError(connect.CodeInternal, err)
			}

			return reply, err
		})
	})
}

func logError(ctx context.Context, method string, err error) {
	if env.IsLocal() {
		fmt.Println(errors.Stack(err))
	}

	if connecterr := new(connect.Error); errors.As(err, &connecterr) {
		// Always log the Connect errors.
		slog.Error("Connect call failed",
			"code", connecterr.Code().String(),
			"message", connecterr.Message(),
			"method", method,
		)

		// Do not notify those status codes.
		switch connecterr.Code() {
		case connect.CodeInvalidArgument, connect.CodeNotFound, connect.CodeAlreadyExists, connect.CodeFailedPrecondition, connect.CodeAborted, connect.CodeUnimplemented, connect.CodeCanceled, connect.CodeUnauthenticated, connect.CodeResourceExhausted, connect.CodeUnavailable:
			return
		}
	} else {
		slog.Error("Unknown error in Connect call", "error", errors.LogValue(err))
	}

	// Do not notify disconnections from the client.
	if ctx.Err() == context.Canceled {
		return
	}

	telemetry.ReportError(ctx, err)
}
