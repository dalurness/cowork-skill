package cmd

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

var inboxCmd = &cobra.Command{
	Use:   "inbox",
	Short: "Inbox management (human → orchestrator)",
}

// timestampNow returns a filename-safe timestamp: YYYYMMDD-HHmmss
func timestampNow() string {
	return time.Now().UTC().Format("20060102-150405")
}

// firstLine returns the first non-empty line of s, or "(no content)" if empty.
func firstLine(s string) string {
	scanner := bufio.NewScanner(strings.NewReader(s))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line != "" {
			return line
		}
	}
	return "(no content)"
}

// moveToHistory moves src into <projectDir>/history/ with the given destName.
func moveToHistory(projectDir, src, destName string) error {
	histDir := filepath.Join(projectDir, "history")
	if err := os.MkdirAll(histDir, 0o755); err != nil {
		return fmt.Errorf("creating history/: %w", err)
	}
	dest := filepath.Join(histDir, destName)
	return os.Rename(src, dest)
}

// ----------------------------------------------------------------
// inbox add
// ----------------------------------------------------------------

var inboxAddCmd = &cobra.Command{
	Use:   "add <message>",
	Short: "Add a message to the inbox",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		projectDir, _ := cmd.Flags().GetString("project")
		if projectDir == "" {
			projectDir = "."
		}

		inboxDir := filepath.Join(projectDir, "inbox")
		if err := os.MkdirAll(inboxDir, 0o755); err != nil {
			return fmt.Errorf("creating inbox/: %w", err)
		}

		filename := timestampNow() + ".md"
		path := filepath.Join(inboxDir, filename)

		if err := os.WriteFile(path, []byte(args[0]+"\n"), 0o644); err != nil {
			return fmt.Errorf("writing inbox file: %w", err)
		}

		fmt.Println(path)
		return nil
	},
}

// ----------------------------------------------------------------
// inbox list
// ----------------------------------------------------------------

var inboxListCmd = &cobra.Command{
	Use:   "list",
	Short: "List pending inbox items",
	RunE: func(cmd *cobra.Command, args []string) error {
		projectDir, _ := cmd.Flags().GetString("project")
		if projectDir == "" {
			projectDir = "."
		}

		inboxDir := filepath.Join(projectDir, "inbox")
		entries, err := os.ReadDir(inboxDir)
		if err != nil {
			if os.IsNotExist(err) {
				fmt.Println("Inbox is empty")
				return nil
			}
			return fmt.Errorf("reading inbox/: %w", err)
		}

		var files []os.DirEntry
		for _, e := range entries {
			if !e.IsDir() && strings.HasSuffix(e.Name(), ".md") {
				files = append(files, e)
			}
		}

		if len(files) == 0 {
			fmt.Println("Inbox is empty")
			return nil
		}

		for _, f := range files {
			path := filepath.Join(inboxDir, f.Name())
			data, err := os.ReadFile(path)
			if err != nil {
				fmt.Printf("%s — (unreadable)\n", f.Name())
				continue
			}
			fmt.Printf("%s — %s\n", filepath.Join("inbox", f.Name()), firstLine(string(data)))
		}
		return nil
	},
}

// ----------------------------------------------------------------
// inbox handle
// ----------------------------------------------------------------

var inboxHandleCmd = &cobra.Command{
	Use:   "handle <item-path>",
	Short: "Mark an inbox item as handled (moves to history/)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		projectDir, _ := cmd.Flags().GetString("project")
		if projectDir == "" {
			projectDir = "."
		}

		itemPath := args[0]
		// Resolve absolute path: if not absolute, treat as relative to project dir
		if !filepath.IsAbs(itemPath) {
			itemPath = filepath.Join(projectDir, itemPath)
		}

		filename := filepath.Base(itemPath)
		destName := timestampNow() + "-inbox-" + filename

		if err := moveToHistory(projectDir, itemPath, destName); err != nil {
			return fmt.Errorf("moving to history/: %w", err)
		}

		fmt.Printf("Handled: %s → history/\n", args[0])
		return nil
	},
}

// ----------------------------------------------------------------
// inbox respond
// ----------------------------------------------------------------

var inboxRespondCmd = &cobra.Command{
	Use:   "respond <item-path> <answer>",
	Short: "Respond to an inbox item (writes outbox, moves original to history/)",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		projectDir, _ := cmd.Flags().GetString("project")
		if projectDir == "" {
			projectDir = "."
		}

		itemPath := args[0]
		answer := args[1]

		// Resolve absolute path
		absItem := itemPath
		if !filepath.IsAbs(absItem) {
			absItem = filepath.Join(projectDir, absItem)
		}

		origFilename := filepath.Base(absItem)
		ts := timestampNow()

		// Write outbox response
		outboxDir := filepath.Join(projectDir, "outbox")
		if err := os.MkdirAll(outboxDir, 0o755); err != nil {
			return fmt.Errorf("creating outbox/: %w", err)
		}

		outboxFilename := ts + ".md"
		outboxContent := fmt.Sprintf("# Re: %s\n\n%s\n", origFilename, answer)
		outboxPath := filepath.Join(outboxDir, outboxFilename)
		if err := os.WriteFile(outboxPath, []byte(outboxContent), 0o644); err != nil {
			return fmt.Errorf("writing outbox file: %w", err)
		}

		// Move original inbox item to history
		destName := ts + "-inbox-" + origFilename
		if err := moveToHistory(projectDir, absItem, destName); err != nil {
			return fmt.Errorf("moving inbox item to history/: %w", err)
		}

		fmt.Println("Response written to outbox/, original moved to history/")
		return nil
	},
}

func init() {
	for _, sub := range []*cobra.Command{inboxAddCmd, inboxListCmd, inboxHandleCmd, inboxRespondCmd} {
		sub.Flags().String("project", ".", "Project root directory")
	}

	inboxCmd.AddCommand(inboxAddCmd)
	inboxCmd.AddCommand(inboxListCmd)
	inboxCmd.AddCommand(inboxHandleCmd)
	inboxCmd.AddCommand(inboxRespondCmd)
}
