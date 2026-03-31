package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

var decisionCmd = &cobra.Command{
	Use:   "decision",
	Short: "Decision management",
}

var decisionSubmitCmd = &cobra.Command{
	Use:   "submit",
	Short: "Submit a decision for a question",
	RunE: func(cmd *cobra.Command, args []string) error {
		projectDir, _ := cmd.Flags().GetString("project")
		if projectDir == "" {
			projectDir = "."
		}
		questionID, _ := cmd.Flags().GetString("question-id")
		answer, _ := cmd.Flags().GetString("answer")
		rationale, _ := cmd.Flags().GetString("rationale")

		decisionsDir := filepath.Join(projectDir, "decisions")
		if err := os.MkdirAll(decisionsDir, 0o755); err != nil {
			return err
		}

		dPath := filepath.Join(decisionsDir, questionID+".md")
		if _, err := os.Stat(dPath); err == nil {
			return fmt.Errorf("decision for %s already exists", questionID)
		}

		// Derive decision ID from question ID (QUESTION-001 → DECISION-001)
		decisionID := strings.Replace(questionID, "QUESTION", "DECISION", 1)
		now := time.Now().UTC().Format(time.RFC3339)

		var b strings.Builder
		b.WriteString(fmt.Sprintf("# %s (answer to %s)\n\n", decisionID, questionID))
		b.WriteString(fmt.Sprintf("**Submitted:** %s\n", now))
		b.WriteString("**Submitted by:** Dallin (via Nova)\n\n")
		b.WriteString("## Answer\n")
		b.WriteString(answer + "\n\n")
		b.WriteString("## Rationale\n")
		b.WriteString(rationale + "\n")

		if err := os.WriteFile(dPath, []byte(b.String()), 0o644); err != nil {
			return err
		}

		fmt.Printf("%s submitted\n", decisionID)
		return nil
	},
}

var decisionListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all submitted decisions",
	RunE: func(cmd *cobra.Command, args []string) error {
		projectDir, _ := cmd.Flags().GetString("project")
		if projectDir == "" {
			projectDir = "."
		}

		decisionsDir := filepath.Join(projectDir, "decisions")
		entries, err := os.ReadDir(decisionsDir)
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}

		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
				continue
			}
			id := strings.TrimSuffix(e.Name(), ".md")
			fmt.Println(id)
		}
		return nil
	},
}

func init() {
	decisionSubmitCmd.Flags().String("project", ".", "Project root directory")
	decisionSubmitCmd.Flags().String("question-id", "", "Question ID (e.g. QUESTION-001)")
	decisionSubmitCmd.Flags().String("answer", "", "The decision answer")
	decisionSubmitCmd.Flags().String("rationale", "", "Rationale for the decision")
	decisionSubmitCmd.MarkFlagRequired("question-id")
	decisionSubmitCmd.MarkFlagRequired("answer")
	decisionSubmitCmd.MarkFlagRequired("rationale")

	decisionListCmd.Flags().String("project", ".", "Project root directory")

	decisionCmd.AddCommand(decisionSubmitCmd)
	decisionCmd.AddCommand(decisionListCmd)
}
