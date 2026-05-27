package cli

import (
	"bufio"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/chinaykc/atm/pkg/lang/compiler"
	"github.com/chinaykc/atm/pkg/runtime/store"
)

const placeholderContent = "This task file is currently managed by ATM.\n\nIgnore this file during the current agent run.\n"

type runManifest struct {
	RunID          string              `json:"runId"`
	Command        string              `json:"command"`
	ProjectRoot    string              `json:"projectRoot"`
	StartWorkdir   string              `json:"startWorkdir"`
	SourcePath     string              `json:"sourcePath"`
	SourceRelPath  string              `json:"sourceRelPath,omitempty"`
	SourceCopy     string              `json:"sourceCopy"`
	HiddenSource   string              `json:"hiddenSource"`
	WorkingFile    string              `json:"workingFile"`
	ResultFile     string              `json:"resultFile"`
	OutputDir      string              `json:"outputDir"`
	TaskDir        string              `json:"taskDir"`
	Status         string              `json:"status"`
	StartedAt      time.Time           `json:"startedAt"`
	EndedAt        time.Time           `json:"endedAt,omitempty"`
	ResumeCommand  string              `json:"resumeCommand"`
	RecoverCommand string              `json:"recoverCommand"`
	Imports        []runManifestSource `json:"imports,omitempty"`
}

type runManifestSource struct {
	SourcePath    string `json:"sourcePath"`
	SourceRelPath string `json:"sourceRelPath,omitempty"`
	SourceCopy    string `json:"sourceCopy"`
	HiddenSource  string `json:"hiddenSource"`
	WorkingFile   string `json:"workingFile"`
}

type runIndex struct {
	Runs []runIndexEntry `json:"runs"`
}

type runIndexEntry struct {
	RunID        string    `json:"runId"`
	ProjectRoot  string    `json:"projectRoot"`
	StartWorkdir string    `json:"startWorkdir,omitempty"`
	SourcePath   string    `json:"sourcePath"`
	Status       string    `json:"status"`
	StartedAt    time.Time `json:"startedAt"`
	EndedAt      time.Time `json:"endedAt,omitempty"`
	Manifest     string    `json:"manifest"`
}

type managedRunWorkspace struct {
	manifest runManifest
	runDir   string
	sources  []runManifestSource
	hidden   []runManifestSource
	restored bool
	runLock  *store.LockFile
	locks    *store.LockSet
}

func atmHomeDir() (string, error) {
	if value := strings.TrimSpace(os.Getenv("ATM_HOME")); value != "" {
		return filepath.Abs(value)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve ATM home: %w", err)
	}
	return filepath.Join(home, ".atm"), nil
}

func atmRunsDir() (string, error) {
	home, err := atmHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "runs"), nil
}

