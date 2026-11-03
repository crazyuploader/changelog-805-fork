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

// Package config provides changelog configuration.
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"

	"gopkg.in/yaml.v3"
)

const (
	// DefaultEntriesDir is the default directory for changelog entries.
	DefaultEntriesDir = ".chloggen"
	// DefaultTemplateYAML is the default template file for changelog entries.
	DefaultTemplateYAML = "TEMPLATE.yaml"
	// DefaultChangeLogKey is the default key for the changelog.
	DefaultChangeLogKey = "default"
	// DefaultChangeLogFilename is the default filename for the changelog.
	DefaultChangeLogFilename = "CHANGELOG.md"
	// DefaultSkipTitlePattern matches PR titles that need no entry. A '!' title never matches.
	DefaultSkipTitlePattern = `^(chore|ci|test|docs)(\([^)]*\))?: `
)

// Config represents the configuration for changelogs.
type Config struct {
	ChangeLogs        map[string]string `yaml:"change_logs"`
	DefaultChangeLogs []string          `yaml:"default_change_logs"`
	EntriesDir        string            `yaml:"entries_dir"`
	TemplateYAML      string            `yaml:"template_yaml"`
	SummaryTemplate   string            `yaml:"summary_template"`
	Components        []string          `yaml:"components"`
	// Repo is the GitHub repo, such as SigNoz/signoz. It turns PR refs into links.
	Repo string `yaml:"repo"`
	// Deployments lists the allowed 'deployment' values. Non-empty makes the field required.
	Deployments []string `yaml:"deployments"`
	// TagPrefix selects the release tags that 'next' reads. Default: v.
	TagPrefix string `yaml:"tag_prefix"`
	// PreOneFeatureBump is the bump for a feature before 1.0: minor (default) or patch.
	PreOneFeatureBump string `yaml:"pre_1_feature_bump"`
	// AllowedRepos limits the repos that 'prs' and 'related' may name. Empty allows any SigNoz repo.
	AllowedRepos []string `yaml:"allowed_repos"`
	// SkipTitlePattern is the PR title regex that needs no entry. Default: DefaultSkipTitlePattern.
	SkipTitlePattern string `yaml:"skip_title_pattern"`
	// NoSkipTitlePattern wins over SkipTitlePattern, such as ^chore\(codegen\) in terraform.
	NoSkipTitlePattern string `yaml:"no_skip_title_pattern"`
	ConfigYAML         string
	// RootDir is the repo root the paths resolve against.
	RootDir string
}

// New returns a new Config with default values.
func New(rootDir string) *Config {
	return &Config{
		RootDir:           rootDir,
		ChangeLogs:        map[string]string{DefaultChangeLogKey: filepath.Join(rootDir, DefaultChangeLogFilename)},
		DefaultChangeLogs: []string{DefaultChangeLogKey},
		EntriesDir:        filepath.Join(rootDir, DefaultEntriesDir),
		TemplateYAML:      filepath.Join(rootDir, DefaultEntriesDir, DefaultTemplateYAML),
		TagPrefix:         "v",
		PreOneFeatureBump: "minor",
		SkipTitlePattern:  DefaultSkipTitlePattern,
	}
}

// NewFromFile returns a new Config from the specified YAML file.
func NewFromFile(rootDir string, cfgFilename string) (*Config, error) {
	if !filepath.IsAbs(cfgFilename) {
		cfgFilename = filepath.Join(rootDir, cfgFilename)
	}
	cfgYAML := filepath.Clean(cfgFilename)
	cfgBytes, err := os.ReadFile(cfgYAML) // nolint:gosec // false positive
	if err != nil {
		return nil, err
	}
	cfg := &Config{}
	if err = yaml.Unmarshal(cfgBytes, &cfg); err != nil {
		return nil, err
	}

	cfg.ConfigYAML = cfgYAML
	cfg.RootDir = rootDir
	cfg.EntriesDir = makeAbs(rootDir, cfg.EntriesDir, DefaultEntriesDir)
	cfg.TemplateYAML = makeAbs(rootDir, cfg.TemplateYAML, filepath.Join(DefaultEntriesDir, DefaultTemplateYAML))

	if cfg.TagPrefix == "" {
		cfg.TagPrefix = "v"
	}
	if cfg.SkipTitlePattern == "" {
		cfg.SkipTitlePattern = DefaultSkipTitlePattern
	}
	for key, pattern := range map[string]string{"skip_title_pattern": cfg.SkipTitlePattern, "no_skip_title_pattern": cfg.NoSkipTitlePattern} {
		if _, err := regexp.Compile(pattern); err != nil {
			return nil, fmt.Errorf("'%s': %w", key, err)
		}
	}
	switch cfg.PreOneFeatureBump {
	case "":
		cfg.PreOneFeatureBump = "minor"
	case "minor", "patch":
	default:
		return nil, fmt.Errorf("'pre_1_feature_bump' must be minor or patch, got %q", cfg.PreOneFeatureBump)
	}

	if len(cfg.ChangeLogs) == 0 && len(cfg.DefaultChangeLogs) > 0 {
		return nil, errors.New("cannot specify 'default_changelogs' without 'changelogs'")
	}

	if len(cfg.ChangeLogs) == 0 {
		cfg.ChangeLogs = map[string]string{DefaultChangeLogKey: filepath.Join(rootDir, DefaultChangeLogFilename)}
		cfg.DefaultChangeLogs = []string{DefaultChangeLogKey}
		return cfg, nil
	}

	// The user specified at least one changelog. Interpret filename as a relative path from rootDir
	// (unless they specified an absolute path including rootDir)
	for key, filename := range cfg.ChangeLogs {
		if !filepath.IsAbs(filename) {
			cfg.ChangeLogs[key] = filepath.Join(rootDir, filename)
		}
		cfg.ChangeLogs[key] = filepath.Clean(cfg.ChangeLogs[key])
	}

	for _, key := range cfg.DefaultChangeLogs {
		if _, ok := cfg.ChangeLogs[key]; !ok {
			return nil, fmt.Errorf("'default_changelogs' contains key %q which is not defined in 'changelogs'", key)
		}
	}

	return cfg, nil
}

func makeAbs(rootDir, path, defaultPath string) string {
	if path == "" {
		return filepath.Clean(filepath.Join(rootDir, defaultPath))
	}
	if filepath.IsAbs(path) {
		return filepath.Clean(path)
	}
	return filepath.Clean(filepath.Join(rootDir, path))
}
