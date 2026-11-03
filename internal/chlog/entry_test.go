// Copyright The SigNoz Authors
// SPDX-License-Identifier: Apache-2.0

package chlog

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/SigNoz/changelog/internal/config"
)

func TestValidate(t *testing.T) {
	ok := Entry{ChangeType: Fix, Component: "logs", Note: "n", PRs: []string{"#1"}}
	tests := []struct {
		name        string
		edit        func(*Entry)
		deployments []string
		wantErr     string
	}{
		{name: "valid", edit: func(*Entry) {}},
		{name: "cross-repo PR", edit: func(e *Entry) { e.PRs = []string{"SigNoz/signoz-otel-collector#962"} }},
		{name: "old type name", edit: func(e *Entry) { e.ChangeType = "bug_fix" }, wantErr: "not a valid 'change_type'"},
		{name: "unknown component", edit: func(e *Entry) { e.Component = "telemetry" }, wantErr: "not a valid 'component'"},
		{name: "no PRs", edit: func(e *Entry) { e.PRs = nil }, wantErr: "specify one or more 'prs'"},
		{name: "bare number", edit: func(e *Entry) { e.PRs = []string{"123"} }, wantErr: "is not a PR reference"},
		{name: "outside org", edit: func(e *Entry) { e.Related = []string{"open-telemetry/foo#1"} }, wantErr: "is not a PR reference"},
		{name: "breaking without steps", edit: func(e *Entry) { e.ChangeType = Breaking }, wantErr: "needs 'breaking_steps'"},
		{name: "steps on a fix", edit: func(e *Entry) { e.BreakingSteps = "x" }, wantErr: "only for breaking"},
		{name: "deprecation without subtext", edit: func(e *Entry) { e.ChangeType = Deprecation }, wantErr: "needs 'subtext'"},
		{name: "docs outside signoz.io", edit: func(e *Entry) { e.Docs = "https://example.com" }, wantErr: "'docs' must start with"},
		{name: "upgrade stop without docs", edit: func(e *Entry) { e.UpgradeStop = true }, wantErr: "needs a 'docs' link"},
		{name: "deployment missing", edit: func(*Entry) {}, deployments: []string{"cloud", "self-hosted", "both"}, wantErr: "'deployment' must be one of"},
		{name: "deployment set", edit: func(e *Entry) { e.Deployment = "cloud" }, deployments: []string{"cloud", "self-hosted", "both"}},
		{name: "deployment in a repo without it", edit: func(e *Entry) { e.Deployment = "cloud" }, wantErr: "does not use 'deployment'"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := config.New(t.TempDir())
			cfg.Components = []string{"logs"}
			cfg.Deployments = tt.deployments
			e := ok
			tt.edit(&e)
			err := e.Validate(cfg)
			if tt.wantErr == "" {
				require.NoError(t, err)
				return
			}
			require.ErrorContains(t, err, tt.wantErr)
		})
	}
}

func TestReadEntriesRejectsUnknownKey(t *testing.T) {
	cfg := config.New(t.TempDir())
	require.NoError(t, os.MkdirAll(cfg.EntriesDir, 0o755))
	body := "change_type: breaking\ncomponent: logs\nnote: n\nprs: [\"#1\"]\nbreaking_step: typo\n"
	require.NoError(t, os.WriteFile(filepath.Join(cfg.EntriesDir, "e.yaml"), []byte(body), 0o600))
	_, err := ReadEntries(cfg)
	require.ErrorContains(t, err, "field breaking_step not found")
}

func TestNextVersion(t *testing.T) {
	tags := []string{"v0.146.0", "v0.147.0", "v0.148.0-rc.1", "histogram-quantile/v0.1.0", "v0.9.9"}
	tests := []struct {
		name, preOneFeature string
		tags                []string
		types               []string
		want, wantErr       string
	}{
		{name: "fix gives patch", tags: tags, types: []string{Fix}, want: "v0.147.1"},
		{name: "feature gives minor before 1.0", tags: tags, types: []string{Fix, Feature}, preOneFeature: "minor", want: "v0.148.0"},
		{name: "components rule: feature gives patch", tags: tags, types: []string{Feature}, preOneFeature: "patch", want: "v0.147.1"},
		{name: "breaking gives minor before 1.0", tags: tags, types: []string{Breaking}, preOneFeature: "patch", want: "v0.148.0"},
		{name: "breaking gives major after 1.0", tags: []string{"v1.4.2"}, types: []string{Breaking}, want: "v2.0.0"},
		{name: "feature gives minor after 1.0", tags: []string{"v1.4.2"}, types: []string{Feature}, preOneFeature: "patch", want: "v1.5.0"},
		{name: "only an rc tag", tags: []string{"v1.0.0-rc.1"}, types: []string{Fix}, wantErr: "no stable tag"},
		{name: "no entries", tags: tags, wantErr: "nothing to release"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var entries []*Entry
			for _, ct := range tt.types {
				entries = append(entries, &Entry{ChangeType: ct})
			}
			got, err := NextVersion(tt.tags, "v", tt.preOneFeature, entries)
			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}
