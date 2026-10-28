package doris

import (
	"context"
	"net/http"
)

type requestKey struct{}

type requestContext struct {
	request *http.Request
}

// RequestFromContext returns the request attached by Doris, or nil outside a Doris handler.
func RequestFromContext(ctx context.Context) *http.Request {
	request, _ := ctx.Value(requestKey{}).(*requestContext)
	if request == nil {
		return nil
	}
	return request.request
}
