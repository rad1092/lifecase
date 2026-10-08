// Package kit checks lifecycle contracts against controlled real OS fixtures.
package kit

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/rad1092/lifecase/runner"
)

const Version = "0.1.0"

var Scenarios = []string{"exit", "flood", "stdin-blocked", "cooperative", "uncooperative", "crash", "inherit", "unicode-path", "startup-timeout", "spawn-cancel", "signal"}

type Options struct {
	Fixture   string
	Runner    runner.Runner
	Scenarios []string
	// StartupTimeout defaults to one second; fixture delayed startup is two seconds.
	StartupTimeout time.Duration
}
type Assertion struct {
	Name   string `json:"name"`
	Pass   bool   `json:"pass"`
	Detail string `json:"detail,omitempty"`
}
type Mark struct {
	Kind   string `json:"kind"`
	Millis int64  `json:"ms"`
	Detail string `json:"detail,omitempty"`
}
type Result struct {
	Scenario        string      `json:"scenario"`
	Status          string      `json:"status"`
	DurationMS      int64       `json:"duration_ms"`
	ParentExitMS    *int64      `json:"parent_exit_ms"`
	StdoutEOFMS     *int64      `json:"stdout_eof_ms"`
	StderrEOFMS     *int64      `json:"stderr_eof_ms"`
	DescendantClean bool        `json:"descendant_clean"`
	Shutdown        string      `json:"shutdown"`
	StdoutBytes     int64       `json:"stdout_bytes"`
	StderrBytes     int64       `json:"stderr_bytes"`
	Assertions      []Assertion `json:"assertions"`
	Timeline        []Mark      `json:"timeline"`
}
type Report struct {
	Protocol int      `json:"protocol"`
	Version  string   `json:"version"`
	OS       string   `json:"os"`
	Arch     string   `json:"arch"`
	Results  []Result `json:"results"`
}

func (r Report) Failed() bool {
	for _, v := range r.Results {
		if v.Status == "fail" {
			return true
		}
	}
	return false
}

// Run runs scenarios serially. Only processes launched by Runner can be terminated.
// Native descendants use token-authenticated stop files and independent watchdogs.
func Run(ctx context.Context, o Options) (Report, error) {
	report := Report{Protocol: 1, Version: Version, OS: runtime.GOOS, Arch: runtime.GOARCH, Results: []Result{}}
	path, err := filepath.Abs(o.Fixture)
	if err != nil {
		return report, err
	}
	info, err := os.Stat(path)
	if err != nil {
		return report, err
	}
	if info.IsDir() {
		return report, errors.New("fixture must be an executable file")
	}
	o.Fixture = path
	if o.Runner == nil {
		o.Runner = runner.Native{}
	}
	if o.StartupTimeout == 0 {
		o.StartupTimeout = time.Second
	}
	if o.StartupTimeout < 50*time.Millisecond || o.StartupTimeout > 1500*time.Millisecond {
		return report, errors.New("startup timeout must be 50..1500 ms")
	}
	names := o.Scenarios
	if len(names) == 0 {
		names = append([]string(nil), Scenarios...)
	}
	if len(names) > 32 {
		return report, errors.New("at most 32 scenarios per invocation")
	}
	for _, name := range names {
		known := false
		for _, v := range Scenarios {
			if v == name {
				known = true
			}
		}
		if !known {
			return report, fmt.Errorf("unknown scenario %q", name)
		}
	}
	for _, name := range names {
		if ctx.Err() != nil {
			return report, ctx.Err()
		}
		report.Results = append(report.Results, runOne(ctx, o, name))
	}
	return report, nil
}

type ready struct {
	V     int    `json:"v"`
	Token string `json:"token"`
	PID   int    `json:"pid"`
	Role  string `json:"role"`
	Event string `json:"event"`
}

