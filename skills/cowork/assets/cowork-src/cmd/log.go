package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"
)

var logCmd = &cobra.Command{
	Use:   "log",
	Short: "Run logging",
}

var logRunCmd = &cobra.Command{
	Use:   "run",
	Short: "Write a run log entry",
	RunE: func(cmd *cobra.Command, args []string) error {
		projectDir, _ := cmd.Flags().GetString("project")
		if projectDir == "" {
			projectDir = "."
		}
		summary, _ := cmd.Flags().GetString("summary")

		// Write JSON log
		logDir := filepath.Join(projectDir, "log")
		if err := os.MkdirAll(logDir, 0o755); err != nil {
			return err
		}

		now := time.Now().UTC()
		ts := now.Format("2006-01-02T150405")

		logEntry := map[string]interface{}{
			"timestamp": now.Format(time.RFC3339),
			"summary":   summary,
		}

		logData, err := json.MarshalIndent(logEntry, "", "  ")
		if err != nil {
			return err
		}

		// Determine run number from state for filename
		runNum := 0
		stateData, err := os.ReadFile(filepath.Join(projectDir, "state.json"))
		if err == nil {
			var s map[string]interface{}
			if json.Unmarshal(stateData, &s) == nil {
				if rc, ok := s["runCount"].(float64); ok {
					runNum = int(rc)
				}
			}
		}

		logFile := filepath.Join(logDir, fmt.Sprintf("%s-run-%03d.json", ts, runNum))
		if err := os.WriteFile(logFile, append(logData, '\n'), 0o644); err != nil {
			return err
		}

		// Append to updates/YYYY-MM-DD.md
		updatesDir := filepath.Join(projectDir, "updates")
		if err := os.MkdirAll(updatesDir, 0o755); err != nil {
			return err
		}

		updateFile := filepath.Join(updatesDir, now.Format("2006-01-02")+".md")
		f, err := os.OpenFile(updateFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
		if err != nil {
			return err
		}
		defer f.Close()

		entry := fmt.Sprintf("\n## %s\n\n%s\n", now.Format("15:04"), summary)
		if _, err := f.WriteString(entry); err != nil {
			return err
		}

		fmt.Printf("Log written: %s\n", logFile)
		return nil
	},
}

func init() {
	logRunCmd.Flags().String("project", ".", "Project root directory")
	logRunCmd.Flags().String("summary", "", "Run summary")
	logRunCmd.MarkFlagRequired("summary")

	logCmd.AddCommand(logRunCmd)
}