func newManagedRunWorkspace(command, sourceFile string, outputOverride string) (*managedRunWorkspace, error) {
	sourceAbs, err := filepath.Abs(sourceFile)
	if err != nil {
		return nil, fmt.Errorf("resolve source ATM file: %w", err)
	}
	if _, err := os.Stat(sourceAbs); err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("read source ATM file %q: %w", sourceFile, err)
		}
		return nil, err
	}
	files, err := collectRunSourceFiles(sourceAbs)
	if err != nil {
		return nil, err
	}
	projectRoot := commonSourceRoot(files)
	runID := newRunID()
	runsDir, err := atmRunsDir()
	if err != nil {
		return nil, err
	}
	runDir := filepath.Join(runsDir, runID)
	for _, dir := range []string{
		filepath.Join(runDir, "sources"),
		filepath.Join(runDir, "hidden"),
		filepath.Join(runDir, "work"),
	} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, err
		}
	}
	runLock, err := store.LockPath(filepath.Join(runDir, "run.lock"))
	if err != nil {
		return nil, err
	}
	sourceLocks, err := lockManagedSources(files)
	if err != nil {
		_ = runLock.Close()
		return nil, err
	}
	releaseOnError := true
	defer func() {
		if releaseOnError {
			_ = sourceLocks.Close()
			_ = runLock.Close()
		}
	}()
	startWD, _ := os.Getwd()
	manifest := runManifest{
		RunID:          runID,
		Command:        command,
		ProjectRoot:    projectRoot,
		StartWorkdir:   startWD,
		SourcePath:     sourceAbs,
		ResultFile:     filepath.Join(runDir, "result.todo.md"),
		OutputDir:      filepath.Join(runDir, "outputs"),
		TaskDir:        filepath.Join(runDir, "tasks"),
		Status:         "running",
		StartedAt:      time.Now(),
		ResumeCommand:  "atm resume " + runID,
		RecoverCommand: "atm resume " + runID + " --restore-source",
	}
	if strings.TrimSpace(outputOverride) != "" {
		manifest.OutputDir = outputOverride
	}
	sources := make([]runManifestSource, 0, len(files))
	workByOriginal := make(map[string]string, len(files))
	for _, path := range files {
		rel, err := filepath.Rel(projectRoot, path)
		if err != nil || rel == "." || strings.HasPrefix(rel, "..") {
			rel = filepath.Base(path)
		}
		entry := runManifestSource{
			SourcePath:    path,
			SourceRelPath: filepath.ToSlash(rel),
			SourceCopy:    filepath.Join(runDir, "sources", rel),
			HiddenSource:  filepath.Join(runDir, "hidden", rel),
			WorkingFile:   filepath.Join(runDir, "work", rel),
		}
		if err := copyFilePreserve(path, entry.SourceCopy); err != nil {
			return nil, fmt.Errorf("copy source %s: %w", path, err)
		}
		if err := copyFilePreserve(path, entry.WorkingFile); err != nil {
			return nil, fmt.Errorf("copy working source %s: %w", path, err)
		}
		sources = append(sources, entry)
		workByOriginal[path] = entry.WorkingFile
	}
	for _, entry := range sources {
		if err := rewriteWorkingImports(entry.SourcePath, entry.WorkingFile, workByOriginal); err != nil {
			return nil, err
		}
	}
	manifest.SourceRelPath = sources[0].SourceRelPath
	manifest.SourceCopy = sources[0].SourceCopy
	manifest.HiddenSource = sources[0].HiddenSource
	manifest.WorkingFile = sources[0].WorkingFile
	if len(sources) > 1 {
		manifest.Imports = append([]runManifestSource(nil), sources[1:]...)
	}
	ws := &managedRunWorkspace{manifest: manifest, runDir: runDir, sources: sources, runLock: runLock, locks: sourceLocks}
	if err := ws.writeManifest(); err != nil {
		return nil, err
	}
	if err := updateRunIndex(manifest); err != nil {
		return nil, err
	}
	if err := ws.hideSources(); err != nil {
		_ = ws.restoreSources()
		return nil, err
	}
	if err := ws.writeManifest(); err != nil {
		_ = ws.restoreSources()
		return nil, err
	}
	if err := updateRunIndex(ws.manifest); err != nil {
		_ = ws.restoreSources()
		return nil, err
	}
	releaseOnError = false
	return ws, nil
}

func (w *managedRunWorkspace) acquireLocks() error {
	if w.runLock == nil {
		lock, err := store.LockPath(filepath.Join(w.runDir, "run.lock"))
		if err != nil {
			return err
		}
		w.runLock = lock
	}
	if w.locks == nil {
		var paths []string
		for _, source := range w.sources {
			paths = append(paths, source.SourcePath)
		}
		locks, err := lockManagedSources(paths)
		if err != nil {
			_ = w.runLock.Close()
			w.runLock = nil
			return err
		}
		w.locks = locks
	}
	return nil
}

func (w *managedRunWorkspace) releaseLocks() error {
	var firstErr error
	if w.locks != nil {
		if err := w.locks.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
		w.locks = nil
	}
	if w.runLock != nil {
		if err := w.runLock.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
		w.runLock = nil
	}
	return firstErr
}

func lockManagedSources(paths []string) (*store.LockSet, error) {
	home, err := atmHomeDir()
	if err != nil {
		return nil, err
	}
	lockPaths := make([]string, 0, len(paths))
	for _, path := range paths {
		abs, err := filepath.Abs(path)
		if err != nil {
			return nil, err
		}
		sum := sha256.Sum256([]byte(filepath.Clean(abs)))
		lockPaths = append(lockPaths, filepath.Join(home, "locks", "sources", hex.EncodeToString(sum[:])+".lock"))
	}
	return store.LockManyPaths(lockPaths)
}

