package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestRunVersion(t *testing.T) {
	var stdout, stderr bytes.Buffer

	if code := run([]string{"version"}, &stdout, &stderr); code != exitOK {
		t.Fatalf("exit code = %d, want %d", code, exitOK)
	}
	if got := stdout.String(); !strings.HasPrefix(got, "reflow ") {
		t.Errorf("stdout = %q, want it to start with %q", got, "reflow ")
	}
	if stderr.Len() != 0 {
		t.Errorf("stderr = %q, want empty", stderr.String())
	}
}

func TestRunVersionAliases(t *testing.T) {
	for _, arg := range []string{"-v", "--version"} {
		t.Run(arg, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			if code := run([]string{arg}, &stdout, &stderr); code != exitOK {
				t.Fatalf("exit code = %d, want %d", code, exitOK)
			}
			if stdout.Len() == 0 {
				t.Error("stdout is empty, want version line")
			}
		})
	}
}

func TestRunHelp(t *testing.T) {
	for _, arg := range []string{"help", "-h", "--help"} {
		t.Run(arg, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			if code := run([]string{arg}, &stdout, &stderr); code != exitOK {
				t.Fatalf("exit code = %d, want %d", code, exitOK)
			}
			if !strings.Contains(stdout.String(), "usage:") {
				t.Errorf("stdout = %q, want usage text", stdout.String())
			}
		})
	}
}

func TestRunNoArgsPrintsUsageToStderr(t *testing.T) {
	var stdout, stderr bytes.Buffer

	if code := run(nil, &stdout, &stderr); code != exitUsage {
		t.Fatalf("exit code = %d, want %d", code, exitUsage)
	}
	if !strings.Contains(stderr.String(), "usage:") {
		t.Errorf("stderr = %q, want usage text", stderr.String())
	}
	if stdout.Len() != 0 {
		t.Errorf("stdout = %q, want empty", stdout.String())
	}
}

func TestRunUnknownCommand(t *testing.T) {
	var stdout, stderr bytes.Buffer

	if code := run([]string{"frobnicate"}, &stdout, &stderr); code != exitUsage {
		t.Fatalf("exit code = %d, want %d", code, exitUsage)
	}
	if !strings.Contains(stderr.String(), `"frobnicate"`) {
		t.Errorf("stderr = %q, want it to name the unknown command", stderr.String())
	}
}
