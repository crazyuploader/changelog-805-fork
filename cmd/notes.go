// Copyright The SigNoz Authors
// SPDX-License-Identifier: Apache-2.0

package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"

	"github.com/spf13/cobra"

	"github.com/SigNoz/changelog/internal/chlog"
	"github.com/SigNoz/changelog/internal/config"
	"github.com/SigNoz/changelog/internal/ghapi"
)

func notesCmd() *cobra.Command {
	var ver, prev, target string
	cmd := &cobra.Command{
		Use:   "notes",
		Short: "Prints the GitHub release body: the CHANGELOG section, Contributors and the compare link",
		RunE: func(cmd *cobra.Command, _ []string) error {
			b, err := os.ReadFile(filepath.Clean(globalCfg.ChangeLogs[config.DefaultChangeLogKey]))
			if err != nil {
				return err
			}
			section, err := releaseSection(string(b), ver)
			if err != nil {
				return err
			}
			if prev == "" {
				out, err := exec.CommandContext(cmd.Context(), "git", "tag", "--merged", "HEAD", "--list", globalCfg.TagPrefix+"*").Output()
				if err != nil {
					return fmt.Errorf("list git tags: %w", err)
				}
				tags := slices.DeleteFunc(strings.Fields(string(out)), func(t string) bool { return t == ver })
				if prev, err = chlog.LatestStable(tags, globalCfg.TagPrefix); err != nil {
					return err
				}
			}
			var footer string
			if globalCfg.Repo != "" {
				c, err := ghapi.FromEnv().ReleaseContributors(cmd.Context(), globalCfg.Repo, ver, prev, target)
				if err != nil {
					cmd.PrintErrf("WARN: no Contributors section, GitHub did not answer (%v)\n", err)
				}
				footer = contributorsFooter(c, globalCfg.Repo, prev, ver)
			}
			cmd.Print(section + footer)
			return nil
		},
	}
	cmd.Flags().StringVar(&ver, "version", "", "the release version, such as v0.148.0")
	cmd.Flags().StringVar(&prev, "previous", "", "the previous release tag. Default: the latest stable tag")
	cmd.Flags().StringVar(&target, "target", "", "the commit the release points at, when the tag does not exist yet")
	if err := cmd.MarkFlagRequired("version"); err != nil {
		panic(err) // the flag is defined above; this cannot fail
	}
	return cmd
}

// releaseSection returns the body under "## <version>" up to the next "## " heading.
func releaseSection(changelog, version string) (string, error) {
	head := "## " + version + "\n"
	i := strings.Index(changelog, head)
	if i < 0 {
		return "", fmt.Errorf("CHANGELOG has no %q section", strings.TrimSpace(head))
	}
	body := changelog[i+len(head):]
	if j := strings.Index(body, "\n## "); j >= 0 {
		body = body[:j+1]
	}
	return strings.TrimSpace(body) + "\n", nil
}

// contributorsFooter renders the Contributors section from TDD Appendix C.2.
func contributorsFooter(c ghapi.Contributors, repo, prev, ver string) string {
	var b strings.Builder
	if len(c.Authors) > 0 {
		b.WriteString("\n### Contributors\n\n")
		names := make([]string, len(c.Authors))
		for i, a := range c.Authors {
			names[i] = "@" + a
		}
		switch n := len(names); {
		case n == 1:
			fmt.Fprintf(&b, "Thanks to %s.\n", names[0])
		case n <= 3:
			fmt.Fprintf(&b, "Thanks to %s and %s.\n", strings.Join(names[:n-1], ", "), names[n-1])
		default:
			fmt.Fprintf(&b, "Thanks to %s and %d others.\n", strings.Join(names[:3], ", "), n-3)
		}
		for _, login := range c.NewOrder {
			fmt.Fprintf(&b, "New: @%s made their first contribution in #%s.\n", login, c.FirstPRs[login])
		}
	}
	fmt.Fprintf(&b, "\n**Full Changelog**: https://github.com/%s/compare/%s...%s\n", repo, prev, ver)
	return b.String()
}
