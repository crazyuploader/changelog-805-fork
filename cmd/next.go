// Copyright The SigNoz Authors
// SPDX-License-Identifier: Apache-2.0

package cmd

import (
	"fmt"
	"os/exec"
	"strings"

	"github.com/spf13/cobra"

	"github.com/SigNoz/changelog/internal/chlog"
)

func nextCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "next",
		Short: "Prints the next version from the latest stable tag and the pending entries. Changes nothing",
		RunE: func(cmd *cobra.Command, _ []string) error {
			entriesByChangelog, err := chlog.ReadEntries(globalCfg)
			if err != nil {
				return err
			}
			if err = chlog.ValidateAll(globalCfg, entriesByChangelog); err != nil {
				return err
			}

			// CI needs the tags: checkout with fetch-depth: 0, or run git fetch --tags.
			// Only tags in HEAD's history count, so on release/v0.148 a later v0.149.0 is ignored.
			out, err := exec.CommandContext(cmd.Context(), "git", "tag", "--merged", "HEAD", "--list", globalCfg.TagPrefix+"*").Output()
			if err != nil {
				return fmt.Errorf("list git tags: %w", err)
			}

			next, err := chlog.NextVersion(strings.Fields(string(out)), globalCfg.TagPrefix,
				globalCfg.PreOneFeatureBump, chlog.Unique(entriesByChangelog))
			if err != nil {
				return err
			}
			cmd.Println(next)
			return nil
		},
	}
}
