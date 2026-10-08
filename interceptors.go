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

	"connectrpc.com/connect/v2"
	"connectrpc.com/connect/v2/connecthttp"
	"github.com/altipla-consulting/errors"
	"github.com/altipla-consulting/sentry"
	"github.com/altipla-consulting/telemetry"
	"google.golang.org/protobuf/proto"
)

func ServerInterceptors() []connect.ServerInterceptor {
	return []connect.ServerInterceptor{
		genericTimeoutInterceptor(),
		trimRequestsInterceptor(),
		sentryLoggerInterceptor(),
	}
}

func genericTimeoutInterceptor() connect.ServerInterceptor {
	return func(next connect.ServerFunc) connect.ServerFunc {
		return func(ctx context.Context, spec connect.Spec, stream connect.ServerStream) error {
			ctx, cancel := context.WithTimeout(ctx, 29*time.Second)
			defer cancel()
			return next(ctx, spec, stream)
		}
	}
}

type trimServerStream struct {
	connect.ServerStream
}

func (s *trimServerStream) Receive(msg any) error {
	if err := s.ServerStream.Receive(msg); err != nil {
		return err
	}
	if m, ok := msg.(proto.Message); ok {
		trimMessage(m.ProtoReflect())
	}
	return nil
}

func trimRequestsInterceptor() connect.ServerInterceptor {
	return func(next connect.ServerFunc) connect.ServerFunc {
		return func(ctx context.Context, spec connect.Spec, stream connect.ServerStream) error {
			return next(ctx, spec, &trimServerStream{ServerStream: stream})
		}
	}
}

func sentryLoggerInterceptor() connect.ServerInterceptor {
	return func(next connect.ServerFunc) connect.ServerFunc {
		return func(ctx context.Context, spec connect.Spec, stream connect.ServerStream) error {
			info, _ := connect.CallInfoForServerContext(ctx)

			method := http.MethodPost
			if httpInfo, ok := connecthttp.ServerInfoForContext(ctx); ok {
				if m := httpInfo.HTTPMethod(); m != "" {
					method = m
				}
			}

			u := url.URL{
				Scheme: "https",
				Host:   info.RequestHeader().Get("host"),
				Path:   spec.Procedure,
			}
			r, err := http.NewRequestWithContext(ctx, method, u.String(), strings.NewReader(""))
			if err != nil {
				return connect.NewError(connect.CodeInternal, err.Error()).WithCause(err)
			}
			r.RemoteAddr = info.PeerAddr
			for k, v := range info.RequestHeader().All() {
				r.Header[k] = v
			}
			ctx = sentry.WithRequest(r).Context()

			err = callProcedure(next, ctx, spec, stream)
			if err != nil {
				if _, err := io.Copy(io.Discard, r.Body); err != nil {
					return fmt.Errorf("doris: cannot read simulated task request body: %w", err)
				}
				logError(ctx, spec.Procedure, err)

				if connecterr := new(connect.Error); errors.As(err, &connecterr) {
					if connecterr.IsRemote() {
						return connect.NewError(connect.CodeInternal, err.Error()).WithCause(err)
					}
					if connecterr.Code() != connect.CodeUnknown {
						return err
					}
				}

				return Errorf(connect.CodeInternal, "internal server error")
			}

			return nil
		}
	}
}

func callProcedure(next connect.ServerFunc, ctx context.Context, spec connect.Spec, stream connect.ServerStream) (reterr error) {
	defer func() {
		if err := errors.Recover(recover()); err != nil {
			reterr = err
		}
	}()
	return next(ctx, spec, stream)
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

func BearerInterceptor(token string) connect.ClientInterceptor {
	return func(next connect.ClientFunc) connect.ClientFunc {
		return func(ctx context.Context, spec connect.Spec) (connect.ClientStream, error) {
			info, ok := connect.CallInfoForClientContext(ctx)
			if !ok {
				ctx, info = connect.NewClientContext(ctx)
			}
			info.RequestHeader().Set("Authorization", fmt.Sprintf("Bearer %s", token))
			return next(ctx, spec)
		}
	}
}
