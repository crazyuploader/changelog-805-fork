// Copyright The SigNoz Authors
// SPDX-License-Identifier: Apache-2.0

package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/SigNoz/changelog/internal/config"
)

// setup writes a repo with a CHANGELOG.md and the given entry files, and points globalCfg at it.
func setup(t *testing.T, entries map[string]string) (root string) {
	t.Helper()
	root = t.TempDir()
	cfg := config.New(root)
	cfg.Repo = "SigNoz/signoz"
	cfg.Components = []string{"alerts", "logs", "auth", "exceptions"}
	require.NoError(t, os.MkdirAll(cfg.EntriesDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "CHANGELOG.md"), []byte("# Changelog\n\n<!-- next version -->\n"), 0o600))
	for name, body := range entries {
		require.NoError(t, os.WriteFile(filepath.Join(cfg.EntriesDir, name), []byte(body), 0o600))
	}
	globalCfg = cfg
	t.Cleanup(func() { globalCfg = nil })
	return root
}

func run(args ...string) (string, error) {
	cmd := rootCmd()
	var out strings.Builder
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return out.String(), err
}

func pending(t *testing.T, root string) []string {
	t.Helper()
	files, err := filepath.Glob(filepath.Join(root, ".chloggen", "*.yaml"))
	require.NoError(t, err)
	for i, f := range files {
		files[i] = filepath.Base(f)
	}
	return files
}

func entry(changeType, component, note string, extra ...string) string {
	return "change_type: " + changeType + "\ncomponent: " + component + "\nnote: " + note + "\nprs: [\"#1\"]\n" + strings.Join(extra, "\n")
}

// Upstream dropped 'telemetry' entries and deleted their files. Every type must render.
func TestUpdateRendersEveryType(t *testing.T) {
	root := setup(t, map[string]string{
		"a.yaml": entry("breaking", "auth", "Breaking note.", "breaking_steps: Do this."),
		"b.yaml": entry("deprecation", "alerts", "Deprecation note.", "subtext: Use v2."),
		"c.yaml": entry("feature", "alerts", "Feature note."),
		"d.yaml": entry("improvement", "logs", "Improvement note."),
		"e.yaml": entry("fix", "logs", "Fix note."),
	})
	_, err := run("update", "--version", "v0.150.0")
	require.NoError(t, err)

	got, err := os.ReadFile(filepath.Join(root, "CHANGELOG.md"))
	require.NoError(t, err)
	for _, s := range []string{"### Breaking changes", "### Deprecated", "### Added", "### Changed", "### Fixed",
		"Breaking note.", "Deprecation note.", "Feature note.", "Improvement note.", "Fix note."} {
		require.Contains(t, string(got), s)
	}
	require.Empty(t, pending(t, root))
}

// Upstream 'update --component' wrote one component and deleted every entry file.
func TestUpdateComponentKeepsOtherEntries(t *testing.T) {
	root := setup(t, map[string]string{
		"alerts.yaml": entry("fix", "alerts", "Alerts fix."),
		"logs.yaml":   entry("fix", "logs", "Logs fix."),
	})
	_, err := run("update", "--version", "v1.0.1", "--component", "alerts")
	require.NoError(t, err)

	got, err := os.ReadFile(filepath.Join(root, "CHANGELOG.md"))
	require.NoError(t, err)
	require.Contains(t, string(got), "Alerts fix.")
	require.NotContains(t, string(got), "Logs fix.")
	require.Equal(t, []string{"logs.yaml"}, pending(t, root))
}

// Upstream 'update' skipped validation and dropped an invalid entry with exit 0.
func TestUpdateFailsOnInvalidEntry(t *testing.T) {
	root := setup(t, map[string]string{
		"bad.yaml":  entry("bug_fix", "logs", "Old type name."),
		"good.yaml": entry("fix", "logs", "Good fix."),
	})
	_, err := run("update", "--version", "v1.0.1")
	require.ErrorContains(t, err, "'bug_fix' is not a valid 'change_type'")

	got, err := os.ReadFile(filepath.Join(root, "CHANGELOG.md"))
	require.NoError(t, err)
	require.NotContains(t, string(got), "Good fix.", "nothing is written when any entry is invalid")
	require.Equal(t, []string{"bad.yaml", "good.yaml"}, pending(t, root))
}

