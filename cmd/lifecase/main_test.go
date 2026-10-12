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

func TestRejectsAliasedParentsBeforeLaunchingFixture(t *testing.T) {
	dir := t.TempDir()
	real, alias := filepath.Join(dir, "real"), filepath.Join(dir, "alias")
	if err := os.Mkdir(real, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(real, alias); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	// The nested parents do not exist yet, so resolving only existing full
	// parent paths would miss this alias too.
	first, second := filepath.Join(real, "new", "report"), filepath.Join(alias, "new", "report")
	var out, stderr bytes.Buffer
	code := run([]string{"run", "--fixture", "not-an-executable", "--json", first, "--junit", second}, &out, &stderr)
	if code != 2 || !strings.Contains(stderr.String(), "different output paths") {
		t.Fatalf("exit %d: %s", code, stderr.String())
	}
	if _, err := os.Lstat(first); !os.IsNotExist(err) {
		t.Fatalf("partial report remains: %v", err)
	}
}

func TestCaseAliasesUseDestinationFilesystemRules(t *testing.T) {
	dir := t.TempDir()
	probe := filepath.Join(dir, "Probe")
	if err := os.WriteFile(probe, nil, 0600); err != nil {
		t.Fatal(err)
	}
	_, caseErr := os.Stat(filepath.Join(dir, "probe"))
	first, second := filepath.Join(dir, "Report"), filepath.Join(dir, "report")
	outputs, err := reserveOutputs(first, second)
	if caseErr == nil {
		if err == nil || !strings.Contains(err.Error(), "different output paths") {
			t.Fatalf("case alias accepted: %v", err)
		}
	} else {
		if err != nil {
			t.Fatalf("distinct case-sensitive paths refused: %v", err)
		}
		cleanupOutputs(outputs)
	}
	if _, err := os.Lstat(first); !os.IsNotExist(err) {
		t.Fatalf("reservation remains: %v", err)
	}
	if _, err := os.Lstat(second); !os.IsNotExist(err) {
		t.Fatalf("reservation remains: %v", err)
	}
}

func TestFailedReservationRemovesOnlyOwnedEmptyFiles(t *testing.T) {
	dir := t.TempDir()
	first := filepath.Join(dir, "report")
	blocker := filepath.Join(dir, "not-a-directory")
	if err := os.WriteFile(blocker, []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := reserveOutputs(first, filepath.Join(blocker, "report")); err == nil {
		t.Fatal("invalid output accepted")
	}
	if _, err := os.Lstat(first); !os.IsNotExist(err) {
		t.Fatalf("partial reservation remains: %v", err)
	}
	actual, err := os.ReadFile(blocker)
	if err != nil || string(actual) != "keep" {
		t.Fatalf("existing file changed: %q %v", actual, err)
	}
}

func TestReservationCleanupPreservesWrittenOutput(t *testing.T) {
	path := filepath.Join(t.TempDir(), "report")
	outputs, err := reserveOutputs(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := outputs[0].write([]byte("evidence")); err != nil {
		t.Fatal(err)
	}
	cleanupOutputs(outputs)
	actual, err := os.ReadFile(path)
	if err != nil || string(actual) != "evidence" {
		t.Fatalf("report not retained: %q %v", actual, err)
	}
}