func collectRunSourceFiles(sourceAbs string) ([]string, error) {
	var out []string
	visited := map[string]bool{}
	active := map[string]bool{}
	var walk func(string) error
	walk = func(path string) error {
		path = filepath.Clean(path)
		if active[path] {
			return fmt.Errorf("recursive import involving %s", path)
		}
		if visited[path] {
			return nil
		}
		visited[path] = true
		active[path] = true
		defer delete(active, path)
		data, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read source ATM file %s: %w", path, err)
		}
		out = append(out, path)
		_, imports, err := compiler.ParseLocalDefinitions(path, string(data), compiler.CompileOptions{Root: filepath.Dir(path)})
		if err != nil {
			return err
		}
		for _, decl := range imports {
			child := decl.Path
			if !filepath.IsAbs(child) {
				child = filepath.Join(filepath.Dir(path), child)
			}
			child, err = filepath.Abs(child)
			if err != nil {
				return err
			}
			if err := walk(child); err != nil {
				return err
			}
		}
		return nil
	}
	if err := walk(sourceAbs); err != nil {
		return nil, err
	}
	return out, nil
}

func commonSourceRoot(files []string) string {
	if len(files) == 0 {
		wd, _ := os.Getwd()
		return wd
	}
	root := filepath.Dir(files[0])
	for _, file := range files[1:] {
		dir := filepath.Dir(file)
		for {
			rel, err := filepath.Rel(root, dir)
			if err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
				break
			}
			parent := filepath.Dir(root)
			if parent == root {
				return root
			}
			root = parent
		}
	}
	return root
}

func rewriteWorkingImports(originalPath, workingPath string, workByOriginal map[string]string) error {
	data, err := os.ReadFile(workingPath)
	if err != nil {
		return err
	}
	lines := compiler.SplitLines(string(data))
	changed := false
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "/import ") {
			continue
		}
		fields := strings.Fields(trimmed)
		pathIndex := -1
		switch len(fields) {
		case 2:
			pathIndex = 1
		case 4:
			if fields[2] == "from" {
				pathIndex = 3
			}
		}
		if pathIndex < 0 {
			continue
		}
		originalImport := fields[pathIndex]
		resolved := originalImport
		if !filepath.IsAbs(resolved) {
			resolved = filepath.Join(filepath.Dir(originalPath), resolved)
		}
		resolved, err = filepath.Abs(resolved)
		if err != nil {
			return err
		}
		target, ok := workByOriginal[filepath.Clean(resolved)]
		if !ok {
			continue
		}
		rel, err := filepath.Rel(filepath.Dir(workingPath), target)
		if err != nil {
			return err
		}
		fields[pathIndex] = filepath.ToSlash(rel)
		replacement := strings.Join(fields, " ")
		prefixLen := len(line) - len(strings.TrimLeft(line, " \t"))
		prefix := line[:prefixLen]
		eol := ""
		if strings.HasSuffix(line, "\n") {
			eol = "\n"
			replacement = strings.TrimSuffix(replacement, "\n")
		}
		lines[i] = prefix + replacement + eol
		changed = true
	}
	if !changed {
		return nil
	}
	return os.WriteFile(workingPath, []byte(strings.Join(lines, "")), 0o644)
}

func (w *managedRunWorkspace) hideSources() error {
	for _, entry := range w.sources {
		hiddenExists := false
		if _, err := os.Stat(entry.HiddenSource); err == nil {
			hiddenExists = true
		} else if !os.IsNotExist(err) {
			return err
		}

		sourceExists := false
		if _, err := os.Stat(entry.SourcePath); err == nil {
			sourceExists = true
		} else if !os.IsNotExist(err) {
			return err
		}

		if hiddenExists {
			if sourceExists && !isATMPlaceholder(entry.SourcePath) {
				return fmt.Errorf("cannot hide source %s: hidden backup already exists at %s", entry.SourcePath, entry.HiddenSource)
			}
			if !sourceExists || isATMPlaceholder(entry.SourcePath) {
				if err := os.MkdirAll(filepath.Dir(entry.SourcePath), 0o755); err != nil {
					return err
				}
				if err := os.WriteFile(entry.SourcePath, []byte(placeholderContent), 0o644); err != nil {
					return fmt.Errorf("refresh ATM placeholder %s: %w", entry.SourcePath, err)
				}
				if err := w.writeActiveSourceMarker(entry); err != nil {
					return err
				}
				w.hidden = append(w.hidden, entry)
				continue
			}
		}
		if !sourceExists {
			return fmt.Errorf("cannot hide source %s: file does not exist", entry.SourcePath)
		}
		if isATMPlaceholder(entry.SourcePath) {
			return fmt.Errorf("cannot hide source %s: placeholder exists but hidden backup is missing", entry.SourcePath)
		}
		if err := os.MkdirAll(filepath.Dir(entry.HiddenSource), 0o755); err != nil {
			return err
		}
		if err := moveFilePortable(entry.SourcePath, entry.HiddenSource); err != nil {
			return fmt.Errorf("hide source %s: %w", entry.SourcePath, err)
		}
		w.hidden = append(w.hidden, entry)
		if err := os.WriteFile(entry.SourcePath, []byte(placeholderContent), 0o644); err != nil {
			return fmt.Errorf("write ATM placeholder %s: %w", entry.SourcePath, err)
		}
		if err := w.writeActiveSourceMarker(entry); err != nil {
			return err
		}
	}
	return nil
}

