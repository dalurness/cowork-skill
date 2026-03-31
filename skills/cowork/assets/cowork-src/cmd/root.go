package cmd

import (
	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:          "cowork",
	Short:        "Async project manager — deterministic backbone for AI-driven projects",
	SilenceUsage: true, // don't print flag list on every error
}

func Execute() error {
	return rootCmd.Execute()
}

func init() {
	rootCmd.AddCommand(runCmd)
	rootCmd.AddCommand(taskCmd)
	rootCmd.AddCommand(queueCmd)
	rootCmd.AddCommand(questionCmd)
	rootCmd.AddCommand(decisionCmd)
	rootCmd.AddCommand(planCmd)
	rootCmd.AddCommand(logCmd)
	rootCmd.AddCommand(doneCmd)
	rootCmd.AddCommand(inboxCmd)
	rootCmd.AddCommand(outboxCmd)
}
