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

// Package chlog reads, validates and deletes changelog entries.
package chlog

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/SigNoz/changelog/internal/config"
)

// Change types, in the order their headings render.
const (
	Breaking    = "breaking"
	Deprecation = "deprecation"
	Feature     = "feature"
	Improvement = "improvement"
	Fix         = "fix"
)

var changeTypes = []string{Breaking, Deprecation, Feature, Improvement, Fix}

// docsPrefix is the only accepted start of a 'docs' link.
const docsPrefix = "https://signoz.io/docs/"

// prRef matches "#123" (this repo) or "SigNoz/<repo>#123".
var prRef = regexp.MustCompile(`^(SigNoz/[A-Za-z0-9._-]+)?#[0-9]+$`)

// Refs returns the entry's PR and related references.
func (e Entry) Refs() []string { return slices.Concat(e.PRs, e.Related) }

// Entry represents a changelog entry.
type Entry struct {
	ChangeLogs    []string `yaml:"change_logs"`
	ChangeType    string   `yaml:"change_type"`
	Component     string   `yaml:"component"`
	Note          string   `yaml:"note"`
	SubText       string   `yaml:"subtext"`
	PRs           []string `yaml:"prs"`
	Related       []string `yaml:"related"`
	Deployment    string   `yaml:"deployment"`
	Docs          string   `yaml:"docs"`
	BreakingSteps string   `yaml:"breaking_steps"`
	Security      bool     `yaml:"security"`
	UpgradeStop   bool     `yaml:"upgrade_stop"`

	// File is the entry's path. update deletes only the files it rendered.
	File string `yaml:"-"`
}

// Validate validates the changelog entry against the repo configuration.
func (e Entry) Validate(cfg *config.Config) error {
	var errs error
	add := func(format string, a ...any) { errs = errors.Join(errs, fmt.Errorf(format, a...)) }

	validChangeLogs := make([]string, 0, len(cfg.ChangeLogs))
	for key := range cfg.ChangeLogs {
		validChangeLogs = append(validChangeLogs, key)
	}
	if len(cfg.DefaultChangeLogs) == 0 && len(e.ChangeLogs) == 0 {
		add("specify one or more 'change_logs'")
	}
	for _, cl := range e.ChangeLogs {
		if !slices.Contains(validChangeLogs, cl) {
			add("'%s' is not a valid value in 'change_logs'. Specify one of %v", cl, validChangeLogs)
		}
	}

	if !slices.Contains(changeTypes, e.ChangeType) {
		add("'%s' is not a valid 'change_type'. Specify one of %v", e.ChangeType, changeTypes)
	}
	if strings.TrimSpace(e.Component) == "" {
		add("specify a 'component'")
	} else if len(cfg.Components) > 0 && !slices.Contains(cfg.Components, e.Component) {
		add("%s is not a valid 'component'. It must be one of %v", e.Component, cfg.Components)
	}
	if strings.TrimSpace(e.Note) == "" {
		add("specify a 'note'")
	}

	if len(e.PRs) == 0 {
		add("specify one or more 'prs', such as \"#123\"")
	}
	for _, ref := range slices.Concat(e.PRs, e.Related) {
		if !prRef.MatchString(ref) {
			add("'%s' is not a PR reference. Use \"#123\" or \"SigNoz/<repo>#123\"", ref)
			continue
		}
		if repo, _, _ := strings.Cut(ref, "#"); repo != "" && len(cfg.AllowedRepos) > 0 && !slices.Contains(cfg.AllowedRepos, repo) {
			add("'%s' names a repo outside 'allowed_repos' %v", ref, cfg.AllowedRepos)
		}
	}

	switch {
	case len(cfg.Deployments) == 0 && e.Deployment != "":
		add("this repo does not use 'deployment'. Remove it")
	case len(cfg.Deployments) > 0 && !slices.Contains(cfg.Deployments, e.Deployment):
		add("'deployment' must be one of %v", cfg.Deployments)
	}

	if e.ChangeType == Breaking && strings.TrimSpace(e.BreakingSteps) == "" {
		add("a breaking change needs 'breaking_steps'")
	}
	if e.ChangeType != Breaking && e.BreakingSteps != "" {
		add("'breaking_steps' is only for breaking changes")
	}
	if e.ChangeType == Deprecation && strings.TrimSpace(e.SubText) == "" {
		add("a deprecation needs 'subtext' that names the replacement")
	}
	if e.Docs != "" && !strings.HasPrefix(e.Docs, docsPrefix) {
		add("'docs' must start with %s", docsPrefix)
	}
	if e.UpgradeStop && e.Docs == "" {
		add("an upgrade stop needs a 'docs' link to the upgrade guide")
	}

	if errs != nil {
		return fmt.Errorf("%s: %w", filepath.Base(e.File), errs)
	}
	return nil
}

