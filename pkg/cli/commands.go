package cli

import (
	"atm/pkg/dsl"
	"atm/pkg/store"
	"bytes"
	"fmt"
	"io"
	"os"
	"strings"
)

type UntagOptions struct {
	Done    bool
	Running bool
}

func RunUntag(filePath string, stdout io.Writer, opts UntagOptions) error {
	doneRemoved := 0
	runningRemoved := 0
	if err := store.RewriteBlocks(filePath, func(blocks []dsl.Block) error {
		for i := range blocks {
			body := blocks[i].Body
			if opts.Done {
				var ok bool
				body, ok = dsl.RemoveDone(body)
				if ok {
					doneRemoved++
				}
			}
			if opts.Running {
				var ok bool
				body, ok = dsl.RemoveRunning(body)
				if ok {
					runningRemoved++
				}
			}
			blocks[i].Body = body
		}
		return nil
	}); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "removed %d done state block(s) and %d running state block(s) from %s\n", doneRemoved, runningRemoved, filePath)
	return nil
}

func RunFormat(filePath string, stdout io.Writer) error {
	count := 0
	if err := store.RewriteBlocks(filePath, func(blocks []dsl.Block) error {
		count = len(blocks)
		for i := range blocks {
			blocks[i].Body = dsl.FormatBlockBody(blocks[i].Body)
		}
		return nil
	}); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "formatted %d block(s) in %s\n", count, filePath)
	return nil
}

func RunAppend(filePath, prompt string, stdout io.Writer) error {
	formatted, blockCount, err := formatAppendPrompt(prompt)
	if err != nil {
		return err
	}
	target, err := store.ResolveActiveTodoPath(filePath)
	if err != nil {
		return err
	}
	lock, err := store.Lock(target)
	if err != nil {
		return err
	}
	defer lock.Close()

	content, err := os.ReadFile(target)
	if err != nil {
		return store.ReadError{Path: target, Err: err}
	}
	var buf bytes.Buffer
	buf.Write(content)
	if len(content) > 0 && !endsWithBlankLine(string(content)) {
		if !strings.HasSuffix(string(content), "\n") && !strings.HasSuffix(string(content), "\r") {
			buf.WriteByte('\n')
		}
		buf.WriteByte('\n')
	}
	buf.WriteString(formatted)
	if err := store.WriteContentLocked(target, buf.Bytes()); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "appended %d block(s) to %s\n", blockCount, target)
	return nil
}

func formatAppendPrompt(prompt string) (string, int, error) {
	blocks := dsl.ParseBlocks(prompt)
	if len(blocks) == 0 {
		return "", 0, fmt.Errorf("prompt block is empty")
	}
	for i := range blocks {
		blocks[i].Body = dsl.FormatBlockBody(blocks[i].Body)
	}
	var buf bytes.Buffer
	for _, b := range blocks {
		buf.WriteString(b.Prefix)
		buf.WriteString(b.Body)
		buf.WriteString(b.Sep)
	}
	if !strings.HasSuffix(buf.String(), "\n") && !strings.HasSuffix(buf.String(), "\r") {
		buf.WriteByte('\n')
	}
	return buf.String(), len(blocks), nil
}

func endsWithBlankLine(content string) bool {
	lines := dsl.SplitLines(content)
	if len(lines) == 0 {
		return false
	}
	return dsl.IsBlankLine(lines[len(lines)-1])
}