func (w *managedRunWorkspace) writeActiveSourceMarker(entry runManifestSource) error {
	marker := store.ActiveMarkerPath(filepath.Join(os.TempDir(), "atm"), entry.SourcePath)
	if err := os.MkdirAll(filepath.Dir(marker), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(marker, []byte(entry.WorkingFile), 0o600); err != nil {
		return fmt.Errorf("write active source marker %s: %w", marker, err)
	}
	return nil
}

func (w *managedRunWorkspace) restoreSources() error {
	if w.restored {
		return nil
	}
	w.restored = true
	var restoreErr error
	restoreOne := func(entry runManifestSource) {
		_ = os.Remove(store.ActiveMarkerPath(filepath.Join(os.TempDir(), "atm"), entry.SourcePath))
		if _, err := os.Stat(entry.HiddenSource); err != nil {
			if !os.IsNotExist(err) && restoreErr == nil {
				restoreErr = err
			}
			return
		}
		if _, err := os.Stat(entry.SourcePath); err == nil && !isATMPlaceholder(entry.SourcePath) {
			conflict := uniqueConflictPath(filepath.Join(filepath.Dir(entry.HiddenSource), ".conflict-"+filepath.Base(entry.SourcePath)))
			if err := moveFilePortable(entry.SourcePath, conflict); err != nil && restoreErr == nil {
				restoreErr = fmt.Errorf("preserve unexpected file at %s: %w", entry.SourcePath, err)
				return
			}
		} else if err != nil && !os.IsNotExist(err) {
			if restoreErr == nil {
				restoreErr = err
			}
			return
		} else {
			_ = os.Remove(entry.SourcePath)
		}
		if err := moveFilePortable(entry.HiddenSource, entry.SourcePath); err != nil && restoreErr == nil {
			restoreErr = err
		}
	}
	for _, entry := range w.sources[1:] {
		restoreOne(entry)
	}
	if len(w.sources) > 0 {
		restoreOne(w.sources[0])
	}
	return restoreErr
}

func (w *managedRunWorkspace) finish(status string, runErr error) error {
	defer w.releaseLocks()
	if status == "" {
		status = "succeeded"
		if runErr != nil {
			status = "failed"
		}
	}
	w.manifest.Status = status
	w.manifest.EndedAt = time.Now()
	_ = copyFilePreserve(w.manifest.WorkingFile, w.manifest.ResultFile)
	manifestErr := w.writeManifest()
	indexErr := updateRunIndex(w.manifest)
	restoreErr := w.restoreSources()
	if manifestErr != nil {
		return manifestErr
	}
	if indexErr != nil {
		return indexErr
	}
	return restoreErr
}

func (w *managedRunWorkspace) writeManifest() error {
	data, err := json.MarshalIndent(w.manifest, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return writeFileAtomic(filepath.Join(w.runDir, "manifest.json"), data, 0o644)
}

func updateRunIndex(manifest runManifest) error {
	runsDir, err := atmRunsDir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(runsDir, 0o755); err != nil {
		return err
	}
	lock, err := store.LockPath(filepath.Join(runsDir, "index.lock"))
	if err != nil {
		return err
	}
	defer lock.Close()
	indexPath := filepath.Join(runsDir, "index.json")
	var index runIndex
	if data, err := os.ReadFile(indexPath); err == nil {
		_ = json.Unmarshal(data, &index)
	} else if !os.IsNotExist(err) {
		return err
	}
	entry := runIndexEntry{
		RunID:        manifest.RunID,
		ProjectRoot:  manifest.ProjectRoot,
		StartWorkdir: manifest.StartWorkdir,
		SourcePath:   manifest.SourcePath,
		Status:       manifest.Status,
		StartedAt:    manifest.StartedAt,
		EndedAt:      manifest.EndedAt,
		Manifest:     filepath.Join(runsDir, manifest.RunID, "manifest.json"),
	}
	replaced := false
	for i := range index.Runs {
		if index.Runs[i].RunID == manifest.RunID {
			index.Runs[i] = entry
			replaced = true
			break
		}
	}
	if !replaced {
		index.Runs = append(index.Runs, entry)
	}
	slices.SortFunc(index.Runs, func(a, b runIndexEntry) int {
		return a.StartedAt.Compare(b.StartedAt)
	})
	data, err := json.MarshalIndent(index, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return writeFileAtomic(indexPath, data, 0o644)
}

func writeFileAtomic(path string, data []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".write-*.tmp")
	if err != nil {
		return err
	}
	name := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		_ = os.Remove(name)
		return err
	}
	if err := tmp.Chmod(mode); err != nil {
		tmp.Close()
		_ = os.Remove(name)
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(name)
		return err
	}
	if err := os.Rename(name, path); err != nil {
		_ = os.Remove(name)
		return err
	}
	return nil
}

func loadRunManifest(ref string) (runManifest, string, error) {
	runsDir, err := atmRunsDir()
	if err != nil {
		return runManifest{}, "", err
	}
	path := ref
	if !strings.HasSuffix(path, "manifest.json") {
		if info, err := os.Stat(path); err == nil && info.IsDir() {
			path = filepath.Join(path, "manifest.json")
		} else if !filepath.IsAbs(path) && !strings.Contains(path, string(filepath.Separator)) {
			path = filepath.Join(runsDir, path, "manifest.json")
		}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return runManifest{}, "", err
	}
	var manifest runManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return runManifest{}, "", err
	}
	return manifest, filepath.Dir(path), nil
}

