package doris

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"connectrpc.com/connect"
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
			// Build a simulated request for Sentry reports.
			u := url.URL{
				Scheme: "https",
				Host:   in.Header().Get("host"),
				Path:   in.Spec().Procedure,
			}
			body := strings.NewReader(protojson.Format(in.Any().(proto.Message)))
			r, err := http.NewRequestWithContext(ctx, in.HTTPMethod(), u.String(), body)
			if err != nil {
				return nil, connect.NewError(connect.CodeInternal, err)
			}
			r.RemoteAddr = in.Peer().Addr
			for k, v := range in.Header() {
				r.Header[k] = v
			}
			ctx = sentry.WithRequest(r).Context()

			reply, err := callProcedure(next, ctx, in)
			if err != nil {
				if _, err := io.Copy(io.Discard, r.Body); err != nil {
					return nil, fmt.Errorf("doris: cannot read simulated task request body: %w", err)
				}
				logError(ctx, in.Spec().Procedure, err)

				if connect.IsWireError(err) {
					return reply, connect.NewError(connect.CodeInternal, err)
				}
				if connecterr := new(connect.Error); errors.As(err, &connecterr) && connecterr.Code() != connect.CodeUnknown {
					return reply, err
				}

				return reply, Errorf(connect.CodeInternal, "internal server error")
			}

			return reply, nil
		})
	})
}

func callProcedure(next connect.UnaryFunc, ctx context.Context, in connect.AnyRequest) (reply connect.AnyResponse, reterr error) {
	defer func() {
		if err := errors.Recover(recover()); err != nil {
			reterr = err
		}
	}()
	return next(ctx, in)
}

func logError(ctx context.Context, method string, err error) {
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
