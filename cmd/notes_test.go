// Copyright The SigNoz Authors
// SPDX-License-Identifier: Apache-2.0

package cmd

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/SigNoz/changelog/internal/ghapi"
)

func TestReleaseSection(t *testing.T) {
	cl := "# Changelog\n\n<!-- next version -->\n## v0.148.0\n\n### Fixed\n\n- a\n\n## v0.147.0\n\n- b\n"
	got, err := releaseSection(cl, "v0.148.0")
	require.NoError(t, err)
	require.Equal(t, "### Fixed\n\n- a\n", got)

	got, err = releaseSection(cl, "v0.147.0")
	require.NoError(t, err)
	require.Equal(t, "- b\n", got)

	_, err = releaseSection(cl, "v0.149.0")
	require.ErrorContains(t, err, "no \"## v0.149.0\" section")
}

// The body shape GitHub's generate-notes API returns.
const generatedNotes = `## What's Changed
* feat(alerts): telegram by @tewarig in https://github.com/SigNoz/signoz/pull/13074
* fix: a by @krsoninikhil in https://github.com/SigNoz/signoz/pull/13100
* fix: b by @krsoninikhil in https://github.com/SigNoz/signoz/pull/13101
* chore(deps): bump by @dependabot[bot] in https://github.com/SigNoz/signoz/pull/13102
* feat: c by @vikrantgupta25 in https://github.com/SigNoz/signoz/pull/13103
* fix: d by @ashu8912 in https://github.com/SigNoz/signoz/pull/13181
* fix: e by @zed in https://github.com/SigNoz/signoz/pull/13182

## New Contributors
* @ashu8912 made their first contribution in https://github.com/SigNoz/signoz/pull/13181

**Full Changelog**: https://github.com/SigNoz/signoz/compare/v0.147.0...v0.148.0`

func TestContributorsFooter(t *testing.T) {
	var gotReq map[string]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/repos/SigNoz/signoz/releases/generate-notes", r.URL.Path)
		require.NoError(t, json.NewDecoder(r.Body).Decode(&gotReq))
		require.NoError(t, json.NewEncoder(w).Encode(map[string]string{"body": generatedNotes}))
	}))
	defer srv.Close()

	gh := &ghapi.Client{BaseURL: srv.URL, HTTP: srv.Client()}
	c, err := gh.ReleaseContributors(context.Background(), "SigNoz/signoz", "v0.148.0", "v0.147.0", "abc123")
	require.NoError(t, err)
	require.Equal(t, map[string]string{"tag_name": "v0.148.0", "previous_tag_name": "v0.147.0", "target_commitish": "abc123"}, gotReq)

	require.Equal(t, `
### Contributors

Thanks to @krsoninikhil, @ashu8912, @tewarig and 2 others.
New: @ashu8912 made their first contribution in #13181.

**Full Changelog**: https://github.com/SigNoz/signoz/compare/v0.147.0...v0.148.0
`, contributorsFooter(c, "SigNoz/signoz", "v0.147.0", "v0.148.0"))

	// GitHub down: the compare link still renders.
	require.Equal(t, "\n**Full Changelog**: https://github.com/SigNoz/signoz/compare/v0.147.0...v0.148.0\n",
		contributorsFooter(ghapi.Contributors{}, "SigNoz/signoz", "v0.147.0", "v0.148.0"))
}
