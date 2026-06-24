// Package exclude provides glob-based file exclusion filtering for backup
// and watch operations.  Patterns are matched against both the full path
// and the base name so that common shorthands like "*.tmp" or ".git" work
// without the user having to prefix "**/" everywhere.
package exclude

import (
	"path/filepath"
	"strings"
)

// Filter holds a compiled set of exclusion patterns.
type Filter struct {
	patterns []string
}

// New creates a Filter from a slice of glob patterns.
// Each pattern is validated with filepath.Match; an invalid pattern returns
// an error.
func New(patterns []string) (*Filter, error) {
	clean := make([]string, 0, len(patterns))
	for _, p := range patterns {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		// Validate the pattern.
		if _, err := filepath.Match(p, "probe"); err != nil {
			return nil, err
		}
		clean = append(clean, p)
	}
	return &Filter{patterns: clean}, nil
}

// Parse splits a comma-separated pattern string and calls New.
// Whitespace around commas is trimmed.
func Parse(csv string) (*Filter, error) {
	if csv == "" {
		return &Filter{}, nil
	}
	parts := strings.Split(csv, ",")
	return New(parts)
}

// Match reports whether path should be excluded.
// It checks:
//  1. The full path against each pattern.
//  2. Each path component (directory names and base name) against each pattern.
//
// This lets patterns like ".git" exclude the directory at any depth, and
// "*.log" exclude any .log file regardless of its containing directory.
func (f *Filter) Match(path string) bool {
	if len(f.patterns) == 0 {
		return false
	}
	clean := filepath.ToSlash(filepath.Clean(path))
	base := filepath.Base(clean)

	for _, pat := range f.patterns {
		// Match base name.
		if ok, _ := filepath.Match(pat, base); ok {
			return true
		}
		// Match full slash-normalised path.
		if ok, _ := filepath.Match(pat, clean); ok {
			return true
		}
		// Match any component — allows "node_modules" to exclude at any depth.
		for _, component := range strings.Split(clean, "/") {
			if ok, _ := filepath.Match(pat, component); ok {
				return true
			}
		}
	}
	return false
}

// Empty reports whether the filter has no patterns (always passes everything).
func (f *Filter) Empty() bool {
	return len(f.patterns) == 0
}
