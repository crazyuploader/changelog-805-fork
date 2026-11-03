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

package chlog

import (
	"bytes"
	_ "embed"
	"fmt"
	"path/filepath"
	"strings"
	"text/template"

	"github.com/SigNoz/changelog/internal/config"
)

//go:embed summary.tmpl
var defaultTmpl []byte

type summary struct {
	Version      string
	UpgradeGuide string
	Security     []*Entry
	Breaking     []*Entry
	Deprecated   []*Entry
	Added        []*Entry
	Changed      []*Entry
	Fixed        []*Entry
}

// GenerateSummary generates a changelog entry summary.
// An unknown change type is an error, so no entry can be dropped without a trace.
func GenerateSummary(version string, entries []*Entry, cfg *config.Config) (string, error) {
	s := summary{Version: version}

	for _, entry := range entries {
		if entry.Security {
			s.Security = append(s.Security, entry)
		}
		if entry.UpgradeStop && s.UpgradeGuide == "" {
			s.UpgradeGuide = entry.Docs
		}
		switch entry.ChangeType {
		case Breaking:
			s.Breaking = append(s.Breaking, entry)
		case Deprecation:
			s.Deprecated = append(s.Deprecated, entry)
		case Feature:
			s.Added = append(s.Added, entry)
		case Improvement:
			s.Changed = append(s.Changed, entry)
		case Fix:
			s.Fixed = append(s.Fixed, entry)
		default:
			return "", fmt.Errorf("%s: no heading for change_type %q", filepath.Base(entry.File), entry.ChangeType)
		}
	}

	return s.String(cfg.SummaryTemplate, cfg.Repo)
}

// TemplateFuncMap returns a map of functions to be used in the template.
// repo, such as SigNoz/signoz, turns "#123" into a link.
func TemplateFuncMap(repo string) template.FuncMap {
	return template.FuncMap{
		"indent": func(n int, s string) string {
			indent := strings.Repeat(" ", n)
			return indent + strings.ReplaceAll(s, "\n", "\n"+indent)
		},
		"trim": strings.TrimSpace,
		"refs": func(e *Entry) string {
			links := make([]string, 0, len(e.PRs)+len(e.Related))
			for _, ref := range e.PRs {
				links = append(links, RefLink(repo, ref))
			}
			for _, ref := range e.Related {
				links = append(links, "related "+RefLink(repo, ref))
			}
			return "(" + strings.Join(links, ", ") + ")"
		},
		"deploy": func(e *Entry) string {
			switch e.Deployment {
			case "cloud":
				return " [Cloud]"
			case "self-hosted":
				return " [Self-Hosted]"
			}
			return ""
		},
	}
}

// refLink renders "#123" or "SigNoz/<repo>#123" as a GitHub PR link.
func RefLink(repo, ref string) string {
	target, num, _ := strings.Cut(ref, "#")
	if target == "" {
		if repo == "" {
			return ref
		}
		target = repo
	}
	return fmt.Sprintf("[%s](https://github.com/%s/pull/%s)", ref, target, num)
}

// String renders the summary using the provided template.
func (s summary) String(summaryTemplate, repo string) (string, error) {
	var tmpl *template.Template
	var err error

	if summaryTemplate != "" {
		tmpl, err = template.
			New(filepath.Base(summaryTemplate)).
			Funcs(TemplateFuncMap(repo)).
			Option("missingkey=error").
			ParseFiles(summaryTemplate)
	} else {
		tmpl, err = template.
			New("summary.tmpl").
			Funcs(TemplateFuncMap(repo)).
			Option("missingkey=error").
			Parse(string(defaultTmpl))
	}
	if err != nil {
		return "", err
	}

	buf := bytes.Buffer{}
	if err := tmpl.Execute(&buf, s); err != nil {
		return "", fmt.Errorf("failed executing template: %w", err)
	}

	return buf.String(), nil
}
