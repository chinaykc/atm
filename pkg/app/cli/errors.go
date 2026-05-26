package cli

import (
	"errors"
	"github.com/chinaykc/atm/pkg/lang/compiler"
)

const (
	ExitOK                = 0
	ExitExecutionFailure  = 1
	ExitValidationFailure = 2
	ExitStateInconsistent = 3
)

type StateInconsistentError struct {
	Err error
}

func (e StateInconsistentError) Error() string {
	if e.Err == nil {
		return "state inconsistent"
	}
	return e.Err.Error()
}

func (e StateInconsistentError) Unwrap() error {
	return e.Err
}

func ExitCode(err error) int {
	if err == nil {
		return ExitOK
	}
	var stateErr StateInconsistentError
	if errors.As(err, &stateErr) {
		return ExitStateInconsistent
	}
	var stateErrPtr *StateInconsistentError
	if errors.As(err, &stateErrPtr) {
		return ExitStateInconsistent
	}
	var diagnosticErr compiler.DiagnosticError
	if errors.As(err, &diagnosticErr) {
		return ExitValidationFailure
	}
	return ExitExecutionFailure
}
