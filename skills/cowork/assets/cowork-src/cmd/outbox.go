package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
)

var outboxCmd = &cobra.Command{
	Use:   "outbox",
	Short: "Outbox management (orchestrator → human)",
}

// ----------------------------------------------------------------
// outbox list
// ----------------------------------------------------------------

var outboxListCmd = &cobra.Command{
	Use:   "list",
	Short: "List pending outbox items",
	RunE: func(cmd *cobra.Command, args []string) error {
		projectDir, _ := cmd.Flags().GetString("project")
		if projectDir == "" {
			projectDir = "."
		}

		outboxDir := filepath.Join(projectDir, "outbox")
		entries, err := os.ReadDir(outboxDir)
		if err != nil {
			if os.IsNotExist(err) {
				fmt.Println("Outbox is empty")
				return nil
			}
			return fmt.Errorf("reading outbox/: %w", err)
		}

		var files []os.DirEntry
		for _, e := range entries {
			if !e.IsDir() && strings.HasSuffix(e.Name(), ".md") {
				files = append(files, e)
			}
		}

		if len(files) == 0 {
			fmt.Println("Outbox is empty")
			return nil
		}

		for _, f := range files {
			path := filepath.Join(outboxDir, f.Name())
			data, err := os.ReadFile(path)
			if err != nil {
				fmt.Printf("%s — (unreadable)\n", f.Name())
				continue
			}
			fmt.Printf("%s — %s\n", filepath.Join("outbox", f.Name()), firstLine(string(data)))
		}
		return nil
	},
}

// ----------------------------------------------------------------
// outbox archive
// ----------------------------------------------------------------

var outboxArchiveCmd = &cobra.Command{
	Use:   "archive <item-path>",
	Short: "Archive an outbox item (moves to history/)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		projectDir, _ := cmd.Flags().GetString("project")
		if projectDir == "" {
			projectDir = "."
		}

		itemPath := args[0]
		if !filepath.IsAbs(itemPath) {
			itemPath = filepath.Join(projectDir, itemPath)
		}

		filename := filepath.Base(itemPath)
		destName := timestampNow() + "-outbox-" + filename

		if err := moveToHistory(projectDir, itemPath, destName); err != nil {
			return fmt.Errorf("moving to history/: %w", err)
		}

		fmt.Printf("Archived: %s → history/\n", args[0])
		return nil
	},
}

func init() {
	for _, sub := range []*cobra.Command{outboxListCmd, outboxArchiveCmd} {
		sub.Flags().String("project", ".", "Project root directory")
	}

	outboxCmd.AddCommand(outboxListCmd)
	outboxCmd.AddCommand(outboxArchiveCmd)
}
