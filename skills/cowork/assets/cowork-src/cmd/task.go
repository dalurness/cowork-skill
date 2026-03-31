package cmd

import (
	"cowork/state"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

var taskCmd = &cobra.Command{
	Use:   "task",
	Short: "Task management",
}

var taskCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a new task",
	RunE: func(cmd *cobra.Command, args []string) error {
		projectDir, _ := cmd.Flags().GetString("project")
		if projectDir == "" {
			projectDir = "."
		}

		title, _ := cmd.Flags().GetString("title")
		mode, _ := cmd.Flags().GetString("mode")
		scope, _ := cmd.Flags().GetString("scope")
		outOfScope, _ := cmd.Flags().GetString("out-of-scope")
		context, _ := cmd.Flags().GetString("context")
		output, _ := cmd.Flags().GetString("output")
		priority, _ := cmd.Flags().GetString("priority")
		blockedBy, _ := cmd.Flags().GetString("blocked-by")
		decisions, _ := cmd.Flags().GetString("decisions")

		s, err := state.Load(projectDir)
		if err != nil {
			return err
		}

		s.TaskCounter++
		taskID := state.FormatTaskID(s.TaskCounter)

		if err := state.Save(projectDir, s); err != nil {
			return err
		}

		taskDir := filepath.Join(projectDir, "work", taskID)
		if err := os.MkdirAll(taskDir, 0o755); err != nil {
			return fmt.Errorf("creating task directory: %w", err)
		}

		brief := buildBrief(taskID, title, mode, scope, outOfScope, context, output, priority, blockedBy, decisions)
		briefPath := filepath.Join(taskDir, "BRIEF.md")
		if err := os.WriteFile(briefPath, []byte(brief), 0o644); err != nil {
			return fmt.Errorf("writing BRIEF.md: %w", err)
		}

		fmt.Println(taskID)
		return nil
	},
}

func buildBrief(taskID, title, mode, scope, outOfScope, context, output, priority, blockedBy, decisions string) string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("# %s — %s\n\n", taskID, title))
	b.WriteString(fmt.Sprintf("**Task ID:** %s\n", taskID))
	b.WriteString(fmt.Sprintf("**Working directory:** work/%s/\n", taskID))
	b.WriteString("**Project root:** ../../\n")
	b.WriteString(fmt.Sprintf("**Mode:** %s\n", mode))
	if priority != "" {
		b.WriteString(fmt.Sprintf("**Priority:** %s\n", priority))
	} else {
		b.WriteString("**Priority:** normal\n")
	}
	b.WriteString(fmt.Sprintf("**Created:** %s\n", time.Now().UTC().Format(time.RFC3339)))
	if blockedBy != "" {
		b.WriteString(fmt.Sprintf("**Blocked by:** %s\n", blockedBy))
	}
	b.WriteString("\n## Scope\n")
	b.WriteString(scope + "\n")
	if outOfScope != "" {
		b.WriteString("\n## Out of Scope\n")
		b.WriteString(outOfScope + "\n")
	}
	if context != "" {
		b.WriteString("\n## Context\n")
		b.WriteString(context + "\n")
	}
	if decisions != "" {
		b.WriteString("\n## Decisions\n")
		b.WriteString(decisions + "\n")
	}
	b.WriteString("\n## Expected Output\n")
	b.WriteString(output + "\n")
	return b.String()
}

