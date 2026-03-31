package cmd

import (
	"cowork/state"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
)

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Initialize a new cowork project",
	Long: `Initialize a new cowork project workspace.

If --from is provided, copies all files from that directory into the project
root before scaffolding, freezing the initial state so the project is
self-contained and not dependent on an external directory.

If --from is not provided, initializes in place (project directory must
already contain OVERVIEW.md).

Examples:
  cowork init --project ./my-project --from ./my-notes
  cowork init --project ./existing-dir-with-overview`,
	RunE: func(cmd *cobra.Command, args []string) error {
		projectDir, _ := cmd.Flags().GetString("project")
		fromDir, _ := cmd.Flags().GetString("from")

		// Resolve to absolute paths
		absProject, err := filepath.Abs(projectDir)
		if err != nil {
			return fmt.Errorf("resolving project path: %w", err)
		}
		projectDir = absProject

		// Create project dir if it doesn't exist
		if err := os.MkdirAll(projectDir, 0o755); err != nil {
			return fmt.Errorf("creating project directory: %w", err)
		}

		// Copy overview directory contents if --from provided
		if fromDir != "" {
			absFrom, err := filepath.Abs(fromDir)
			if err != nil {
				return fmt.Errorf("resolving --from path: %w", err)
			}
			if err := copyDir(absFrom, projectDir); err != nil {
				return fmt.Errorf("copying overview directory: %w", err)
			}
			fmt.Printf("Copied overview files from %s\n", absFrom)
		}

		// Validate OVERVIEW.md exists
		overviewPath := filepath.Join(projectDir, "OVERVIEW.md")
		if _, err := os.Stat(overviewPath); err != nil {
			return fmt.Errorf("OVERVIEW.md not found in %s — create it before running init", projectDir)
		}

		// Check if already initialized
		stateFile := filepath.Join(projectDir, "state.json")
		if _, err := os.Stat(stateFile); err == nil {
			return fmt.Errorf("project already initialized (state.json exists) — remove it to reinitialize")
		}

		// Create workspace directories
		dirs := []string{
			"work",
			"queue",
			"plan/features",
			"log",
			"history",
			"updates",
			"questions",
			"decisions",
		}

		for _, d := range dirs {
			full := filepath.Join(projectDir, d)
			if err := os.MkdirAll(full, 0o755); err != nil {
				return fmt.Errorf("creating %s: %w", d, err)
			}
		}

		// Write empty queue/todo.md
		todoPath := filepath.Join(projectDir, "queue", "todo.md")
		if err := os.WriteFile(todoPath, []byte(""), 0o644); err != nil {
			return fmt.Errorf("creating queue/todo.md: %w", err)
		}

		// Initialize state.json
		if err := state.Save(projectDir, state.DefaultState()); err != nil {
			return fmt.Errorf("writing state.json: %w", err)
		}

		fmt.Printf("Initialized cowork project at %s\n", projectDir)
		fmt.Printf("\nDirectories created:\n")
		for _, d := range dirs {
			fmt.Printf("  %s/\n", d)
		}
		fmt.Printf("\nNext steps:\n")
		fmt.Printf("  1. Verify your agent CLI — workers default to 'claude'.\n")
		fmt.Printf("     Edit cmd/workers.go if you're using codex, opencode, etc.\n")
		fmt.Printf("\n  2. Schedule run crons (fires the worker/orchestrator loop):\n")
		fmt.Printf("     timeout 3600 cowork run --project %s --skill-dir <skill-dir> --forever\n", projectDir)
		fmt.Printf("\n  3. Schedule a question-check cron (recommended, every 2-3 hours):\n")
		fmt.Printf("     Workers raise questions for blocking decisions — without a question-check\n")
		fmt.Printf("     cron, questions may sit unnoticed until the next run cron fires.\n")
		fmt.Printf("     See SKILL.md step 4 for the cron setup command.\n")
		fmt.Printf("\n  4. Run manually to seed the first batch of tasks:\n")
		fmt.Printf("     cowork run --project %s --skill-dir <skill-dir>\n", projectDir)

		return nil
	},
}

// copyDir copies all files from src into dst, preserving subdirectory structure.
// Skips any cowork-managed directories if they already exist in src.
func copyDir(src, dst string) error {
	// Directories that belong to cowork — don't copy from src if present
	coworkDirs := map[string]bool{
		"work": true, "queue": true, "plan": true, "log": true,
		"history": true, "updates": true, "questions": true, "decisions": true,
	}

	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}

		// Skip the source root itself
		if rel == "." {
			return nil
		}

		// Skip cowork-managed directories from source
		topLevel := rel
		if idx := len(rel); idx > 0 {
			for i, c := range rel {
				if c == '/' || c == os.PathSeparator {
					topLevel = rel[:i]
					break
				}
			}
		}
		if info.IsDir() && coworkDirs[topLevel] {
			return filepath.SkipDir
		}

		target := filepath.Join(dst, rel)

		if info.IsDir() {
			return os.MkdirAll(target, info.Mode())
		}

		return copyFile(path, target, info.Mode())
	})
}

// copyFile copies a single file from src to dst.
func copyFile(src, dst string, mode os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, in)
	return err
}

func init() {
	initCmd.Flags().String("project", ".", "Project root directory to initialize")
	initCmd.Flags().String("from", "", "Source directory to copy into the project (optional)")

	rootCmd.AddCommand(initCmd)
}
