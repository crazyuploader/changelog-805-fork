// Copyright The SigNoz Authors
// SPDX-License-Identifier: Apache-2.0

package cmd

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"github.com/SigNoz/changelog/internal/chlog"
)

func docsCheckCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "docs-check",
		Short: "Fetches each pending entry's docs link. A 404 on a breaking or upgrade-stop entry fails",
		RunE: func(cmd *cobra.Command, _ []string) error {
			entriesByChangelog, err := chlog.ReadEntries(globalCfg)
			if err != nil {
				return err
			}
			warnings, err := checkDocs(cmd.Context(), &http.Client{Timeout: 15 * time.Second}, chlog.Unique(entriesByChangelog))
			for _, w := range warnings {
				cmd.Printf("WARN: %s\n", w)
			}
			if err != nil {
				return err
			}
			cmd.Println("PASS: docs links checked")
			return nil
		},
	}
}

// checkDocs fails only where a missing page blocks users: breaking and upgrade-stop entries.
func checkDocs(ctx context.Context, client *http.Client, entries []*chlog.Entry) (warnings []string, errs error) {
	for _, e := range entries {
		if e.Docs == "" {
			continue
		}
		name := filepath.Base(e.File)
		status, err := fetchStatus(ctx, client, e.Docs)
		switch {
		case err != nil:
			warnings = append(warnings, fmt.Sprintf("%s: could not fetch %s (%v)", name, e.Docs, err))
		case status == http.StatusNotFound && (e.ChangeType == chlog.Breaking || e.UpgradeStop):
			errs = errors.Join(errs, fmt.Errorf("%s: %s returns 404. Publish the page before the release", name, e.Docs))
		case status >= 400:
			warnings = append(warnings, fmt.Sprintf("%s: %s returns HTTP %d", name, e.Docs, status))
		}
	}
	return warnings, errs
}

func fetchStatus(ctx context.Context, client *http.Client, url string) (int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return 0, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	_ = resp.Body.Close() // only the status matters
	return resp.StatusCode, nil
}
