package doris

import (
	"connectrpc.com/connect"
	"github.com/altipla-consulting/errors"
)

func Errorf(code connect.Code, msg string, args ...any) error {
	return errors.Trace(connect.NewError(code, errors.Errorf(msg, args...)))
}
