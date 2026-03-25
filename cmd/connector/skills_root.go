package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/Alice-space/alice/internal/bootstrap"
	"github.com/Alice-space/alice/internal/config"
)

func newSkillsCmd() *cobra.Command {
	skillsCmd := &cobra.Command{
		Use:   "skills",
		Short: "Manage bundled Alice skills",
		Args:  cobra.NoArgs,
	}
	skillsCmd.AddCommand(newSkillsSyncCmd())
	return skillsCmd
}

func newSkillsSyncCmd() *cobra.Command {
	codexHome := ""
	var allowedSkills []string

	cmd := &cobra.Command{
		Use:   "sync",
		Short: "Sync bundled skills into CODEX_HOME",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			report, err := bootstrap.EnsureBundledSkillsLinkedForCodexHome(config.ResolveCodexHomeDir(codexHome), allowedSkills)
			if err != nil {
				return err
			}
			_, err = fmt.Fprintf(
				cmd.OutOrStdout(),
				"bundled skills synced codex_home=%s discovered=%d linked=%d updated=%d unchanged=%d failed=%d\n",
				report.CodexHome,
				report.Discovered,
				report.Linked,
				report.Updated,
				report.Unchanged,
				report.Failed,
			)
			return err
		},
	}
	cmd.Flags().StringVar(&codexHome, "codex-home", "", "target CODEX_HOME (default: $CODEX_HOME or ~/.codex)")
	cmd.Flags().StringArrayVar(&allowedSkills, "skill", nil, "limit sync to specific bundled skill names (repeatable)")
	return cmd
}
