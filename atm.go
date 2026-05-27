package atm

import (
	"io"

	"github.com/chinaykc/atm/pkg/app/cli"
)

const (
	ExitOK                = cli.ExitOK
	ExitExecutionFailure  = cli.ExitExecutionFailure
	ExitValidationFailure = cli.ExitValidationFailure
	ExitStateInconsistent = cli.ExitStateInconsistent
)

func RunCLI(args []string, stdout, stderr io.Writer) error {
	return cli.Run(args, stdout, stderr)
}

func ExitCode(err error) int {
	return cli.ExitCode(err)
}