// Upstream panicked with "assignment to entry in nil map" on a config without change_logs.
func TestConfigWithoutChangeLogs(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "config.yaml"), []byte("components: [logs]\n"), 0o600))
	cfg, err := config.NewFromFile(root, "config.yaml")
	require.NoError(t, err)
	require.Equal(t, filepath.Join(root, "CHANGELOG.md"), cfg.ChangeLogs[config.DefaultChangeLogKey])
}

// The v0.147.0 example from the TDD (Appendix C.1), plus deployment and docs.
func TestRender(t *testing.T) {
	root := setup(t, map[string]string{
		"12163.yaml": `change_type: breaking
component: alerts
note: Remove the v1 alert rule history endpoints. They now return ` + "`404 Not Found`" + `.
prs: ["#12163"]
security: true
breaking_steps: Use ` + "`GET /api/v2/rules/{id}/history/*`" + ` instead.
`,
		"13074.yaml": `change_type: feature
component: alerts
note: Send alert notifications to Telegram.
subtext: |
  Add a Telegram channel under Settings > Notification Channels.
prs: ["#13074", "#13119"]
`,
		"13204.yaml": `change_type: fix
component: exceptions
note: Keep the filter pills on the Exceptions page when you run the query.
prs: ["#13204"]
related: ["SigNoz/signoz-otel-collector#962"]
`,
	})
	_, err := run("update", "--version", "v0.147.0")
	require.NoError(t, err)

	got, err := os.ReadFile(filepath.Join(root, "CHANGELOG.md"))
	require.NoError(t, err)
	want := "# Changelog\n\n<!-- next version -->\n" + `## v0.147.0

### Security

This release fixes a security issue. Upgrade recommended.

- ` + "`alerts`" + `: Remove the v1 alert rule history endpoints. They now return ` + "`404 Not Found`" + `. See Breaking changes for the steps. ([#12163](https://github.com/SigNoz/signoz/pull/12163))

### Breaking changes

- ` + "`alerts`" + `: Remove the v1 alert rule history endpoints. They now return ` + "`404 Not Found`" + `. ([#12163](https://github.com/SigNoz/signoz/pull/12163))

  **What to do:** Use ` + "`GET /api/v2/rules/{id}/history/*`" + ` instead.

### Added

- ` + "`alerts`" + `: Send alert notifications to Telegram. ([#13074](https://github.com/SigNoz/signoz/pull/13074), [#13119](https://github.com/SigNoz/signoz/pull/13119))
  Add a Telegram channel under Settings > Notification Channels.

### Fixed

- ` + "`exceptions`" + `: Keep the filter pills on the Exceptions page when you run the query. ([#13204](https://github.com/SigNoz/signoz/pull/13204), related [SigNoz/signoz-otel-collector#962](https://github.com/SigNoz/signoz-otel-collector/pull/962))

`
	require.Equal(t, want, string(got))
}

func TestUpgradeStopBannerAndDeployment(t *testing.T) {
	root := setup(t, map[string]string{
		"stop.yaml": entry("breaking", "auth", "Login sessions use opaque tokens.",
			"breaking_steps: Set SIGNOZ_TOKENIZER_JWT_SECRET.",
			"deployment: self-hosted",
			"upgrade_stop: true",
			"docs: https://signoz.io/docs/operate/migration/upgrade-0-143"),
	})
	globalCfg.Deployments = []string{"cloud", "self-hosted", "both"}
	_, err := run("update", "--version", "v0.143.0")
	require.NoError(t, err)

	got, err := os.ReadFile(filepath.Join(root, "CHANGELOG.md"))
	require.NoError(t, err)
	require.Contains(t, string(got), "> **Mandatory upgrade stop.** Upgrade to v0.143.0 before any later version.\n> Read the [upgrade guide](https://signoz.io/docs/operate/migration/upgrade-0-143) first.")
	require.Contains(t, string(got), "Login sessions use opaque tokens. [Self-Hosted] ([#1](https://github.com/SigNoz/signoz/pull/1)) · [Docs](https://signoz.io/docs/operate/migration/upgrade-0-143)")
}
