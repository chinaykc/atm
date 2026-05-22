package store

import (
	"fmt"
	"io"
	"os"
	"os/signal"
)

func SetupRestoreSignals(workspace *Workspace, stderr io.Writer) func() {
	signals := make(chan os.Signal, 1)
	notifyRestoreSignals(signals)
	done := make(chan struct{})
	go func() {
		select {
		case sig := <-signals:
			fmt.Fprintf(stderr, "received %s, restoring todo file\n", sig)
			if err := workspace.Restore(); err != nil {
				fmt.Fprintf(stderr, "restore todo file failed: %v\n", err)
			}
			signal.Stop(signals)
			os.Exit(signalExitCode(sig))
		case <-done:
			return
		}
	}()

	return func() {
		signal.Stop(signals)
		close(done)
	}
}
