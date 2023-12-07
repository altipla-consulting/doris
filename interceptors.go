package doris

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/altipla-consulting/env"
	"github.com/altipla-consulting/errors"
	"github.com/altipla-consulting/telemetry"
	"github.com/bufbuild/connect-go"
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
			ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
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

			reply, err := next(ctx, in)
			if err != nil {
				logError(ctx, in.Spec().Procedure, err)
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
		case connect.CodeInvalidArgument, connect.CodeNotFound, connect.CodeAlreadyExists, connect.CodeFailedPrecondition, connect.CodeAborted, connect.CodeUnimplemented, connect.CodeCanceled, connect.CodeUnauthenticated, connect.CodeResourceExhausted:
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
