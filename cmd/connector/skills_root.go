package main

import (
	"fmt"
	"strings"

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
	legacyCodexHome := ""
	var allowedSkills []string

	cmd := &cobra.Command{
		Use:   "sync",
		Short: "Sync bundled skills into ~/.agents/skills",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			aliceHome, err := cmd.Root().PersistentFlags().GetString("alice-home")
			if err != nil {
				return err
			}
			aliceHome = strings.TrimSpace(aliceHome)
			if aliceHome == "" {
				aliceHome = config.AliceHomeDir()
			} else {
				aliceHome = config.ResolveAliceHomeDir(aliceHome)
			}
			report, err := bootstrap.EnsureBundledSkillsLinkedForAliceHome(aliceHome, allowedSkills)
			if err != nil {
				return err
			}
			_, err = fmt.Fprintf(
				cmd.OutOrStdout(),
				"bundled skills synced alice_home=%s source_root=%s agents_skills=%s claude_skills=%s discovered=%d linked=%d updated=%d unchanged=%d failed=%d\n",
				report.AliceHome,
				report.SourceRoot,
				report.AgentsSkillsDir,
				report.ClaudeSkillsDir,
				report.Discovered,
				report.Linked,
				report.Updated,
				report.Unchanged,
				report.Failed,
			)
			return err
		},
	}
	cmd.Flags().StringVar(&legacyCodexHome, "codex-home", "", "deprecated: bundled skills no longer sync into CODEX_HOME")
	cmd.Flags().StringArrayVar(&allowedSkills, "skill", nil, "limit sync to specific bundled skill names (repeatable)")
	_ = cmd.Flags().MarkDeprecated("codex-home", "bundled skills now sync into ~/.agents/skills (source: <ALICE_HOME>/skills)")
	return cmd
}
