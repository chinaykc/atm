package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/chinaykc/atm/pkg/integration/agent"
	"github.com/chinaykc/atm/pkg/runtime/engine"
	"github.com/chinaykc/atm/pkg/runtime/store"
	urfavecli "github.com/urfave/cli/v3"
)

func resumeCommand(env commandEnv) *urfavecli.Command {
	flags := executeFlags()
	flags = append(flags,
		&urfavecli.BoolFlag{Name: "last", Usage: "resume the latest unfinished run"},
		&urfavecli.StringFlag{Name: "project", Usage: "select project root for --restore-source"},
		&urfavecli.StringFlag{Name: "source", Usage: "select source file for --restore-source"},
		&urfavecli.BoolFlag{Name: "restore-source", Usage: "restore the source copy instead of resuming execution"},
		&urfavecli.BoolFlag{Name: "force", Usage: "overwrite an existing non-placeholder restore target"},
	)
	return &urfavecli.Command{
		Name:      "resume",
		Usage:     "resume or recover a managed ATM run",
		ArgsUsage: "[run-id] [restore-target]",
		Flags:     flags,
		Action: func(ctx context.Context, cmd *urfavecli.Command) error {
			return runResumeCommand(ctx, cmd, env)
		},
	}
}

func runResumeCommand(ctx context.Context, cmd *urfavecli.Command, env commandEnv) error {
	if cmd.Bool("restore-source") {
		manifest, target, err := manifestAndTargetForRestoreCommand(cmd)
		if err != nil {
			return err
		}
		runDir := filepath.Dir(manifest.SourceCopy)
		for filepath.Base(runDir) != "sources" && filepath.Dir(runDir) != runDir {
			runDir = filepath.Dir(runDir)
		}
		if filepath.Base(runDir) == "sources" {
			runDir = filepath.Dir(runDir)
		}
		lock, err := store.LockPath(filepath.Join(runDir, "run.lock"))
		if err != nil {
			return err
		}
		defer lock.Close()
		var lockPaths []string
		if target != "" {
			lockPaths = append(lockPaths, target)
		} else {
			lockPaths = append(lockPaths, manifest.SourcePath)
			for _, item := range manifest.Imports {
				lockPaths = append(lockPaths, item.SourcePath)
			}
		}
		sourceLocks, err := lockManagedSources(lockPaths)
		if err != nil {
			return err
		}
		defer sourceLocks.Close()
		return restoreSourceCopy(manifest, target, cmd.Bool("force"), env)
	}
	manifest, runDir, err := manifestForResumeCommand(cmd)
	if err != nil {
		return err
	}
	if cmd.Args().Len() > 1 {
		return fmt.Errorf("unexpected argument %q", cmd.Args().Get(1))
	}
	opts, err := executeOptions(cmd, env)
	if err != nil {
		return err
	}
	runner, err := agent.NewRunner(opts.ToolName, agent.Config{CodexPath: opts.CodexPath, ClaudePath: opts.ClaudePath})
	if err != nil {
		return err
	}
	opts.Runner = runner
	opts.FilePath = manifest.WorkingFile
	opts.OutputDir = manifest.OutputDir
	if opts.OutputDir == "" {
		opts.OutputDir = runDir
	}
	opts.TaskDir = manifest.TaskDir
	if opts.TaskDir == "" {
		opts.TaskDir = filepath.Join(runDir, "tasks")
	}
	opts.WorkdirRoot = manifest.StartWorkdir
	workspace := &managedRunWorkspace{manifest: manifest, runDir: runDir}
	for _, entry := range append([]runManifestSource{{
		SourcePath:    manifest.SourcePath,
		SourceRelPath: manifest.SourceRelPath,
		SourceCopy:    manifest.SourceCopy,
		HiddenSource:  manifest.HiddenSource,
		WorkingFile:   manifest.WorkingFile,
	}}, manifest.Imports...) {
		workspace.sources = append(workspace.sources, entry)
	}
	if err := workspace.acquireLocks(); err != nil {
		return err
	}
	if err := workspace.hideSources(); err != nil {
		_ = workspace.restoreSources()
		_ = workspace.releaseLocks()
		return err
	}
	workspace.manifest.Status = "running"
	if err := workspace.writeManifest(); err != nil {
		_ = workspace.restoreSources()
		_ = workspace.releaseLocks()
		return err
	}
	_ = updateRunIndex(workspace.manifest)
	fmt.Fprintf(env.Stderr, "atm resume started\n")
	fmt.Fprintf(env.Stderr, "run: %s\n", workspace.manifest.RunID)
	fmt.Fprintf(env.Stderr, "source hidden during execution: %s\n", workspace.manifest.SourcePath)
	fmt.Fprintf(env.Stderr, "working file: %s\n", workspace.manifest.WorkingFile)
	runErr := engine.Run(ctx, opts)
	status := "succeeded"
	if runErr != nil {
		status = "failed"
		if ctx.Err() != nil {
			status = "interrupted"
		}
	}
	finishErr := workspace.finish(status, runErr)
	if runErr != nil {
		fmt.Fprintf(env.Stderr, "atm resume %s\n", status)
		fmt.Fprintf(env.Stderr, "source restored unchanged: %s\n", workspace.manifest.SourcePath)
		fmt.Fprintf(env.Stderr, "partial result: %s\n", workspace.manifest.ResultFile)
		fmt.Fprintf(env.Stderr, "resume with:\n  %s\n", workspace.manifest.ResumeCommand)
		if finishErr != nil {
			return fmt.Errorf("%w; additionally failed to finish run workspace: %v", runErr, finishErr)
		}
		return runErr
	}
	if finishErr != nil {
		return finishErr
	}
	fmt.Fprintf(env.Stderr, "atm resume finished: %s\n", status)
	fmt.Fprintf(env.Stderr, "source restored unchanged: %s\n", workspace.manifest.SourcePath)
	fmt.Fprintf(env.Stderr, "result document: %s\n", workspace.manifest.ResultFile)
	fmt.Fprintf(env.Stderr, "artifacts: %s\n", workspace.runDir)
	return nil
}

