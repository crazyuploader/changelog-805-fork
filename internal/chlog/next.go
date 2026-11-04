// Copyright The SigNoz Authors
// SPDX-License-Identifier: Apache-2.0

package chlog

import (
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

var stableVersion = regexp.MustCompile(`^([0-9]+)\.([0-9]+)\.([0-9]+)$`)

// ErrNothingToRelease means no entries are pending.
var ErrNothingToRelease = errors.New("nothing to release: no pending entries. Pass --version to release anyway")

// NextVersion returns the version after the latest stable tag that starts with prefix.
// Tags with a suffix, such as v0.150.0-rc.1, are skipped.
// Bump: breaking gives major (minor before 1.0), feature gives minor
// (preOneFeature before 1.0), anything else gives patch.
func NextVersion(tags []string, prefix, preOneFeature string, entries []*Entry) (string, error) {
	if len(entries) == 0 {
		return "", ErrNothingToRelease
	}

	latest, err := LatestStable(tags, prefix)
	if err != nil {
		return "", err
	}
	breaking := slices.ContainsFunc(entries, func(e *Entry) bool { return e.ChangeType == Breaking })
	feature := slices.ContainsFunc(entries, func(e *Entry) bool { return e.ChangeType == Feature })
	var major, minor, patch int
	if _, err := fmt.Sscanf(strings.TrimPrefix(latest, prefix), "%d.%d.%d", &major, &minor, &patch); err != nil {
		return "", fmt.Errorf("parse %s: %w", latest, err)
	}
	switch {
	case breaking && major > 0:
		major, minor, patch = major+1, 0, 0
	case breaking, feature && (major > 0 || preOneFeature == "minor"):
		minor, patch = minor+1, 0
	default:
		patch++
	}
	return fmt.Sprintf("%s%d.%d.%d", prefix, major, minor, patch), nil
}

// LatestStable returns the highest tag that is prefix plus X.Y.Z, skipping
// prerelease tags and other release streams.
// IsStable reports whether v is the prefix plus a stable version, such as v0.149.0.
func IsStable(v, prefix string) bool {
	rest, ok := strings.CutPrefix(v, prefix)
	return ok && stableVersion.MatchString(rest)
}

func LatestStable(tags []string, prefix string) (string, error) {
	var latest []int
	var latestTag string
	for _, tag := range tags {
		rest, ok := strings.CutPrefix(tag, prefix)
		m := stableVersion.FindStringSubmatch(rest)
		if !ok || m == nil {
			continue
		}
		v := make([]int, 3)
		for i := range v {
			v[i], _ = strconv.Atoi(m[i+1]) // the regex guarantees digits
		}
		if latest == nil || slices.Compare(v, latest) > 0 {
			latest, latestTag = v, tag
		}
	}
	if latest == nil {
		return "", fmt.Errorf("no stable tag starts with %q. Pass --version for the first release", prefix)
	}
	return latestTag, nil
}