func selectLastRun(project, source string) (runManifest, string, error) {
	return selectRunFromIndex(project, source, false, true)
}

func selectLatestRunCopy(project, source string) (runManifest, string, error) {
	return selectRunFromIndex(project, source, true, false)
}

func selectRunFromIndex(project, source string, includeSucceeded, requireDisambiguation bool) (runManifest, string, error) {
	runsDir, err := atmRunsDir()
	if err != nil {
		return runManifest{}, "", err
	}
	data, err := os.ReadFile(filepath.Join(runsDir, "index.json"))
	if err != nil {
		if os.IsNotExist(err) {
			if includeSucceeded {
				return runManifest{}, "", fmt.Errorf("no ATM run copy found")
			}
			return runManifest{}, "", fmt.Errorf("no unfinished ATM run found")
		}
		return runManifest{}, "", err
	}
	var index runIndex
	if err := json.Unmarshal(data, &index); err != nil {
		return runManifest{}, "", err
	}
	if project != "" {
		project, _ = filepath.Abs(project)
	}
	if source != "" {
		source, _ = filepath.Abs(source)
	}
	var matches []runIndexEntry
	for _, entry := range index.Runs {
		if !includeSucceeded && entry.Status == "succeeded" {
			continue
		}
		if project != "" && !runIndexEntryMatchesProject(entry, project) {
			continue
		}
		if source != "" && filepath.Clean(entry.SourcePath) != filepath.Clean(source) {
			continue
		}
		matches = append(matches, entry)
	}
	if len(matches) == 0 {
		if includeSucceeded {
			return runManifest{}, "", fmt.Errorf("no ATM run copy found")
		}
		return runManifest{}, "", fmt.Errorf("no unfinished ATM run found")
	}
	if requireDisambiguation && len(matches) > 1 && project == "" && source == "" {
		var b strings.Builder
		b.WriteString("multiple unfinished runs found:\n")
		for i := len(matches) - 1; i >= 0; i-- {
			fmt.Fprintf(&b, "  %s  %s\n", matches[i].RunID, matches[i].SourcePath)
		}
		b.WriteString("\nresume with:\n  atm resume <run-id>")
		return runManifest{}, "", errors.New(b.String())
	}
	entry := matches[len(matches)-1]
	return loadRunManifest(entry.Manifest)
}