func manifestAndTargetForRestoreCommand(cmd *urfavecli.Command) (runManifest, string, error) {
	switch cmd.Args().Len() {
	case 0:
		manifest, _, err := selectLatestRunCopy(defaultRestoreProject(cmd), cmd.String("source"))
		return manifest, "", err
	case 1:
		manifest, _, err := loadRunManifest(cmd.Args().Get(0))
		if err == nil {
			return manifest, "", nil
		}
		manifest, _, latestErr := selectLatestRunCopy(defaultRestoreProject(cmd), cmd.String("source"))
		if latestErr != nil {
			return runManifest{}, "", fmt.Errorf("load run %q: %w; also failed to select latest project copy: %v", cmd.Args().Get(0), err, latestErr)
		}
		return manifest, cmd.Args().Get(0), nil
	case 2:
		manifest, _, err := loadRunManifest(cmd.Args().Get(0))
		if err != nil {
			return runManifest{}, "", err
		}
		return manifest, cmd.Args().Get(1), nil
	default:
		return runManifest{}, "", fmt.Errorf("too many arguments for --restore-source")
	}
}

func defaultRestoreProject(cmd *urfavecli.Command) string {
	if project := cmd.String("project"); project != "" {
		return project
	}
	wd, err := os.Getwd()
	if err != nil {
		return ""
	}
	return wd
}

func manifestForResumeCommand(cmd *urfavecli.Command) (runManifest, string, error) {
	if cmd.Bool("last") || cmd.String("project") != "" || cmd.String("source") != "" {
		if cmd.Args().Len() > 0 {
			return runManifest{}, "", fmt.Errorf("resume filters cannot be combined with positional arguments")
		}
		project := cmd.String("project")
		if project == "" && cmd.String("source") == "" {
			wd, _ := os.Getwd()
			project = wd
		}
		return selectLastRun(project, cmd.String("source"))
	}
	if cmd.Args().Len() == 0 {
		return runManifest{}, "", errors.New("no run id specified")
	}
	return loadRunManifest(cmd.Args().Get(0))
}
