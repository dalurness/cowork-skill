package cmd

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/spf13/cobra"
)

// queueTaskRe matches the heading format written by cowork queue add: ### [TASK-XXX] ...
var queueTaskRe = regexp.MustCompile(`^#{1,3}\s+\[(TASK-\d+)\]`)

var queueCmd = &cobra.Command{
	Use:   "queue",
	Short: "Queue management",
}

func todoPath(projectDir string) string {
	return filepath.Join(projectDir, "queue", "todo.md")
}

func ensureQueueDir(projectDir string) error {
	return os.MkdirAll(filepath.Join(projectDir, "queue"), 0o755)
}

func formatQueueEntry(taskID, priority string) string {
	if priority == "" {
		priority = "normal"
	}
	// Use heading format so the worker-manager regex (^#{1,3}\s+\[(TASK-\d+)\]) can parse it
	return fmt.Sprintf("### [%s] (%s)", taskID, priority)
}

var queueAddCmd = &cobra.Command{
	Use:   "add",
	Short: "Add a task to the queue",
	RunE: func(cmd *cobra.Command, args []string) error {
		projectDir, _ := cmd.Flags().GetString("project")
		if projectDir == "" {
			projectDir = "."
		}
		taskID, _ := cmd.Flags().GetString("task")
		priority, _ := cmd.Flags().GetString("priority")
		top, _ := cmd.Flags().GetBool("top")

		if err := ensureQueueDir(projectDir); err != nil {
			return err
		}

		entry := formatQueueEntry(taskID, priority)
		p := todoPath(projectDir)

		existing, _ := os.ReadFile(p)
		lines := strings.Split(string(existing), "\n")

		// Remove trailing empty lines
		for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
			lines = lines[:len(lines)-1]
		}

		if top {
			lines = append([]string{entry}, lines...)
		} else {
			lines = append(lines, entry)
		}

		content := strings.Join(lines, "\n") + "\n"
		return os.WriteFile(p, []byte(content), 0o644)
	},
}

var queueRemoveCmd = &cobra.Command{
	Use:   "remove",
	Short: "Remove a task from the queue",
	RunE: func(cmd *cobra.Command, args []string) error {
		projectDir, _ := cmd.Flags().GetString("project")
		if projectDir == "" {
			projectDir = "."
		}
		taskID, _ := cmd.Flags().GetString("task")

		p := todoPath(projectDir)
		data, err := os.ReadFile(p)
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}

		var kept []string
		scanner := bufio.NewScanner(strings.NewReader(string(data)))
		for scanner.Scan() {
			line := scanner.Text()
			if !strings.Contains(line, taskID) {
				kept = append(kept, line)
			}
		}

		content := strings.Join(kept, "\n")
		if !strings.HasSuffix(content, "\n") && content != "" {
			content += "\n"
		}
		return os.WriteFile(p, []byte(content), 0o644)
	},
}

var queueListCmd = &cobra.Command{
	Use:   "list",
	Short: "List queued task IDs",
	RunE: func(cmd *cobra.Command, args []string) error {
		projectDir, _ := cmd.Flags().GetString("project")
		if projectDir == "" {
			projectDir = "."
		}

		p := todoPath(projectDir)
		data, err := os.ReadFile(p)
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}

		scanner := bufio.NewScanner(strings.NewReader(string(data)))
		for scanner.Scan() {
			line := scanner.Text()
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			// Extract task ID from "### [TASK-XXX] ..." heading format
			if m := queueTaskRe.FindStringSubmatch(line); m != nil {
				fmt.Println(m[1])
			}
		}
		return nil
	},
}

func init() {
	queueAddCmd.Flags().String("project", ".", "Project root directory")
	queueAddCmd.Flags().String("task", "", "Task ID")
	queueAddCmd.Flags().String("priority", "normal", "Priority: high|normal|low")
	queueAddCmd.Flags().Bool("top", false, "Insert at top of queue")
	queueAddCmd.MarkFlagRequired("task")

	queueRemoveCmd.Flags().String("project", ".", "Project root directory")
	queueRemoveCmd.Flags().String("task", "", "Task ID")
	queueRemoveCmd.MarkFlagRequired("task")

	queueListCmd.Flags().String("project", ".", "Project root directory")

	queueCmd.AddCommand(queueAddCmd)
	queueCmd.AddCommand(queueRemoveCmd)
	queueCmd.AddCommand(queueListCmd)
}
