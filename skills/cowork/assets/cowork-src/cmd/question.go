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

var questionCmd = &cobra.Command{
	Use:   "question",
	Short: "Question management",
}

var questionCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a new question",
	RunE: func(cmd *cobra.Command, args []string) error {
		projectDir, _ := cmd.Flags().GetString("project")
		if projectDir == "" {
			projectDir = "."
		}
		question, _ := cmd.Flags().GetString("question")
		options, _ := cmd.Flags().GetString("options")
		context, _ := cmd.Flags().GetString("context")
		recommendation, _ := cmd.Flags().GetString("recommendation")

		s, err := state.Load(projectDir)
		if err != nil {
			return err
		}

		s.QuestionCounter++
		qID := state.FormatQuestionID(s.QuestionCounter)

		if err := state.Save(projectDir, s); err != nil {
			return err
		}

		questionsDir := filepath.Join(projectDir, "questions")
		if err := os.MkdirAll(questionsDir, 0o755); err != nil {
			return err
		}

		now := time.Now().UTC().Format(time.RFC3339)
		var b strings.Builder
		b.WriteString(fmt.Sprintf("# %s\n\n", qID))
		b.WriteString(fmt.Sprintf("**Asked:** %s\n", now))
		b.WriteString(fmt.Sprintf("**Asked by:** orchestrator (run %d)\n", s.RunCount))
		b.WriteString("**Status:** open\n\n")
		b.WriteString("## Question\n")
		b.WriteString(question + "\n\n")
		b.WriteString("## Options\n")
		b.WriteString(options + "\n")
		if context != "" {
			b.WriteString("\n## Context\n")
			b.WriteString(context + "\n")
		}
		if recommendation != "" {
			b.WriteString("\n## Recommendation\n")
			b.WriteString(recommendation + "\n")
		}

		qPath := filepath.Join(questionsDir, qID+".md")
		if err := os.WriteFile(qPath, []byte(b.String()), 0o644); err != nil {
			return err
		}

		fmt.Println(qID)
		return nil
	},
}

var questionListCmd = &cobra.Command{
	Use:   "list",
	Short: "List open questions",
	RunE: func(cmd *cobra.Command, args []string) error {
		projectDir, _ := cmd.Flags().GetString("project")
		if projectDir == "" {
			projectDir = "."
		}

		unanswered, _ := cmd.Flags().GetBool("unanswered")

		questionsDir := filepath.Join(projectDir, "questions")
		entries, err := os.ReadDir(questionsDir)
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}

		decisionsDir := filepath.Join(projectDir, "decisions")

		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
				continue
			}

			if unanswered {
				decisionPath := filepath.Join(decisionsDir, e.Name())
				if _, err := os.Stat(decisionPath); err == nil {
					continue // has a matching decision, skip
				}
			}

			qID := strings.TrimSuffix(e.Name(), ".md")
			// Read question text for display
			data, err := os.ReadFile(filepath.Join(questionsDir, e.Name()))
			if err != nil {
				fmt.Println(qID)
				continue
			}
			// Extract question text
			questionText := extractSection(string(data), "## Question")
			if questionText != "" {
				fmt.Printf("%s: %s\n", qID, strings.TrimSpace(questionText))
			} else {
				fmt.Println(qID)
			}
		}
		return nil
	},
}

func extractSection(content, header string) string {
	idx := strings.Index(content, header)
	if idx == -1 {
		return ""
	}
	rest := content[idx+len(header):]
	// Find next ## header or end
	nextHeader := strings.Index(rest, "\n## ")
	if nextHeader != -1 {
		rest = rest[:nextHeader]
	}
	return strings.TrimSpace(rest)
}

var questionArchiveCmd = &cobra.Command{
	Use:   "archive",
	Short: "Archive a question and its decision",
	RunE: func(cmd *cobra.Command, args []string) error {
		projectDir, _ := cmd.Flags().GetString("project")
		if projectDir == "" {
			projectDir = "."
		}
		id, _ := cmd.Flags().GetString("id")

		historyDir := filepath.Join(projectDir, "history")
		if err := os.MkdirAll(historyDir, 0o755); err != nil {
			return err
		}

		ts := time.Now().UTC().Format("20060102T150405")

		// Move question file
		qSrc := filepath.Join(projectDir, "questions", id+".md")
		if _, err := os.Stat(qSrc); err == nil {
			qDst := filepath.Join(historyDir, fmt.Sprintf("%s-%s.md", ts, id))
			if err := os.Rename(qSrc, qDst); err != nil {
				return fmt.Errorf("archiving question: %w", err)
			}
		}

		// Move decision file
		dSrc := filepath.Join(projectDir, "decisions", id+".md")
		if _, err := os.Stat(dSrc); err == nil {
			dDst := filepath.Join(historyDir, fmt.Sprintf("%s-DECISION-%s.md", ts, id))
			if err := os.Rename(dSrc, dDst); err != nil {
				return fmt.Errorf("archiving decision: %w", err)
			}
		}

		return nil
	},
}

func init() {
	questionCreateCmd.Flags().String("project", ".", "Project root directory")
	questionCreateCmd.Flags().String("question", "", "The question text")
	questionCreateCmd.Flags().String("options", "", "Available options")
	questionCreateCmd.Flags().String("context", "", "Relevant context")
	questionCreateCmd.Flags().String("recommendation", "", "Recommended option")
	questionCreateCmd.MarkFlagRequired("question")
	questionCreateCmd.MarkFlagRequired("options")

	questionListCmd.Flags().String("project", ".", "Project root directory")
	questionListCmd.Flags().Bool("unanswered", false, "Only show unanswered questions")

	questionArchiveCmd.Flags().String("project", ".", "Project root directory")
	questionArchiveCmd.Flags().String("id", "", "Question ID (e.g. QUESTION-001)")
	questionArchiveCmd.MarkFlagRequired("id")

	questionCmd.AddCommand(questionCreateCmd)
	questionCmd.AddCommand(questionListCmd)
	questionCmd.AddCommand(questionArchiveCmd)
}
