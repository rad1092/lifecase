package runner

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func helperArgs(mode string, args ...string) []string {
	return append([]string{"-test.run=^TestRunnerHelperProcess$", "--", "--lifecase-helper", mode}, args...)
}

func helperRequest(t *testing.T, mode string, args ...string) Request {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	return Request{V: 1, Op: "start", Fixture: exe, Args: helperArgs(mode, args...), CaptureLimit: 97}
}

func collect(t *testing.T, s Session) []Event {
	t.Helper()
	var result []Event
	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()
	for {
		select {
		case e, ok := <-s.Events():
			if !ok {
				return result
			}
			result = append(result, e)
		case <-timer.C:
			t.Fatal("session event stream did not finish")
		}
	}
}

func TestNativeConcurrentDrainBoundsCapture(t *testing.T) {
	s, err := (Native{}).Start(context.Background(), helperRequest(t, "flood"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	events := collect(t, s)
	seen := map[string]Event{}
	for _, e := range events {
		seen[e.Kind] = e
	}
	if len(events) != 4 || seen["started"].PID <= 0 || seen["parent_exit"].ExitCode != 0 {
		t.Fatalf("unexpected lifecycle events: %+v", events)
	}
	for _, out := range []struct{ kind, char string }{{"stdout_eof", "O"}, {"stderr_eof", "E"}} {
		e := seen[out.kind]
		if e.Bytes != StdinWriteBytes || e.Prefix != strings.Repeat(out.char, 97) {
			t.Errorf("invalid %s count/capture: bytes=%d prefix=%q", out.kind, e.Bytes, e.Prefix)
		}
	}
}

func TestNativeParentExitPrecedesInheritedPipeEOF(t *testing.T) {
	dir := t.TempDir()
	stop := filepath.Join(dir, "stop")
	defer os.WriteFile(stop, nil, 0600)
	s, err := (Native{}).Start(context.Background(), helperRequest(t, "inherit", dir))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	for {
		select {
		case e := <-s.Events():
			if strings.HasSuffix(e.Kind, "_eof") || e.Kind == "error" {
				t.Fatalf("output ended before stop: %+v", e)
			}
			if e.Kind == "parent_exit" {
				goto exited
			}
		case <-deadline.C:
			t.Fatal("parent exit was blocked by inherited pipes")
		}
	}
exited:
	select {
	case e := <-s.Events():
		t.Fatalf("unexpected event before descendant release: %+v", e)
	case <-time.After(75 * time.Millisecond):
	}
	if err := os.WriteFile(stop, nil, 0600); err != nil {
		t.Fatal(err)
	}
	events := collect(t, s)
	if len(events) != 2 {
		t.Fatalf("expected two actual EOFs after stop: %+v", events)
	}
	if _, err := os.Stat(filepath.Join(dir, "child-exit")); err != nil {
		t.Fatalf("child did not acknowledge cleanup: %v", err)
	}
}

func TestNativeBlockedStdinCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s, err := (Native{}).Start(ctx, helperRequest(t, "blocked"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	start := time.Now()
	if err := s.Command("write_stdin"); err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(start); elapsed > 500*time.Millisecond {
		t.Fatalf("write_stdin blocked: %v", elapsed)
	}
	if err := s.Command("write_stdin"); err == nil {
		t.Fatal("duplicate write accepted")
	}
	cancel()
	collect(t, s)
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestPrecancelledContextCannotLaunch(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	marker := filepath.Join(t.TempDir(), "launched")
	req := helperRequest(t, "marker", marker)
	for _, r := range []Runner{Native{}, External{Executable: req.Fixture, Args: req.Args}} {
		s, err := r.Start(ctx, req)
		if !errors.Is(err, context.Canceled) || s != nil {
			t.Fatalf("pre-cancelled launch: session=%v error=%v", s, err)
		}
	}
	if _, err := os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("pre-cancelled request launched: %v", err)
	}
}

func TestExternalProtocolValidation(t *testing.T) {
	for _, mode := range []string{"valid", "duplicate", "unknown", "oversize", "prefix", "missing", "version", "event-limit", "extra-json", "missing-code", "missing-bytes", "null-code"} {
		t.Run(mode, func(t *testing.T) {
			req := helperRequest(t, "unused")
			r := External{Executable: req.Fixture, Args: helperArgs("adapter", mode)}
			s, err := r.Start(context.Background(), req)
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			events := collect(t, s)
			errors := 0
			for _, e := range events {
				if e.Kind == "error" {
					errors++
				}
			}
			if mode == "valid" {
				if errors != 0 || len(events) != 4 {
					t.Fatalf("valid adapter rejected: %+v", events)
				}
			} else if errors != 1 {
				t.Fatalf("invalid adapter needs one error: %+v", events)
			}
		})
	}
}

func TestExternalCommandAndBoundedClose(t *testing.T) {
	for _, mode := range []string{"command", "hang"} {
		t.Run(mode, func(t *testing.T) {
			req := helperRequest(t, "unused")
			s, err := (External{Executable: req.Fixture, Args: helperArgs("adapter", mode)}).Start(context.Background(), req)
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			if mode == "command" {
				if err := s.Command("write_stdin"); err != nil {
					t.Fatal(err)
				}
				for _, e := range collect(t, s) {
					if e.Kind == "error" {
						t.Fatal(e.Message)
					}
				}
			}
			start := time.Now()
			if err := s.Close(); err != nil {
				t.Fatal(err)
			}
			if time.Since(start) > 2*time.Second {
				t.Fatal("adapter close exceeded bound")
			}
		})
	}
}

func TestDiagnosticCaptureBound(t *testing.T) {
	c := &diagnosticCapture{}
	p := []byte(strings.Repeat("X", 32768))
	for range 20 {
		if n, err := c.Write(p); n != len(p) || err != nil {
			t.Fatalf("diagnostic write %d: %v", n, err)
		}
	}
	if len(c.prefix) != 4096 || c.bytes != 20*32768 {
		t.Fatalf("unbounded diagnostic capture: len=%d total=%d", len(c.prefix), c.bytes)
	}
}

func TestRunnerHelperProcess(t *testing.T) {
	var args []string
	for i, a := range os.Args {
		if a == "--lifecase-helper" {
			args = os.Args[i+1:]
			break
		}
	}
	if len(args) == 0 {
		return
	}
	switch args[0] {
	case "flood":
		var wg sync.WaitGroup
		wg.Add(2)
		go func() { defer wg.Done(); _, _ = io.WriteString(os.Stdout, strings.Repeat("O", StdinWriteBytes)) }()
		go func() { defer wg.Done(); _, _ = io.WriteString(os.Stderr, strings.Repeat("E", StdinWriteBytes)) }()
		wg.Wait()
	case "blocked":
		time.Sleep(5 * time.Second)
	case "marker":
		_ = os.WriteFile(args[1], nil, 0600)
	case "inherit":
		exe, _ := os.Executable()
		cmd := exec.Command(exe, helperArgs("child", args[1])...)
		cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
		if err := cmd.Start(); err != nil {
			os.Exit(90)
		}
		until := time.Now().Add(3 * time.Second)
		for time.Now().Before(until) {
			if _, err := os.Stat(filepath.Join(args[1], "child-ready")); err == nil {
				os.Exit(0)
			}
			time.Sleep(time.Millisecond)
		}
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		os.Exit(91)
	case "child":
		_ = os.WriteFile(filepath.Join(args[1], "child-ready"), nil, 0600)
		until := time.Now().Add(4 * time.Second)
		for time.Now().Before(until) {
			if _, err := os.Stat(filepath.Join(args[1], "stop")); err == nil {
				_ = os.WriteFile(filepath.Join(args[1], "child-exit"), nil, 0600)
				os.Exit(0)
			}
			time.Sleep(5 * time.Millisecond)
		}
		os.Exit(124)
	case "adapter":
		var req Request
		decoder := json.NewDecoder(os.Stdin)
		if err := decoder.Decode(&req); err != nil {
			os.Exit(92)
		}
		encoder := json.NewEncoder(os.Stdout)
		emit := func(e Event) { e.V = 1; _ = encoder.Encode(e) }
		emit(Event{Kind: "started", PID: os.Getpid()})
		switch args[1] {
		case "duplicate":
			emit(Event{Kind: "started", PID: os.Getpid()})
		case "unknown":
			emit(Event{Kind: "invented"})
		case "oversize":
			fmt.Println(strings.Repeat("x", maxProtocolLine+1))
		case "prefix":
			emit(Event{Kind: "stdout_eof", Bytes: 1000, Prefix: strings.Repeat("P", req.CaptureLimit+1)})
		case "missing":
			os.Exit(0)
		case "version":
			fmt.Println(`{"v":2,"kind":"parent_exit"}`)
		case "extra-json":
			fmt.Println(`{"v":1,"kind":"parent_exit"} {}`)
		case "missing-code":
			fmt.Println(`{"v":1,"kind":"parent_exit"}`)
		case "missing-bytes":
			fmt.Println(`{"v":1,"kind":"stdout_eof"}`)
		case "null-code":
			fmt.Println(`{"v":1,"kind":"parent_exit","exit_code":null}`)
		case "event-limit":
			for range 17 {
				emit(Event{Kind: "unsupported", Message: "repeat"})
			}
		case "hang":
			time.Sleep(5 * time.Second)
		case "command":
			var c struct {
				V     int
				Op    string
				Bytes int
			}
			if err := decoder.Decode(&c); err != nil || c.V != 1 || c.Op != "write_stdin" || c.Bytes != StdinWriteBytes {
				emit(Event{Kind: "error", Message: "incorrect command"})
				os.Exit(93)
			}
		}
		emit(Event{Kind: "parent_exit"})
		emit(Event{Kind: "stdout_eof", Bytes: 3, Prefix: "ok\n"})
		emit(Event{Kind: "stderr_eof"})
	}
	os.Exit(0)
}
