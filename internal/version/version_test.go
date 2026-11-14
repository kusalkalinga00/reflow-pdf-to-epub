package version

import (
	"runtime"
	"strings"
	"testing"
)

func TestStringContainsVersionAndPlatform(t *testing.T) {
	got := String()

	if !strings.HasPrefix(got, "reflow dev (commit ") {
		t.Errorf("String() = %q, want it to start with the default version and commit", got)
	}
	if !strings.Contains(got, runtime.GOOS+"/"+runtime.GOARCH) {
		t.Errorf("String() = %q, want it to contain the platform %q", got, runtime.GOOS+"/"+runtime.GOARCH)
	}
	if strings.Contains(got, "\n") {
		t.Errorf("String() = %q, want a single line", got)
	}
}

func TestStringHonoursLinkTimeValues(t *testing.T) {
	oldVersion, oldCommit, oldDate := Version, Commit, BuildDate
	t.Cleanup(func() { Version, Commit, BuildDate = oldVersion, oldCommit, oldDate })

	Version, Commit, BuildDate = "v1.2.3", "abc1234", "2026-11-14"

	got := String()
	want := "reflow v1.2.3 (commit abc1234, built 2026-11-14)"
	if !strings.HasPrefix(got, want) {
		t.Errorf("String() = %q, want it to start with %q", got, want)
	}
}
