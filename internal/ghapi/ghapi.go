// Copyright The SigNoz Authors
// SPDX-License-Identifier: Apache-2.0

// Package ghapi is the small GitHub REST client the checks and notes use.
package ghapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"slices"
	"strings"
	"time"
)

// Client calls the GitHub REST API.
type Client struct {
	BaseURL string
	Token   string
	HTTP    *http.Client
}

// FromEnv builds a client from GITHUB_API_URL and GITHUB_TOKEN, which GitHub Actions sets.
func FromEnv() *Client {
	base := os.Getenv("GITHUB_API_URL")
	if base == "" {
		base = "https://api.github.com"
	}
	return &Client{BaseURL: strings.TrimSuffix(base, "/"), Token: os.Getenv("GITHUB_TOKEN"), HTTP: &http.Client{Timeout: 15 * time.Second}}
}

// ErrNotFound means GitHub answered 404.
var ErrNotFound = errors.New("not found")

func (c *Client) do(ctx context.Context, method, path string, in, out any) error {
	var body io.Reader
	if in != nil {
		b, err := json.Marshal(in)
		if err != nil {
			return err
		}
		body = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.BaseURL+path, body)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }() // read-only body; a close error changes nothing
	switch {
	case resp.StatusCode == http.StatusNotFound:
		return fmt.Errorf("%s %s: %w", method, path, ErrNotFound)
	case resp.StatusCode >= 300:
		return fmt.Errorf("%s %s: HTTP %d", method, path, resp.StatusCode)
	}
	if out == nil {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

// PRExists reports whether repo has pull request n. It returns ErrNotFound when it does not.
// Any other error means GitHub could not answer.
func (c *Client) PRExists(ctx context.Context, repo, n string) error {
	return c.do(ctx, http.MethodGet, fmt.Sprintf("/repos/%s/pulls/%s", repo, n), nil, nil)
}

var (
	authorLine = regexp.MustCompile(`(?m)^\* .+ by @(\S+) in https://\S+$`)
	newLine    = regexp.MustCompile(`(?m)^\* @(\S+) made their first contribution in https://\S+/pull/(\d+)$`)
)

// Contributors holds the people in a release.
type Contributors struct {
	// Authors are sorted by PR count, most first. Bots are left out.
	Authors []string
	// FirstPRs maps a first-time contributor to their first PR number.
	FirstPRs map[string]string
	// NewOrder keeps the order GitHub lists first-time contributors in.
	NewOrder []string
}

// ReleaseContributors asks GitHub's generate-notes API who contributed between prevTag and target.
// The tag does not need to exist yet.
func (c *Client) ReleaseContributors(ctx context.Context, repo, tag, prevTag, target string) (Contributors, error) {
	in := map[string]string{"tag_name": tag, "previous_tag_name": prevTag}
	if target != "" {
		in["target_commitish"] = target
	}
	var out struct {
		Body string `json:"body"`
	}
	if err := c.do(ctx, http.MethodPost, "/repos/"+repo+"/releases/generate-notes", in, &out); err != nil {
		return Contributors{}, err
	}
	return parseNotes(out.Body), nil
}

func parseNotes(body string) Contributors {
	counts := map[string]int{}
	for _, m := range authorLine.FindAllStringSubmatch(body, -1) {
		if !strings.HasSuffix(m[1], "[bot]") {
			counts[m[1]]++
		}
	}
	c := Contributors{FirstPRs: map[string]string{}}
	for login := range counts {
		c.Authors = append(c.Authors, login)
	}
	slices.SortFunc(c.Authors, func(a, b string) int {
		if d := counts[b] - counts[a]; d != 0 {
			return d
		}
		return strings.Compare(a, b)
	})
	for _, m := range newLine.FindAllStringSubmatch(body, -1) {
		if !strings.HasSuffix(m[1], "[bot]") {
			c.FirstPRs[m[1]] = m[2]
			c.NewOrder = append(c.NewOrder, m[1])
		}
	}
	return c
}
