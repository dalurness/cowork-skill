package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"
)

var planCmd = &cobra.Command{
	Use:   "plan",
	Short: "Plan file management",
}

var planCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a feature plan file",
	RunE: func(cmd *cobra.Command, args []string) error {
		projectDir, _ := cmd.Flags().GetString("project")
		if projectDir == "" {
			projectDir = "."
		}
		feature, _ := cmd.Flags().GetString("feature")

		featuresDir := filepath.Join(projectDir, "plan", "features")
		if err := os.MkdirAll(featuresDir, 0o755); err != nil {
			return err
		}

		featurePath := filepath.Join(featuresDir, feature+".md")
		if _, err := os.Stat(featurePath); err == nil {
			// Already exists, just return the path
			fmt.Println(featurePath)
			return nil
		}

		if err := os.WriteFile(featurePath, []byte(fmt.Sprintf("# %s\n", feature)), 0o644); err != nil {
			return err
		}

		fmt.Println(featurePath)
		return nil
	},
}

var planSnapshotCmd = &cobra.Command{
	Use:   "snapshot",
	Short: "Save a versioned copy of a feature plan",
	RunE: func(cmd *cobra.Command, args []string) error {
		projectDir, _ := cmd.Flags().GetString("project")
		if projectDir == "" {
			projectDir = "."
		}
		feature, _ := cmd.Flags().GetString("feature")

		historyDir := filepath.Join(projectDir, "history")
		if err := os.MkdirAll(historyDir, 0o755); err != nil {
			return err
		}

		src := filepath.Join(projectDir, "plan", "features", feature+".md")
		data, err := os.ReadFile(src)
		if err != nil {
			return fmt.Errorf("reading feature plan: %w", err)
		}

		ts := time.Now().UTC().Format("20060102T150405")
		dst := filepath.Join(historyDir, fmt.Sprintf("%s-plan-%s.md", ts, feature))

		if err := os.WriteFile(dst, data, 0o644); err != nil {
			return err
		}

		fmt.Printf("Snapshot saved to %s\n", dst)
		return nil
	},
}

var planIntegrateCmd = &cobra.Command{
	Use:   "integrate",
	Short: "Record that a question has been integrated into a feature plan",
	RunE: func(cmd *cobra.Command, args []string) error {
		projectDir, _ := cmd.Flags().GetString("project")
		if projectDir == "" {
			projectDir = "."
		}
		questionID, _ := cmd.Flags().GetString("question-id")
		// feature flag is accepted but not used for file content changes
		// (the agent handles content changes before calling this)

		// Archive the question/decision pair
		historyDir := filepath.Join(projectDir, "history")
		if err := os.MkdirAll(historyDir, 0o755); err != nil {
			return err
		}

		ts := time.Now().UTC().Format("20060102T150405")

		qSrc := filepath.Join(projectDir, "questions", questionID+".md")
		if _, err := os.Stat(qSrc); err == nil {
			qDst := filepath.Join(historyDir, fmt.Sprintf("%s-%s.md", ts, questionID))
			if err := os.Rename(qSrc, qDst); err != nil {
				return fmt.Errorf("archiving question: %w", err)
			}
		}

		dSrc := filepath.Join(projectDir, "decisions", questionID+".md")
		if _, err := os.Stat(dSrc); err == nil {
			dDst := filepath.Join(historyDir, fmt.Sprintf("%s-DECISION-%s.md", ts, questionID))
			if err := os.Rename(dSrc, dDst); err != nil {
				return fmt.Errorf("archiving decision: %w", err)
			}
		}

		fmt.Printf("Integrated %s into plan\n", questionID)
		return nil
	},
}

func init() {
	planCreateCmd.Flags().String("project", ".", "Project root directory")
	planCreateCmd.Flags().String("feature", "", "Feature name")
	planCreateCmd.MarkFlagRequired("feature")

	planSnapshotCmd.Flags().String("project", ".", "Project root directory")
	planSnapshotCmd.Flags().String("feature", "", "Feature name")
	planSnapshotCmd.MarkFlagRequired("feature")

	planIntegrateCmd.Flags().String("project", ".", "Project root directory")
	planIntegrateCmd.Flags().String("question-id", "", "Question ID")
	planIntegrateCmd.Flags().String("feature", "", "Feature name")
	planIntegrateCmd.MarkFlagRequired("question-id")
	planIntegrateCmd.MarkFlagRequired("feature")

	planCmd.AddCommand(planCreateCmd)
	planCmd.AddCommand(planSnapshotCmd)
	planCmd.AddCommand(planIntegrateCmd)
}
