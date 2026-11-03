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
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/spf13/cobra"

	"github.com/SigNoz/changelog/internal/chlog"
)

const (
	insertPoint = "<!-- next version -->\n"
)

var (
	version         string
	dry             bool
	componentFilter string
)

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
					cmd.Printf("Generated changelog updates for %s:", changeLogKey)
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
	return cmd
}
