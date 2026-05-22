//go:build windows

package store

import (
	"errors"
	"os"
	"os/signal"
	"syscall"
)

func notifyRestoreSignals(signals chan<- os.Signal) {
	signal.Notify(signals, os.Interrupt)
}

func signalExitCode(os.Signal) int {
	return 130
}

func isCrossDeviceLinkError(err error) bool {
	return errors.Is(err, syscall.Errno(17))
}
