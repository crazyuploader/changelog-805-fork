// Copyright The OpenTelemetry Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package cmd

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/spf13/cobra"

	"github.com/SigNoz/changelog/internal/chlog"
	"github.com/SigNoz/changelog/internal/ghapi"
)

const (
	insertPoint = "<!-- next version -->\n"
)

var (
	version         string
	dry             bool
	componentFilter string
	fallbackPR      int
)

// squashSuffix matches the "(#123)" GitHub appends to a squash merge's subject.
var squashSuffix = regexp.MustCompile(`\(#([0-9]+)\)$`)

// fillPRs sets 'prs' on entries that leave it empty. It reads the commit that added the file:
// first the "(#N)" a squash merge puts in its subject, then GitHub's list of PRs for that commit,
// which also covers merge commits and rebase merges. pr is the last resort, for files not merged
// yet (the PR preview).
func fillPRs(cmd *cobra.Command, entries []*chlog.Entry, pr int, gh *ghapi.Client) error {
	var errs error
	for _, e := range entries {
		if len(e.PRs) > 0 {
			continue
		}
		out, err := exec.CommandContext(cmd.Context(), "git", "log", "--diff-filter=A", "--format=%H%x09%s", "-1", "--", e.File).Output()
		sha, subject, _ := strings.Cut(strings.TrimSpace(string(out)), "\t")
		if m := squashSuffix.FindStringSubmatch(subject); err == nil && m != nil {
			e.PRs = []string{"#" + m[1]}
			continue
		}
		var apiErr error
		if err == nil && sha != "" && globalCfg.Repo != "" {
			var n int
			if n, apiErr = gh.CommitPR(cmd.Context(), globalCfg.Repo, sha); apiErr == nil && n > 0 {
				e.PRs = []string{fmt.Sprintf("#%d", n)}
				continue
			}
		}
		switch {
		case pr > 0:
			e.PRs = []string{fmt.Sprintf("#%d", pr)}
		case apiErr != nil:
			errs = errors.Join(errs, fmt.Errorf("%s: no 'prs', and GitHub did not answer for its commit (%w). Rerun, or set 'prs'", filepath.Base(e.File), apiErr))
		default:
			errs = errors.Join(errs, fmt.Errorf("%s: no 'prs', and no PR brought its commit. Set 'prs'", filepath.Base(e.File)))
		}
	}
	return errs
}

// released reports whether every PR of e already appears in the changelog history.
// It needs 'repo' in the config, because a bare "#123" also matches unrelated text.
func released(e *chlog.Entry, history string) bool {
	if globalCfg.Repo == "" {
		return false
	}
	for _, ref := range e.PRs {
		if !strings.Contains(history, chlog.RefLink(globalCfg.Repo, ref)) {
			return false
		}
	}
	return len(e.PRs) > 0
}

func sortedByFile(entries []*chlog.Entry) []*chlog.Entry {
	slices.SortFunc(entries, func(a, b *chlog.Entry) int { return strings.Compare(a.File, b.File) })
	return entries
}

func updateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "update",
		Short: "Updates CHANGELOG.MD to include all new changes",
		RunE: func(cmd *cobra.Command, _ []string) error {
			// A typo such as v0.149 or 0.149.0 in a v-prefixed repo would become a tag. --dry only previews.
			if !dry && !chlog.IsStable(version, globalCfg.TagPrefix) {
				return fmt.Errorf("--version %q is not %sX.Y.Z", version, globalCfg.TagPrefix)
			}
			entriesByChangelog, err := chlog.ReadEntries(globalCfg)
			if err != nil {
				return err
			}
			// Validate before writing anything, so an invalid entry fails the run instead of vanishing.
			if err = chlog.ValidateAll(globalCfg, entriesByChangelog); err != nil {
				return err
			}

			var rendered []*chlog.Entry
			for changeLogKey, entries := range entriesByChangelog {
				slices.SortFunc(entries, func(a, b *chlog.Entry) int {
					return strings.Compare(a.Component, b.Component)
				})

				if componentFilter != "" {
					entries = slices.DeleteFunc(slices.Clone(entries), func(e *chlog.Entry) bool {
						return e.Component != componentFilter
					})
				}
				filename := globalCfg.ChangeLogs[changeLogKey]
				oldChlogBytes, err := os.ReadFile(filepath.Clean(filename))
				if err != nil {
					return err
				}
				chlogParts := bytes.Split(oldChlogBytes, []byte(insertPoint))
				if len(chlogParts) != 2 {
					return fmt.Errorf("expected one instance of %s", insertPoint)
				}

				if err = fillPRs(cmd, entries, fallbackPR, ghapi.FromEnv()); err != nil {
					return err
				}
				// A fix released from a branch is already in the history. Do not repeat it, but delete its file.
				entries = slices.DeleteFunc(entries, func(e *chlog.Entry) bool {
					if released(e, string(chlogParts[1])) {
						cmd.Printf("Skipped %s: its PRs are in an earlier release\n", filepath.Base(e.File))
						rendered = append(rendered, e)
						return true
					}
					return false
				})
				rendered = append(rendered, entries...)

				chlogUpdate, err := chlog.GenerateSummary(version, entries, globalCfg)
				if err != nil {
					return err
				}

				if dry {
					if len(globalCfg.ChangeLogs) > 1 { // name the changelog only when there is more than one
						cmd.Printf("Generated changelog updates for %s:\n\n", changeLogKey)
					}
					cmd.Println(chlogUpdate)
					continue
				}

				chlogHeader, chlogHistory := string(chlogParts[0]), string(chlogParts[1])

				var chlogBuilder strings.Builder
				chlogBuilder.WriteString(chlogHeader)
				chlogBuilder.WriteString(insertPoint)
				chlogBuilder.WriteString(chlogUpdate)
				chlogBuilder.WriteString(chlogHistory)

				tmpMD := filename + ".tmp"
				if err = os.WriteFile(filepath.Clean(tmpMD), []byte(chlogBuilder.String()), 0600); err != nil {
					return err
				}

				if err = os.Rename(tmpMD, filename); err != nil {
					return err
				}

				cmd.Printf("Finished updating %s\n", filename)
			}
			if dry {
				return nil
			}
			// Delete only what was rendered. With --component, other entries stay pending.
			return chlog.DeleteEntries(slices.Compact(sortedByFile(rendered)))
		},
	}
	cmd.Flags().StringVarP(&version, "version", "v", "vTODO", "will be rendered directly into the update text")
	cmd.Flags().BoolVarP(&dry, "dry", "d", false, "will generate the update text and print to stdout")
	cmd.Flags().StringVarP(&componentFilter, "component", "c", "", "only select entries with this exact component")
	cmd.Flags().IntVar(&fallbackPR, "pr", 0, "the PR number for entries not merged yet, for the PR preview")
	return cmd
}
