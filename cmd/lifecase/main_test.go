package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRejectsReportCollisionBeforeLaunchingFixture(t *testing.T) {
	path := filepath.Join(t.TempDir(), "report.json")
	var out, stderr bytes.Buffer
	code := run([]string{"run", "--fixture", "not-an-executable", "--json", path,
		"--junit", filepath.Join(filepath.Dir(path), ".", "report.json")}, &out, &stderr)
	if code != 2 || !strings.Contains(stderr.String(), "different output paths") {
		t.Fatalf("exit %d: %s", code, stderr.String())
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("preflight created report: %v", err)
	}
}

func TestExistingOutputsAndFixtureArePreserved(t *testing.T) {
	for _, flag := range []string{"--json", "--junit"} {
		t.Run(flag, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "fixture")
			original := []byte("an executable or valuable existing report")
			if err := os.WriteFile(path, original, 0755); err != nil {
				t.Fatal(err)
			}
			var out, stderr bytes.Buffer
			if code := run([]string{"run", "--fixture", path, flag, path}, &out, &stderr); code != 2 {
				t.Fatalf("exit %d: %s", code, stderr.String())
			}
			if !strings.Contains(stderr.String(), "already exists") {
				t.Fatal(stderr.String())
			}
			if err := write(path, []byte("replacement")); !os.IsExist(err) {
				t.Fatalf("write should refuse an existing file: %v", err)
			}
			actual, err := os.ReadFile(path)
			if err != nil || !bytes.Equal(actual, original) {
				t.Fatalf("original changed: %q, %v", actual, err)
			}
		})
	}
}

func TestOutputCreatedAfterPreflightIsPreserved(t *testing.T) {
	path := filepath.Join(t.TempDir(), "reports", "run.json")
	if err := validateOutputs(path, ""); err != nil {
		t.Fatal(err)
	}
	if err := write(path, []byte("first")); err != nil {
		t.Fatal(err)
	}
	if err := write(path, []byte("second")); !os.IsExist(err) {
		t.Fatalf("expected exclusive creation: %v", err)
	}
}

func TestReportSymlinkDoesNotChangeTarget(t *testing.T) {
	dir := t.TempDir()
	target, link := filepath.Join(dir, "target"), filepath.Join(dir, "report")
	if err := os.WriteFile(target, []byte("original"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if err := validateOutputs(link); err == nil {
		t.Fatal("symlink passed preflight")
	}
	if err := write(link, []byte("replacement")); !os.IsExist(err) {
		t.Fatalf("symlink output not refused: %v", err)
	}
	actual, err := os.ReadFile(target)
	if err != nil || string(actual) != "original" {
		t.Fatalf("target changed: %q, %v", actual, err)
	}
}
