package exclude_test

import (
	"testing"

	"github.com/cerclbackup/cerclbackup/internal/exclude"
)

func TestBasenameMatch(t *testing.T) {
	f, _ := exclude.New([]string{"*.tmp", ".git"})
	cases := []struct {
		path string
		want bool
	}{
		{"/home/user/project/file.tmp", true},
		{"/home/user/.git/config", true},
		{"/home/user/.git", true},
		{"/home/user/project/main.go", false},
		{"/home/user/project/notes.txt", false},
	}
	for _, c := range cases {
		if got := f.Match(c.path); got != c.want {
			t.Errorf("Match(%q) = %v, want %v", c.path, got, c.want)
		}
	}
}

func TestDeepDirectoryExclusion(t *testing.T) {
	f, _ := exclude.New([]string{"node_modules"})
	cases := []struct {
		path string
		want bool
	}{
		{"/proj/node_modules/lodash/index.js", true},
		{"/proj/src/node_modules/pkg/file.js", true},
		{"/proj/src/main.js", false},
	}
	for _, c := range cases {
		if got := f.Match(c.path); got != c.want {
			t.Errorf("Match(%q) = %v, want %v", c.path, got, c.want)
		}
	}
}

func TestParse(t *testing.T) {
	f, err := exclude.Parse("*.log , .git, *.tmp")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if !f.Match("/var/log/app.log") {
		t.Error("expected *.log to match app.log")
	}
	if !f.Match("/project/.git/HEAD") {
		t.Error("expected .git to match .git/HEAD")
	}
	if f.Match("/project/main.go") {
		t.Error("main.go should not be excluded")
	}
}

func TestEmptyFilter(t *testing.T) {
	f, _ := exclude.New(nil)
	if f.Match("/any/path.txt") {
		t.Error("empty filter should not exclude anything")
	}
	if !f.Empty() {
		t.Error("Empty() should return true")
	}
}

func TestInvalidPattern(t *testing.T) {
	_, err := exclude.New([]string{"[invalid"})
	if err == nil {
		t.Error("expected error for invalid glob pattern")
	}
}

func TestParseEmptyString(t *testing.T) {
	f, err := exclude.Parse("")
	if err != nil {
		t.Fatalf("Parse empty: %v", err)
	}
	if !f.Empty() {
		t.Error("should be empty")
	}
}
