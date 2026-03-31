package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

var doneCmd = &cobra.Command{
	Use:   "done",
	Short: "Mark task complete and archive",
}

var doneAddCmd = &cobra.Command{
	Use:   "add",
	Short: "Mark a task as done",
	RunE: func(cmd *cobra.Command, args []string) error {
		projectDir, _ := cmd.Flags().GetString("project")
		if projectDir == "" {
			projectDir = "."
		}
		taskID, _ := cmd.Flags().GetString("task")
		summary, _ := cmd.Flags().GetString("summary")

		now := time.Now().UTC()

		// Append to done.md
		donePath := filepath.Join(projectDir, "done.md")
		f, err := os.OpenFile(donePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
		if err != nil {
			return fmt.Errorf("opening done.md: %w", err)
		}
		defer f.Close()

		entry := fmt.Sprintf("\n## %s — %s\n\n%s\n", taskID, now.Format(time.RFC3339), summary)
		if _, err := f.WriteString(entry); err != nil {
			return err
		}

		// Archive worker files to history/
		if err := archiveTaskFiles(projectDir, taskID, now); err != nil {
			return err
		}

		// Remove from queue
		removeFromQueue(projectDir, taskID)

		fmt.Printf("%s marked as done\n", taskID)
		return nil
	},
}

func archiveTaskFiles(projectDir, taskID string, ts time.Time) error {
	historyDir := filepath.Join(projectDir, "history")
	if err := os.MkdirAll(historyDir, 0o755); err != nil {
		return err
	}

	taskDir := filepath.Join(projectDir, "work", taskID)
	timestamp := ts.Format("20060102T150405")

	// Files to archive (leave BRIEF.md in place)
	filesToArchive := []string{"OUTPUT.md", "SPEC.md", "PROGRESS.md", "HANDOFF.md"}

	for _, fname := range filesToArchive {
		src := filepath.Join(taskDir, fname)
		if _, err := os.Stat(src); err != nil {
			continue // File doesn't exist, skip
		}

		baseName := strings.TrimSuffix(fname, ".md")
		dst := filepath.Join(historyDir, fmt.Sprintf("%s-%s-%s.md", timestamp, taskID, baseName))

		data, err := os.ReadFile(src)
		if err != nil {
			return fmt.Errorf("reading %s: %w", fname, err)
		}
		if err := os.WriteFile(dst, data, 0o644); err != nil {
			return fmt.Errorf("writing archived %s: %w", fname, err)
		}
		os.Remove(src)
	}

	return nil
}

func removeFromQueue(projectDir, taskID string) {
	p := filepath.Join(projectDir, "queue", "todo.md")
	data, err := os.ReadFile(p)
	if err != nil {
		return
	}

	var kept []string
	for _, line := range strings.Split(string(data), "\n") {
		if !strings.Contains(line, taskID) {
			kept = append(kept, line)
		}
	}
	content := strings.Join(kept, "\n")
	os.WriteFile(p, []byte(content), 0o644)
}

func init() {
	doneAddCmd.Flags().String("project", ".", "Project root directory")
	doneAddCmd.Flags().String("task", "", "Task ID")
	doneAddCmd.Flags().String("summary", "", "Summary of what was produced")
	doneAddCmd.MarkFlagRequired("task")
	doneAddCmd.MarkFlagRequired("summary")

	doneCmd.AddCommand(doneAddCmd)
}
