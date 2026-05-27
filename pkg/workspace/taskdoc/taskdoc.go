package taskdoc

import (
	"bytes"
	"fmt"
	"os"
	"strings"

	"github.com/chinaykc/atm/pkg/lang/document"
	langformat "github.com/chinaykc/atm/pkg/lang/format"
	"github.com/chinaykc/atm/pkg/lang/marker"
	"github.com/chinaykc/atm/pkg/runtime/store"
)

type UntagOptions struct {
	Done    bool
	Running bool
}

type FormatResult struct {
	BlockCount int
}

type UntagResult struct {
	DoneRemoved    int
	RunningRemoved int
}

type RepairIDsResult struct {
	Repaired int
	Changes  []RepairIDChange
}

type RepairIDChange struct {
	Block     int
	OldID     string
	NewID     string
	OldReport string
	NewReport string
}

type AppendResult struct {
	BlockCount int
	Target     string
}

func FormatFile(filePath string) (FormatResult, error) {
	var result FormatResult
	err := updateFileContent(filePath, func(content string) (string, bool, error) {
		formatted, count := FormatContent(content)
		result.BlockCount = count
		return formatted, true, nil
	})
	if err != nil {
		return FormatResult{}, err
	}
	return result, nil
}

func FormatContent(content string) (string, int) {
	blocks := document.ParseBlocks(content)
	if len(blocks) == 0 {
		return content, 0
	}
	for i := range blocks {
		blocks[i].Body = document.FormatTaskHeaderBody(blocks[i].Body)
		blocks[i].Body = langformat.BlockBody(blocks[i].Body)
	}
	return renderBlocks(blocks), len(blocks)
}

func UntagFile(filePath string, opts UntagOptions) (UntagResult, error) {
	var result UntagResult
	err := updateFileContent(filePath, func(content string) (string, bool, error) {
		var updated string
		updated, result = UntagContent(content, opts)
		return updated, true, nil
	})
	if err != nil {
		return UntagResult{}, err
	}
	return result, nil
}

func UntagContent(content string, opts UntagOptions) (string, UntagResult) {
	blocks := document.ParseBlocks(content)
	if len(blocks) == 0 {
		return content, UntagResult{}
	}
	var result UntagResult
	for i := range blocks {
		body := blocks[i].Body
		if opts.Done {
			var ok bool
			body, ok = marker.RemoveDone(body)
			if ok {
				result.DoneRemoved++
			}
		}
		if opts.Running {
			var ok bool
			body, ok = marker.RemoveRunning(body)
			if ok {
				result.RunningRemoved++
			}
		}
		blocks[i].Body = body
	}
	return renderBlocks(blocks), result
}

func RepairIDsFile(filePath string) (RepairIDsResult, error) {
	var result RepairIDsResult
	err := updateFileContent(filePath, func(content string) (string, bool, error) {
		var updated string
		updated, result = RepairIDsContent(content)
		return updated, result.Repaired > 0, nil
	})
	if err != nil {
		return RepairIDsResult{}, err
	}
	return result, nil
}

func RepairIDsContent(content string) (string, RepairIDsResult) {
	blocks := document.ParseBlocks(content)
	if len(blocks) == 0 {
		return content, RepairIDsResult{}
	}
	seen := make(map[string]bool)
	used := make(map[string]bool)
	var result RepairIDsResult
	for i := range blocks {
		meta, ok := marker.ATMReportMetadata(blocks[i].Body)
		if !ok {
			continue
		}
		if !seen[meta.ID] {
			seen[meta.ID] = true
			used[meta.ID] = true
			continue
		}
		baseID, source := marker.ReportIdentityForSource(blocks[i].Body, blocks[i].Context)
		newID := uniqueReportID(baseID, used)
		newReport := marker.ATMReportPath(newID)
		updated, ok := marker.RewriteATMReportIdentity(blocks[i].Body, newID, source, newReport)
		if !ok {
			continue
		}
		blocks[i].Body = updated
		seen[newID] = true
		used[newID] = true
		result.Repaired++
		result.Changes = append(result.Changes, RepairIDChange{
			Block:     i + 1,
			OldID:     meta.ID,
			NewID:     newID,
			OldReport: meta.Report,
			NewReport: newReport,
		})
	}
	if result.Repaired == 0 {
		return content, result
	}
	return renderBlocks(blocks), result
}

func uniqueReportID(base string, used map[string]bool) string {
	if base == "" {
		base = "task"
	}
	if !used[base] {
		return base
	}
	for n := 2; ; n++ {
		candidate := fmt.Sprintf("%s-%d", base, n)
		if !used[candidate] {
			return candidate
		}
	}
}

func AppendFile(filePath, prompt string) (AppendResult, error) {
	formatted, blockCount, err := FormatAppendPrompt(prompt)
	if err != nil {
		return AppendResult{}, err
	}
	target, err := store.ResolveActiveTodoPath(filePath)
	if err != nil {
		return AppendResult{}, err
	}
	lock, err := store.Lock(target)
	if err != nil {
		return AppendResult{}, err
	}
	defer lock.Close()

	content, err := os.ReadFile(target)
	if err != nil {
		return AppendResult{}, store.ReadError{Path: target, Err: err}
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
		return AppendResult{}, err
	}
	return AppendResult{BlockCount: blockCount, Target: target}, nil
}

func FormatAppendPrompt(prompt string) (string, int, error) {
	blocks := document.ParseBlocks(prompt)
	if len(blocks) == 0 {
		return "", 0, fmt.Errorf("prompt block is empty")
	}
	for i := range blocks {
		blocks[i].Body = document.FormatTaskHeaderBody(blocks[i].Body)
		blocks[i].Body = langformat.BlockBody(blocks[i].Body)
	}
	var buf bytes.Buffer
	buf.WriteString(renderBlocks(blocks))
	if !strings.HasSuffix(buf.String(), "\n") && !strings.HasSuffix(buf.String(), "\r") {
		buf.WriteByte('\n')
	}
	return buf.String(), len(blocks), nil
}

func renderBlocks(blocks []document.Block) string {
	var buf bytes.Buffer
	for _, b := range blocks {
		buf.WriteString(b.Prefix)
		buf.WriteString(b.Body)
		buf.WriteString(b.Sep)
	}
	return buf.String()
}

func updateFileContent(filePath string, update func(string) (string, bool, error)) error {
	lock, err := store.Lock(filePath)
	if err != nil {
		return err
	}
	defer lock.Close()

	content, err := os.ReadFile(filePath)
	if err != nil {
		return store.ReadError{Path: filePath, Err: err}
	}
	updated, shouldWrite, err := update(string(content))
	if err != nil {
		return err
	}
	if !shouldWrite {
		return nil
	}
	return store.WriteContentLocked(filePath, []byte(updated))
}

func endsWithBlankLine(content string) bool {
	lines := document.SplitLines(content)
	if len(lines) == 0 {
		return false
	}
	return document.IsBlankLine(lines[len(lines)-1])
}
