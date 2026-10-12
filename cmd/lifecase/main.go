// Lifecase is a developer verifier for the native lifecycle fixtures.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"github.com/rad1092/lifecase/kit"
	"github.com/rad1092/lifecase/runner"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
)

type args []string

func (a *args) String() string     { return strings.Join(*a, " ") }
func (a *args) Set(v string) error { *a = append(*a, v); return nil }
func main()                        { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr)) }
func run(argv []string, out, errOut io.Writer) int {
	if len(argv) == 1 && argv[0] == "version" {
		fmt.Fprintln(out, kit.Version)
		return 0
	}
	if len(argv) == 0 || argv[0] != "run" {
		fmt.Fprintln(errOut, "usage: lifecase run --fixture PATH [--adapter EXE --adapter-arg ARG] [--scenario NAME] [--json PATH] [--junit PATH]")
		return 2
	}
	f := flag.NewFlagSet("run", flag.ContinueOnError)
	f.SetOutput(errOut)
	fixture := f.String("fixture", "", "native fixture executable")
	adapter := f.String("adapter", "", "external JSONL adapter executable")
	jsonPath := f.String("json", "", "JSON output path (default stdout)")
	junit := f.String("junit", "", "JUnit output path")
	var extra, names args
	f.Var(&extra, "adapter-arg", "adapter argument (repeatable)")
	f.Var(&names, "scenario", "scenario to run (repeatable)")
	if e := f.Parse(argv[1:]); e != nil {
		return 2
	}
	if *fixture == "" || f.NArg() != 0 {
		fmt.Fprintln(errOut, "--fixture is required and positional arguments are not accepted")
		return 2
	}
	if e := validateOutputs(*jsonPath, *junit); e != nil {
		fmt.Fprintln(errOut, e)
		return 2
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	var subject runner.Runner = runner.Native{}
	if *adapter != "" {
		subject = runner.External{Executable: *adapter, Args: extra}
	}
	report, e := kit.Run(ctx, kit.Options{Fixture: *fixture, Runner: subject, Scenarios: names})
	if e != nil {
		fmt.Fprintln(errOut, e)
		return 2
	}
	b, e := json.MarshalIndent(report, "", "  ")
	if e != nil {
		fmt.Fprintln(errOut, e)
		return 2
	}
	b = append(b, '\n')
	if *jsonPath == "" {
		_, e = out.Write(b)
	} else {
		e = write(*jsonPath, b)
	}
	if e != nil {
		fmt.Fprintln(errOut, e)
		return 2
	}
	if *junit != "" {
		var sb strings.Builder
		if e = report.WriteJUnit(&sb); e == nil {
			e = write(*junit, []byte(sb.String()))
		}
		if e != nil {
			fmt.Fprintln(errOut, e)
			return 2
		}
	}
	if report.Failed() {
		return 1
	}
	return 0
}
func write(path string, b []byte) error {
	if e := os.MkdirAll(filepath.Dir(path), 0755); e != nil {
		return e
	}
	// Exclusive creation also protects against an existing executable, symlink,
	// or a destination created after preflight. Never truncate a user's file.
	f, e := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
	if e != nil {
		return e
	}
	_, e = f.Write(b)
	closeErr := f.Close()
	if e != nil {
		return e
	}
	return closeErr
}

func validateOutputs(paths ...string) error {
	seen := make(map[string]bool)
	for _, path := range paths {
		if path == "" {
			continue
		}
		if _, e := os.Lstat(path); e == nil {
			return fmt.Errorf("report path already exists; choose a new filename: %s", path)
		} else if !os.IsNotExist(e) {
			return e
		}
		absolute, e := filepath.Abs(path)
		if e != nil {
			return e
		}
		if seen[absolute] {
			return fmt.Errorf("JSON and JUnit reports require different output paths: %s", path)
		}
		seen[absolute] = true
	}
	return nil
}