// ValidateAll validates every entry once, even when it belongs to several changelogs.
func ValidateAll(cfg *config.Config, entriesByChangelog map[string][]*Entry) error {
	var errs error
	for _, e := range Unique(entriesByChangelog) {
		errs = errors.Join(errs, e.Validate(cfg))
	}
	return errs
}

// Unique returns each entry once, in file order.
func Unique(entriesByChangelog map[string][]*Entry) []*Entry {
	seen := map[*Entry]bool{}
	var out []*Entry
	for _, entries := range entriesByChangelog {
		for _, e := range entries {
			if !seen[e] {
				seen[e] = true
				out = append(out, e)
			}
		}
	}
	slices.SortFunc(out, func(a, b *Entry) int { return strings.Compare(a.File, b.File) })
	return out
}

// ReadEntries reads changelog entries from YAML files based on the provided configuration.
// Unknown keys are an error, so a typo such as 'breaking_step' cannot be dropped silently.
func ReadEntries(cfg *config.Config) (map[string][]*Entry, error) {
	yamlFiles, err := findYamlFiles(cfg.EntriesDir)
	if err != nil {
		return nil, err
	}

	entries := make(map[string][]*Entry)
	for key := range cfg.ChangeLogs {
		entries[key] = make([]*Entry, 0)
	}

	for _, file := range yamlFiles {
		if file == cfg.TemplateYAML || file == cfg.ConfigYAML {
			continue
		}

		fileBytes, err := os.ReadFile(filepath.Clean(file))
		if err != nil {
			return nil, err
		}

		entry := &Entry{File: file}
		dec := yaml.NewDecoder(bytes.NewReader(fileBytes))
		dec.KnownFields(true)
		if err = dec.Decode(entry); err != nil {
			return nil, fmt.Errorf("%s: %w", filepath.Base(file), err)
		}
		entry.SubText = strings.ReplaceAll(entry.SubText, "\r\n", "\n")
		entry.BreakingSteps = strings.ReplaceAll(entry.BreakingSteps, "\r\n", "\n")

		if len(entry.ChangeLogs) == 0 {
			for _, cl := range cfg.DefaultChangeLogs {
				entries[cl] = append(entries[cl], entry)
			}
		} else {
			for _, cl := range entry.ChangeLogs {
				entries[cl] = append(entries[cl], entry)
			}
		}
	}
	return entries, nil
}

// DeleteEntries deletes the files of the given entries and returns any failure.
func DeleteEntries(entries []*Entry) error {
	var errs error
	for _, e := range entries {
		if err := os.Remove(e.File); err != nil && !errors.Is(err, os.ErrNotExist) {
			errs = errors.Join(errs, fmt.Errorf("delete %s: %w", e.File, err))
		}
	}
	return errs
}

// findYamlFiles finds all YAML files in the specified directory.
// It includes files with both .yaml and .yml extensions.
func findYamlFiles(dir string) ([]string, error) {
	yamlFiles, err := filepath.Glob(filepath.Join(dir, "*.yaml"))
	if err != nil {
		return nil, fmt.Errorf("failed to find YAML files in %s: %w", dir, err)
	}

	ymlFiles, err := filepath.Glob(filepath.Join(dir, "*.yml"))
	if err != nil {
		return nil, fmt.Errorf("failed to find YML files in %s: %w", dir, err)
	}

	return slices.Concat(yamlFiles, ymlFiles), nil
}