var taskProgressCmd = &cobra.Command{
	Use:   "progress",
	Short: "Append a progress entry to a task",
	RunE: func(cmd *cobra.Command, args []string) error {
		projectDir, _ := cmd.Flags().GetString("project")
		if projectDir == "" {
			projectDir = "."
		}
		taskID, _ := cmd.Flags().GetString("task")
		message, _ := cmd.Flags().GetString("message")

		progressPath := filepath.Join(projectDir, "work", taskID, "PROGRESS.md")

		now := time.Now().UTC()
		entry := fmt.Sprintf("[%s] %s\n", now.Format("15:04"), message)

		f, err := os.OpenFile(progressPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
		if err != nil {
			return fmt.Errorf("opening PROGRESS.md: %w", err)
		}
		defer f.Close()

		if _, err := f.WriteString(entry); err != nil {
			return fmt.Errorf("writing progress entry: %w", err)
		}
		return nil
	},
}

var taskHandoffCmd = &cobra.Command{
	Use:   "handoff",
	Short: "Write or overwrite HANDOFF.md for a task",
	RunE: func(cmd *cobra.Command, args []string) error {
		projectDir, _ := cmd.Flags().GetString("project")
		if projectDir == "" {
			projectDir = "."
		}
		taskID, _ := cmd.Flags().GetString("task")
		content, _ := cmd.Flags().GetString("content")

		handoffPath := filepath.Join(projectDir, "work", taskID, "HANDOFF.md")
		if err := os.WriteFile(handoffPath, []byte(content+"\n"), 0o644); err != nil {
			return fmt.Errorf("writing HANDOFF.md: %w", err)
		}
		return nil
	},
}

var taskOutputCmd = &cobra.Command{
	Use:   "output",
	Short: "Signal task completion by copying output file",
	RunE: func(cmd *cobra.Command, args []string) error {
		projectDir, _ := cmd.Flags().GetString("project")
		if projectDir == "" {
			projectDir = "."
		}
		taskID, _ := cmd.Flags().GetString("task")
		filePath, _ := cmd.Flags().GetString("file")

		data, err := os.ReadFile(filePath)
		if err != nil {
			return fmt.Errorf("reading output file: %w", err)
		}

		outputPath := filepath.Join(projectDir, "work", taskID, "OUTPUT.md")
		if err := os.WriteFile(outputPath, data, 0o644); err != nil {
			return fmt.Errorf("writing OUTPUT.md: %w", err)
		}

		fmt.Printf("Output saved to %s\n", outputPath)
		return nil
	},
}

func init() {
	// task create flags
	taskCreateCmd.Flags().String("project", ".", "Project root directory")
	taskCreateCmd.Flags().String("title", "", "Short descriptive title")
	taskCreateCmd.Flags().String("mode", "implementation", "Task mode: research|spec|implementation")
	taskCreateCmd.Flags().String("scope", "", "What this task covers")
	taskCreateCmd.Flags().String("out-of-scope", "", "What to skip")
	taskCreateCmd.Flags().String("context", "", "Relevant context files")
	taskCreateCmd.Flags().String("output", "", "What to produce")
	taskCreateCmd.Flags().String("priority", "", "Priority: high|normal|low")
	taskCreateCmd.Flags().String("blocked-by", "", "Task ID this is blocked by")
	taskCreateCmd.Flags().String("decisions", "", "Relevant decisions")
	taskCreateCmd.MarkFlagRequired("title")
	taskCreateCmd.MarkFlagRequired("scope")
	taskCreateCmd.MarkFlagRequired("output")

	// task progress flags
	taskProgressCmd.Flags().String("project", ".", "Project root directory")
	taskProgressCmd.Flags().String("task", "", "Task ID (e.g. TASK-001)")
	taskProgressCmd.Flags().String("message", "", "Progress message")
	taskProgressCmd.MarkFlagRequired("task")
	taskProgressCmd.MarkFlagRequired("message")

	// task handoff flags
	taskHandoffCmd.Flags().String("project", ".", "Project root directory")
	taskHandoffCmd.Flags().String("task", "", "Task ID")
	taskHandoffCmd.Flags().String("content", "", "Handoff content")
	taskHandoffCmd.MarkFlagRequired("task")
	taskHandoffCmd.MarkFlagRequired("content")

	// task output flags
	taskOutputCmd.Flags().String("project", ".", "Project root directory")
	taskOutputCmd.Flags().String("task", "", "Task ID")
	taskOutputCmd.Flags().String("file", "", "Path to output file")
	taskOutputCmd.MarkFlagRequired("task")
	taskOutputCmd.MarkFlagRequired("file")

	taskCmd.AddCommand(taskCreateCmd)
	taskCmd.AddCommand(taskProgressCmd)
	taskCmd.AddCommand(taskHandoffCmd)
	taskCmd.AddCommand(taskOutputCmd)
}
