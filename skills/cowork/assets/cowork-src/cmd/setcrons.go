package cmd

import (
	"cowork/state"
	"fmt"
	"path/filepath"

	"github.com/spf13/cobra"
)

var setCronsCmd = &cobra.Command{
	Use:   "set-crons --project <dir> <id1> [id2 ...]",
	Short: "Register cron IDs to cancel when the project completes",
	Long: `Register one or more cron job IDs with a project.

When cowork detects project completion (queue empty, no remaining questions),
it automatically cancels all registered crons via 'openclaw cron delete <id>'.

This is a PUT operation — calling set-crons again replaces all previously
registered IDs with the new list.

To clear all registered crons:
  cowork set-crons --project ./my-project  (no IDs = clears the list)`,
	RunE: setCronsMain,
}

func init() {
	setCronsCmd.Flags().String("project", ".", "Project root directory")
	rootCmd.AddCommand(setCronsCmd)
}

func setCronsMain(cmd *cobra.Command, args []string) error {
	projectDir, _ := cmd.Flags().GetString("project")
	absProject, err := filepath.Abs(projectDir)
	if err != nil {
		return fmt.Errorf("resolving project path: %w", err)
	}
	projectDir = absProject

	s, err := state.Load(projectDir)
	if err != nil {
		return fmt.Errorf("loading state: %w", err)
	}

	previous := s.CronIDs

	// PUT semantics: replace entirely with the new list (args may be empty = clear)
	s.CronIDs = args

	if err := state.Save(projectDir, s); err != nil {
		return fmt.Errorf("saving state: %w", err)
	}

	if len(previous) > 0 {
		fmt.Printf("Replaced %d previous cron ID(s)\n", len(previous))
	}

	if len(s.CronIDs) == 0 {
		fmt.Println("Cron IDs cleared.")
	} else {
		fmt.Printf("Registered %d cron ID(s) for auto-cancel on completion:\n", len(s.CronIDs))
		for _, id := range s.CronIDs {
			fmt.Printf("  %s\n", id)
		}
	}

	return nil
}
