package cli

import (
	"fmt"
	"github.com/chinaykc/atm/pkg/workspace/taskdoc"
	"io"
	"os"
	"path/filepath"
)

type UntagOptions = taskdoc.UntagOptions

type CleanOptions struct {
	Document bool
	Reports  bool
	State    bool
	Logs     bool
}

type CleanResult struct {
	DocumentBlocks int
	ReportDirs     int
	StateFiles     int
	LogDirs        int
}

func RunUntag(filePath string, stdout io.Writer, opts taskdoc.UntagOptions) error {
	result, err := taskdoc.UntagFile(filePath, opts)
	if err != nil {
		return err
	}
	fmt.Fprintf(stdout, "removed %d done state block(s) and %d running state block(s) from %s\n", result.DoneRemoved, result.RunningRemoved, filePath)
	return nil
}

func RunFormat(filePath string, stdout io.Writer) error {
	result, err := taskdoc.FormatFile(filePath)
	if err != nil {
		return err
	}
	fmt.Fprintf(stdout, "formatted %d block(s) in %s\n", result.BlockCount, filePath)
	return nil
}

func RunClean(filePath string, opts CleanOptions) (CleanResult, error) {
	var result CleanResult
	if opts.Document {
		untag, err := taskdoc.UntagFile(filePath, taskdoc.UntagOptions{Done: true, Running: true})
		if err != nil {
			return CleanResult{}, err
		}
		result.DocumentBlocks = untag.DoneRemoved + untag.RunningRemoved
	}
	root := filepath.Dir(filePath)
	if opts.Reports {
		removed, err := removeGeneratedPath(filepath.Join(root, ".atm", "reports"))
		if err != nil {
			return CleanResult{}, err
		}
		if removed {
			result.ReportDirs = 1
		}
	}
	if opts.State {
		removed, err := removeGeneratedPath(filepath.Join(root, ".atm", "state.json"))
		if err != nil {
			return CleanResult{}, err
		}
		if removed {
			result.StateFiles = 1
		}
	}
	if opts.Logs {
		removed, err := removeGeneratedPath(filepath.Join(root, ".atm", "logs"))
		if err != nil {
			return CleanResult{}, err
		}
		if removed {
			result.LogDirs = 1
		}
	}
	return result, nil
}

func RunRepairIDs(filePath string, stdout io.Writer) error {
	result, err := taskdoc.RepairIDsFile(filePath)
	if err != nil {
		return err
	}
	fmt.Fprintf(stdout, "repaired %d duplicate ATM report id(s) in %s\n", result.Repaired, filePath)
	for _, change := range result.Changes {
		fmt.Fprintf(stdout, "  block %d: %s -> %s\n", change.Block, change.OldID, change.NewID)
	}
	return nil
}

func removeGeneratedPath(path string) (bool, error) {
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	if err := os.RemoveAll(path); err != nil {
		return false, err
	}
	return true, nil
}

func RunAppend(filePath, prompt string, stdout io.Writer) error {
	result, err := taskdoc.AppendFile(filePath, prompt)
	if err != nil {
		return err
	}
	fmt.Fprintf(stdout, "appended %d block(s) to %s\n", result.BlockCount, result.Target)
	return nil
}
