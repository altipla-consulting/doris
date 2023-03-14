package doris

import (
	"github.com/altipla-consulting/errors"
	"github.com/bufbuild/connect-go"
)

func Errorf(code connect.Code, msg string, args ...any) error {
	return errors.Trace(connect.NewError(code, errors.Errorf(msg, args...)))
}