func runIndexEntryMatchesProject(entry runIndexEntry, project string) bool {
	project = filepath.Clean(project)
	for _, candidate := range []string{entry.StartWorkdir, entry.ProjectRoot} {
		if candidate == "" {
			continue
		}
		candidate = filepath.Clean(candidate)
		if candidate == project {
			return true
		}
		rel, err := filepath.Rel(candidate, project)
		if err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return true
		}
		rel, err = filepath.Rel(project, candidate)
		if err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

func copyFilePreserve(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	info, err := in.Stat()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(dst), ".copy-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	if _, err := io.Copy(tmp, in); err != nil {
		tmp.Close()
		_ = os.Remove(tmpName)
		return err
	}
	if err := tmp.Chmod(info.Mode().Perm()); err != nil {
		tmp.Close()
		_ = os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	if err := os.Rename(tmpName, dst); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	return nil
}

func moveFilePortable(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	if err := os.Rename(src, dst); err == nil {
		return nil
	}
	if err := copyFilePreserve(src, dst); err != nil {
		return err
	}
	return os.Remove(src)
}

func uniqueConflictPath(base string) string {
	if _, err := os.Stat(base); os.IsNotExist(err) {
		return base
	}
	ext := filepath.Ext(base)
	stem := strings.TrimSuffix(base, ext)
	for i := 1; ; i++ {
		candidate := fmt.Sprintf("%s-%d%s", stem, i, ext)
		if _, err := os.Stat(candidate); os.IsNotExist(err) {
			return candidate
		}
	}
}

func newRunID() string {
	var b [4]byte
	if _, err := rand.Read(b[:]); err != nil {
		return time.Now().Format("20060102-150405")
	}
	return time.Now().Format("20060102-150405") + "-" + hex.EncodeToString(b[:])
}

func isATMPlaceholder(path string) bool {
	data, err := os.ReadFile(path)
	return err == nil && string(data) == placeholderContent
}

func restoreSourceCopy(manifest runManifest, target string, force bool, env commandEnv) error {
	if target == "" {
		sources := append([]runManifestSource{manifestMainSource(manifest)}, manifest.Imports...)
		for _, source := range sources {
			if err := restoreOneSourceCopy(source.SourceCopy, source.SourcePath, force, env); err != nil {
				return err
			}
		}
		if len(manifest.Imports) > 0 {
			fmt.Fprintf(env.Stdout, "restored import source copies: %d file(s)\n", len(manifest.Imports))
		}
		return nil
	}
	return restoreOneSourceCopy(manifest.SourceCopy, target, force, env)
}

func manifestMainSource(manifest runManifest) runManifestSource {
	return runManifestSource{
		SourcePath:    manifest.SourcePath,
		SourceRelPath: manifest.SourceRelPath,
		SourceCopy:    manifest.SourceCopy,
		HiddenSource:  manifest.HiddenSource,
		WorkingFile:   manifest.WorkingFile,
	}
}

func restoreOneSourceCopy(sourceCopy, target string, force bool, env commandEnv) error {
	if target == "" {
		return fmt.Errorf("restore target is empty")
	}
	targetAbs, err := filepath.Abs(target)
	if err != nil {
		return err
	}
	if _, err := os.Stat(targetAbs); err == nil && !isATMPlaceholder(targetAbs) && !force {
		stat, statErr := env.Stdin.Stat()
		if statErr != nil || stat.Mode()&os.ModeCharDevice == 0 {
			return fmt.Errorf("target exists: %s; pass --force to overwrite", targetAbs)
		}
		fmt.Fprintf(env.Stderr, "target exists: %s\noverwrite with %s? [y/N] ", targetAbs, sourceCopy)
		answer, _ := bufio.NewReader(env.Stdin).ReadString('\n')
		answer = strings.ToLower(strings.TrimSpace(answer))
		if answer != "y" && answer != "yes" {
			return fmt.Errorf("restore cancelled")
		}
	} else if err != nil && !os.IsNotExist(err) {
		return err
	}
	if err := copyFilePreserve(sourceCopy, targetAbs); err != nil {
		return err
	}
	fmt.Fprintf(env.Stdout, "restored source copy: %s\n", targetAbs)
	return nil
}
