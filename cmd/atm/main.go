package main

import (
	"fmt"
	"github.com/chinaykc/atm"
	"os"
)

func main() {
	if err := atm.RunCLI(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(atm.ExitCode(err))
	}
}
