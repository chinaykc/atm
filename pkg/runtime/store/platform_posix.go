//go:build !windows

package store

import (
	"errors"
	"os"
	"os/signal"
	"syscall"
)

func notifyRestoreSignals(signals chan<- os.Signal) {
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
}

func signalExitCode(sig os.Signal) int {
	if s, ok := sig.(syscall.Signal); ok {
		return 128 + int(s)
	}
	return 1
}

func isCrossDeviceLinkError(err error) bool {
	return errors.Is(err, syscall.EXDEV)
}
