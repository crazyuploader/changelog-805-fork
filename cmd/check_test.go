// Copyright The SigNoz Authors
// SPDX-License-Identifier: Apache-2.0

package cmd

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"

	"github.com/SigNoz/changelog/internal/chlog"
	"github.com/SigNoz/changelog/internal/ghapi"
)

// fakeGitHub answers /pulls/<n> with 200 for the listed numbers, 404 otherwise, and 500 for "500".
func fakeGitHub(t *testing.T, existing ...string) *ghapi.Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:]
		switch {
		case n == "500":
			w.WriteHeader(http.StatusInternalServerError)
		case slices.Contains(existing, n):
			w.WriteHeader(http.StatusOK)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return &ghapi.Client{BaseURL: srv.URL, HTTP: srv.Client()}
}

func TestCheckPR(t *testing.T) {
	good := entry("feature", "alerts", "Send alert notifications to Telegram.")
	tests := []struct {
		name         string
		title        string
		files        []changedFile
		entries      map[string]string
		noSkip       string
		wantErr      string
		wantWarnings int
	}{
		{name: "entry added", title: "feat(alerts): support telegram", entries: map[string]string{"t.yaml": good},
			files: []changedFile{{"A", ".chloggen/t.yaml"}}},
		{name: "no entry", title: "feat(alerts): support telegram", wantErr: "adds no changelog entry"},
		{name: "skip title", title: "chore(deps): bump x"},
		{name: "refactor, style and build titles skip", title: "build(deps): Bump golang.org/x/net"},
		{name: "perf needs an entry", title: "perf(logs): faster query", wantErr: "adds no changelog entry"},
		{name: "revert needs an entry", title: "revert: feat(alerts): telegram", wantErr: "adds no changelog entry"},
		{name: "breaking title never skips", title: "chore!: drop v1 api", wantErr: "'!' title is a breaking change"},
		{name: "codegen title does not skip", title: "chore(codegen): regenerate", noSkip: `^chore\(codegen\)`, wantErr: "adds no changelog entry"},
		{name: "hand edit with skip title", title: "docs: fix typo", files: []changedFile{{"M", "CHANGELOG.md"}}, wantErr: "written by the release PR"},
		{name: "no prs: update fills them in", title: "feat: x", entries: map[string]string{"t.yaml": strings.Replace(good, `["#1"]`, `[]`, 1)},
			files: []changedFile{{"A", ".chloggen/t.yaml"}}},
		{name: "explicit prs must exist", title: "feat: x", entries: map[string]string{"t.yaml": strings.Replace(good, `"#1"`, `"#404"`, 1)},
			files: []changedFile{{"A", ".chloggen/t.yaml"}}, wantErr: "#404 is not a pull request"},
		{name: "edit of an older entry keeps its PR", title: "fix: wording", entries: map[string]string{"t.yaml": strings.Replace(good, `"#1"`, `"#7"`, 1)},
			files: []changedFile{{"M", ".chloggen/t.yaml"}}},
		{name: "note copies the title", title: "feat(alerts): Send alert notifications to Telegram", entries: map[string]string{"t.yaml": good},
			files: []changedFile{{"A", ".chloggen/t.yaml"}}, wantErr: "copies the PR title"},
		{name: "related PR does not exist", title: "feat: x", entries: map[string]string{"t.yaml": good + "related: [\"SigNoz/signoz-otel-collector#404\"]\n"},
			files: []changedFile{{"A", ".chloggen/t.yaml"}}, wantErr: "is not a pull request in SigNoz/signoz-otel-collector"},
		{name: "GitHub down warns and passes", title: "feat: x", entries: map[string]string{"t.yaml": good + "related: [\"SigNoz/signoz-otel-collector#500\"]\n"},
			files: []changedFile{{"A", ".chloggen/t.yaml"}}, wantWarnings: 1},
		{name: "invalid entry", title: "feat: x", entries: map[string]string{"t.yaml": entry("bug_fix", "alerts", "n")},
			files: []changedFile{{"A", ".chloggen/t.yaml"}}, wantErr: "not a valid 'change_type'"},
		{name: "a bad pending entry on main does not fail another PR", title: "feat: x",
			entries: map[string]string{"t.yaml": good, "old.yaml": entry("fix", "traces", "Old."), "broken.yaml": "unknown_key: 1\n"},
			files:   []changedFile{{"A", ".chloggen/t.yaml"}}},
		{name: "a config change checks every pending entry", title: "chore: drop the traces component",
			entries: map[string]string{"old.yaml": entry("fix", "traces", "Old.")},
			files:   []changedFile{{"M", ".chloggen/config.yaml"}}, wantErr: "traces is not a valid 'component'"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setup(t, tt.entries)
			globalCfg.NoSkipTitlePattern = tt.noSkip
			globalCfg.ConfigYAML = filepath.Join(globalCfg.EntriesDir, "config.yaml")
			cmd := &cobra.Command{}
			cmd.SetContext(context.Background())
			res, err := checkPR(cmd, tt.files, 1, tt.title, fakeGitHub(t, "1", "7"))
			require.Len(t, res.warnings, tt.wantWarnings)
			if tt.wantErr == "" {
				require.NoError(t, err)
				return
			}
			require.ErrorContains(t, err, tt.wantErr)
		})
	}
}

func TestCheckSkipLabel(t *testing.T) {
	setup(t, nil)
	out, err := run("check", "--pr", "1", "--title", "feat: x", "--labels", "bug,skip-changelog")
	require.NoError(t, err)
	require.Contains(t, out, "skips every rule")
}

func TestDocsCheck(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/live" {
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()
	entries := []*chlog.Entry{
		{File: "live.yaml", ChangeType: chlog.Breaking, Docs: srv.URL + "/live"},
		{File: "fix.yaml", ChangeType: chlog.Fix, Docs: srv.URL + "/gone"},
		{File: "stop.yaml", ChangeType: chlog.Fix, UpgradeStop: true, Docs: srv.URL + "/gone"},
		{File: "breaking.yaml", ChangeType: chlog.Breaking, Docs: srv.URL + "/gone"},
	}
	warnings, err := checkDocs(context.Background(), srv.Client(), entries)
	require.Equal(t, []string{"fix.yaml: " + srv.URL + "/gone returns HTTP 404"}, warnings)
	require.ErrorContains(t, err, "stop.yaml")
	require.ErrorContains(t, err, "breaking.yaml")
	require.NotContains(t, err.Error(), "live.yaml")
}

// A fix shipped from a release branch is in the history after the port PR. The next release must not repeat it.
func TestUpdateSkipsReleasedEntries(t *testing.T) {
	root := setup(t, map[string]string{
		"backport.yaml": strings.Replace(entry("fix", "logs", "Fixed on the branch."), `"#1"`, `"#42"`, 1),
		"new.yaml":      entry("fix", "logs", "New fix."),
	})
	history := "# Changelog\n\n<!-- next version -->\n## v0.147.1\n\n### Fixed\n\n- `logs`: Fixed on the branch. ([#42](https://github.com/SigNoz/signoz/pull/42))\n"
	require.NoError(t, os.WriteFile(filepath.Join(root, "CHANGELOG.md"), []byte(history), 0o600))

	out, err := run("update", "--version", "v0.148.0")
	require.NoError(t, err)
	require.Contains(t, out, "Skipped backport.yaml")

	got, err := os.ReadFile(filepath.Join(root, "CHANGELOG.md"))
	require.NoError(t, err)
	require.Equal(t, 1, strings.Count(string(got), "Fixed on the branch."))
	require.Contains(t, string(got), "New fix.")
	require.Empty(t, pending(t, root))
}
