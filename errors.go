package doris

import (
	"connectrpc.com/connect/v2"
	"github.com/altipla-consulting/errors"
)

func Errorf(code connect.Code, msg string, args ...any) error {
	return errors.Trace(connect.Errorf(code, msg, args...))
}
