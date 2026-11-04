// Copyright The SigNoz Authors
// SPDX-License-Identifier: Apache-2.0

package cmd

import (
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/spf13/cobra"

	"github.com/SigNoz/changelog/internal/chlog"
	"github.com/SigNoz/changelog/internal/ghapi"
)

const skipLabel = "skip-changelog"

var (
	breakingTitle = regexp.MustCompile(`^\w+(\([^)]*\))?!: `)
	titlePrefix   = regexp.MustCompile(`^\w+(\([^)]*\))?!?:\s*`)
)

// changedFile is one line of git diff --name-status.
type changedFile struct{ status, path string }

func checkCmd() *cobra.Command {
	var (
		pr     int
		title  string
		labels []string
		base   string
	)
	cmd := &cobra.Command{
		Use:   "check",
		Short: "Checks a pull request's changelog entries. CI runs it on every PR",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if slices.Contains(labels, skipLabel) {
				cmd.Printf("PASS: the %s label skips every rule\n", skipLabel)
				return nil
			}
			out, err := exec.CommandContext(cmd.Context(), "git", "diff", "--name-status", "--no-renames", base+"...HEAD").Output()
			if err != nil {
				return fmt.Errorf("list changed files against %s (CI needs fetch-depth: 0): %w", base, err)
			}
			res, err := checkPR(cmd, parseNameStatus(string(out)), pr, title, ghapi.FromEnv())
			for _, w := range res.warnings {
				cmd.Printf("WARN: %s\n", w)
			}
			if err != nil {
				return err
			}
			cmd.Println("PASS: " + res.pass)
			return nil
		},
	}
	cmd.Flags().IntVar(&pr, "pr", 0, "the pull request number")
	cmd.Flags().StringVar(&title, "title", "", "the pull request title")
	cmd.Flags().StringSliceVar(&labels, "labels", nil, "the pull request labels, comma-separated")
	cmd.Flags().StringVar(&base, "base", "origin/main", "the ref the pull request merges into")
	for _, f := range []string{"pr", "title"} {
		if err := cmd.MarkFlagRequired(f); err != nil {
			panic(err) // the flag is defined above; this cannot fail
		}
	}
	return cmd
}

func parseNameStatus(out string) []changedFile {
	var files []changedFile
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		if status, path, ok := strings.Cut(line, "\t"); ok {
			files = append(files, changedFile{status: status, path: path})
		}
	}
	return files
}

type checkResult struct {
	pass     string
	warnings []string
}

// checkPR applies the rules in TDD Appendix I. paths in files are relative to the repo root.
func checkPR(cmd *cobra.Command, files []changedFile, pr int, title string, gh *ghapi.Client) (checkResult, error) {
	var res checkResult
	root := globalCfg.RootDir
	rel := func(p string) string {
		r, err := filepath.Rel(root, p)
		if err != nil {
			return p
		}
		return filepath.ToSlash(r)
	}

	// A hand edit of CHANGELOG.md fails even with a skip title. Only the label lifts it.
	var errs error
	for _, f := range files {
		for _, cl := range globalCfg.ChangeLogs {
			if f.path == rel(cl) {
				errs = errors.Join(errs, fmt.Errorf("%s is written by the release PR. Edit an entry in %s/ instead", f.path, rel(globalCfg.EntriesDir)))
			}
		}
	}

	var touched []string
	for _, f := range files {
		p := filepath.Join(root, filepath.FromSlash(f.path))
		if filepath.Dir(p) == globalCfg.EntriesDir && p != globalCfg.TemplateYAML && p != globalCfg.ConfigYAML &&
			(f.status == "A" || f.status == "M") && (strings.HasSuffix(p, ".yaml") || strings.HasSuffix(p, ".yml")) {
			touched = append(touched, p)
		}
	}

	// A config change, such as a removed component, can break any pending entry, so it checks them all.
	configChanged := slices.ContainsFunc(files, func(f changedFile) bool { return f.path == rel(globalCfg.ConfigYAML) })
	if configChanged {
		all, err := chlog.ReadEntries(globalCfg)
		if err == nil {
			err = chlog.ValidateAll(globalCfg, all)
		}
		errs = errors.Join(errs, err)
	}

	skipTitle := !breakingTitle.MatchString(title) && regexp.MustCompile(globalCfg.SkipTitlePattern).MatchString(title) &&
		(globalCfg.NoSkipTitlePattern == "" || !regexp.MustCompile(globalCfg.NoSkipTitlePattern).MatchString(title))
	if len(touched) == 0 {
		if errs != nil {
			return res, errs
		}
		if skipTitle {
			res.pass = "the title needs no entry"
			return res, nil
		}
		hint := "Add one with: changelog new -f <name>. If users see no change, a maintainer adds the " + skipLabel + " label"
		if breakingTitle.MatchString(title) {
			hint = "A '!' title is a breaking change and needs an entry with breaking_steps"
		}
		return res, fmt.Errorf("this PR adds no changelog entry. %s", hint)
	}

	// Read only this PR's entries, so a bad pending entry on main does not fail unrelated PRs.
	entriesByChangelog, err := chlog.ReadEntryFiles(globalCfg, touched)
	if err != nil {
		return res, err
	}
	if !configChanged {
		errs = errors.Join(errs, chlog.ValidateAll(globalCfg, entriesByChangelog))
	}

	own := fmt.Sprintf("#%d", pr)
	plainTitle := normalize(titlePrefix.ReplaceAllString(title, ""))
	for _, e := range chlog.Unique(entriesByChangelog) {
		if !slices.Contains(touched, e.File) {
			continue
		}
		name := filepath.Base(e.File)
		if plainTitle != "" && normalize(e.Note) == plainTitle {
			errs = errors.Join(errs, fmt.Errorf("%s: 'note' copies the PR title. Say what users see and what changed", name))
		}
		for _, ref := range e.Refs() {
			repo, n, _ := strings.Cut(ref, "#")
			if repo == "" {
				repo = globalCfg.Repo
			}
			if repo == "" || ref == own {
				continue
			}
			switch err := gh.PRExists(cmd.Context(), repo, n); {
			case err == nil:
			case errors.Is(err, ghapi.ErrNotFound):
				errs = errors.Join(errs, fmt.Errorf("%s: %s is not a pull request in %s", name, ref, repo))
			default:
				res.warnings = append(res.warnings, fmt.Sprintf("%s: could not check %s, GitHub did not answer (%v)", name, ref, err))
			}
		}
	}
	if errs != nil {
		return res, errs
	}
	res.pass = fmt.Sprintf("%d entry file(s) checked", len(touched))
	return res, nil
}

// normalize lowercases s and drops spaces and punctuation at the ends.
func normalize(s string) string {
	return strings.ToLower(strings.Trim(strings.TrimSpace(s), " .!"))
}
