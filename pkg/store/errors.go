package store

import "fmt"

var ErrObsolete = fmt.Errorf("todo block changed")

type ReadError struct {
	Path string
	Err  error
}

func (e ReadError) Error() string {
	return fmt.Sprintf("read todo file %q: %v", e.Path, e.Err)
}

func (e ReadError) Unwrap() error {
	return e.Err
}