func readMarker(dir, name, token, role, event string) (ready, bool) {
	var r ready
	b, e := os.ReadFile(filepath.Join(dir, name))
	if e != nil || len(b) > 2048 {
		return r, false
	}
	e = json.Unmarshal(b, &r)
	return r, e == nil && r.V == 1 && r.Token == token && r.PID > 0 && r.Role == role && r.Event == event
}
func runOne(ctx context.Context, o Options, name string) Result {
	start := time.Now()
	r := Result{Scenario: name, Status: "pass", Shutdown: "natural", Assertions: []Assertion{}, Timeline: []Mark{}}
	mark := func(s, d string) { r.Timeline = append(r.Timeline, Mark{s, time.Since(start).Milliseconds(), d}) }
	check := func(s string, b bool, d string) {
		r.Assertions = append(r.Assertions, Assertion{s, b, d})
		if !b {
			r.Status = "fail"
		}
	}
	if name == "signal" && runtime.GOOS == "windows" {
		r.Status = "unsupported"
		r.Shutdown = "unsupported"
		mark("unsupported", "Windows console signaling is not implemented in protocol 1")
		return r
	}
	dir, err := os.MkdirTemp("", "lifecase-")
	if err != nil {
		check("setup", false, err.Error())
		return r
	}
	defer os.RemoveAll(dir)
	tokenBytes := make([]byte, 16)
	if _, err = rand.Read(tokenBytes); err != nil {
		check("token", false, err.Error())
		return r
	}
	token := hex.EncodeToString(tokenBytes)
	control := func(file string) {
		if e := os.WriteFile(filepath.Join(dir, file), []byte(token), 0600); e != nil {
			check("control_"+file, false, e.Error())
		}
	}
	fixture := o.Fixture
	scenario := name
	if name == "unicode-path" {
		scenario = "exit"
		b, e := os.ReadFile(fixture)
		if e != nil {
			check("copy", false, e.Error())
			return r
		}
		sub := filepath.Join(dir, "fixture 공간")
		if e = os.Mkdir(sub, 0700); e != nil {
			check("mkdir", false, e.Error())
			return r
		}
		ext := ""
		if runtime.GOOS == "windows" {
			ext = ".exe"
		}
		fixture = filepath.Join(sub, "child ü"+ext)
		if e = os.WriteFile(fixture, b, 0700); e != nil {
			check("copy", false, e.Error())
			return r
		}
	}
	session, err := o.Runner.Start(ctx, runner.Request{V: 1, Op: "start", Fixture: fixture, Args: []string{"--scenario", scenario, "--dir", dir, "--token", token, "--lease-ms", "5000"}, CaptureLimit: 4096})
	if err != nil {
		check("launch", false, err.Error())
		return r
	}
	defer session.Close()
	events := session.Events()
	tick := time.NewTicker(5 * time.Millisecond)
	defer tick.Stop()
	deadline := time.NewTimer(4 * time.Second)
	defer deadline.Stop()
	var parent ready
	var child ready
	var startedPID int
	readySeen := false
	released := false
	stopped := false
	killed := false
	parentCode := -999
	done := false
	cancelled := false
	inheritanceProved := false
	var releasedAt time.Time
	var parentAt time.Time
	kill := func() {
		if !killed {
			killed = true
			r.Shutdown = "forced"
			mark("force_requested", "")
			if e := session.Command("kill"); e != nil {
				check("force_command", false, e.Error())
			}
		}
	}
	for !done {
		select {
		case e, ok := <-events:
			if !ok {
				events = nil
				if r.ParentExitMS == nil || r.StdoutEOFMS == nil || r.StderrEOFMS == nil {
					check("complete_events", false, "adapter closed before all milestones")
				}
				done = true
				break
			}
			mark(e.Kind, e.Message)
			now := time.Since(start).Milliseconds()
			switch e.Kind {
			case "started":
				startedPID = e.PID
				if name == "spawn-cancel" {
					kill()
					cancelled = true
				}
			case "parent_exit":
				r.ParentExitMS = &now
				parentCode = e.ExitCode
				parentAt = time.Now()
			case "stdout_eof":
				r.StdoutEOFMS = &now
				r.StdoutBytes = e.Bytes
			case "stderr_eof":
				r.StderrEOFMS = &now
				r.StderrBytes = e.Bytes
			case "unsupported":
				if r.Status != "fail" {
					r.Status = "unsupported"
				}
				kill()
			case "error":
				check("adapter", false, e.Message)
				kill()
				done = true
			}
		case <-tick.C:
			if !readySeen && startedPID > 0 {
				if v, ok := readMarker(dir, "parent-ready.json", token, "parent", "ready"); ok {
					parent = v
					readySeen = true
					mark("fixture_ready", "")
					check("owned_parent", parent.PID == startedPID, "fixture token and launched PID must agree")
					if name == "inherit" {
						child, _ = readMarker(dir, "child-ready.json", token, "child", "ready")
						check("child_ready", child.PID > 0, "")
					}
					control("go")
					released = true
					releasedAt = time.Now()
					mark("barrier_released", "")
					if name == "stdin-blocked" {
						if e := session.Command("write_stdin"); e != nil {
							check("stdin_command", false, e.Error())
						}
					}
				}
			}
			if !readySeen && !killed && time.Since(start) > o.StartupTimeout {
				mark("startup_timeout", "")
				kill()
			}
			if released && !stopped && time.Since(releasedAt) > 100*time.Millisecond {
				switch name {
				case "cooperative":
					control("stop")
					stopped = true
					r.Shutdown = "cooperative"
					mark("stop_requested", "")
				case "uncooperative", "stdin-blocked":
					stopped = true
					kill()
				case "signal":
					stopped = true
					r.Shutdown = "signal"
					if e := session.Command("signal"); e != nil {
						check("signal_command", false, e.Error())
					}
				}
			}
			if name == "inherit" && r.ParentExitMS != nil && !stopped && time.Since(parentAt) > 150*time.Millisecond {
				inheritanceProved = r.StdoutEOFMS == nil && r.StderrEOFMS == nil
				check("parent_exit_before_pipe_eof", inheritanceProved, "child still owns inherited streams")
				control("stop")
				stopped = true
				mark("descendant_stop_requested", "")
			}
		case <-ctx.Done():
			check("context", false, ctx.Err().Error())
			control("stop")
			kill()
			done = true
		case <-deadline.C:
			check("deadline", false, "scenario exceeded four seconds")
			control("stop")
			kill()
			done = true
		}
	}
	// Independent cleanup runs even for failed/lying adapters. It never kills marker PIDs.
	control("stop")
	if e := session.Close(); e != nil {
		check("session_cleanup", false, e.Error())
	}
	if child.PID == 0 {
		child, _ = readMarker(dir, "child-ready.json", token, "child", "ready")
	}
	if child.PID > 0 {
		until := time.Now().Add(5200 * time.Millisecond)
		for time.Now().Before(until) {
			_, ack := readMarker(dir, "child-exit.json", token, "child", "exit")
			alive, e := processAlive(child.PID)
			if ack && e == nil && !alive {
				r.DescendantClean = true
				break
			}
			time.Sleep(10 * time.Millisecond)
		}
		check("descendant_cleanup", r.DescendantClean, "token-owned exit marker and OS liveness inventory")
	} else {
		r.DescendantClean = true
	}
	if name == "inherit" {
		check("inherited_pipes_held_until_release", inheritanceProved, "EOF before authenticated descendant stop is a failure")
	}
	check("parent_exit_observed", r.ParentExitMS != nil, "")
	check("stdout_eof_observed", r.StdoutEOFMS != nil, "")
	check("stderr_eof_observed", r.StderrEOFMS != nil, "")
	switch name {
	case "startup-timeout":
		check("startup_deadline", !readySeen && killed, "")
	case "spawn-cancel":
		check("cancellation_before_ready", cancelled && !readySeen, "")
	default:
		check("ready_barrier", readySeen && released, "")
	}
	switch name {
	case "exit", "unicode-path":
		check("exit_code", parentCode == 0, "")
		check("output", r.StdoutBytes == 3 && r.StderrBytes == 0, "")
	case "flood":
		check("exit_code", parentCode == 0, "")
		check("dual_pipe_drain", r.StdoutBytes == 262144 && r.StderrBytes == 262144, "")
	case "crash":
		check("abrupt_exit_code", parentCode == 23, "")
	case "cooperative", "inherit":
		check("exit_code", parentCode == 0, "")
	case "uncooperative", "stdin-blocked", "startup-timeout", "spawn-cancel":
		check("forced_exit", parentCode != 0 && parentCode != -999, "")
	case "signal":
		if r.Status != "unsupported" {
			check("signal_exit", parentCode == 0, "")
		}
	}
	r.DurationMS = time.Since(start).Milliseconds()
	return r
}
