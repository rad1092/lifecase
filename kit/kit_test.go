package kit

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"github.com/rad1092/lifecase/runner"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestNativeContracts(t *testing.T) {
	f := os.Getenv("LIFECASE_FIXTURE")
	if f == "" {
		t.Skip("set LIFECASE_FIXTURE to run real native fixture contracts")
	}
	r, e := Run(context.Background(), Options{Fixture: f})
	if e != nil {
		t.Fatal(e)
	}
	if r.Failed() {
		b, _ := json.MarshalIndent(r, "", "  ")
		t.Fatalf("contracts failed:\n%s", b)
	}
	if len(r.Results) != len(Scenarios) {
		t.Fatal("missing scenarios")
	}
}
func TestRejectUnknownScenarioBeforeLaunch(t *testing.T) {
	f, e := os.CreateTemp(t.TempDir(), "fixture")
	if e != nil {
		t.Fatal(e)
	}
	f.Close()
	_, e = Run(context.Background(), Options{Fixture: f.Name(), Scenarios: []string{"typo"}})
	if e == nil {
		t.Fatal("unknown scenario accepted")
	}
}
func TestJUnitIncludesFailuresAndSkipped(t *testing.T) {
	r := Report{OS: "test", Results: []Result{{Scenario: "pass", Status: "pass"}, {Scenario: "bad<&", Status: "fail", Assertions: []Assertion{{Name: "output", Pass: false, Detail: "wrong < bytes"}}}, {Scenario: "signal", Status: "unsupported"}}}
	var b strings.Builder
	if e := r.WriteJUnit(&b); e != nil {
		t.Fatal(e)
	}
	var v suite
	if e := xml.Unmarshal([]byte(b.String()), &v); e != nil {
		t.Fatal(e)
	}
	if v.Tests != 3 || v.Failures != 1 || v.Skipped != 1 || v.Cases[1].Failure == nil {
		t.Fatal(v)
	}
}
func TestMarkerRejectsForeignOwnership(t *testing.T) {
	d := t.TempDir()
	if e := os.WriteFile(d+"/parent-ready.json", []byte(`{"v":1,"token":"foreign","pid":123,"role":"parent","event":"ready"}`), 0600); e != nil {
		t.Fatal(e)
	}
	if _, ok := readMarker(d, "parent-ready.json", "owned", "parent", "ready"); ok {
		t.Fatal("foreign marker accepted")
	}
}

// A common incorrect adapter equates parent exit with stream completion.
// Run this mutation against the real inherited-pipe fixture, not a fake child.
type prematureEOFRunner struct{}
type prematureEOFSession struct {
	runner.Session
	events chan runner.Event
}

func (s *prematureEOFSession) Events() <-chan runner.Event { return s.events }
func (prematureEOFRunner) Start(ctx context.Context, req runner.Request) (runner.Session, error) {
	owned, err := (runner.Native{}).Start(ctx, req)
	if err != nil {
		return nil, err
	}
	s := &prematureEOFSession{Session: owned, events: make(chan runner.Event, 8)}
	go func() {
		defer close(s.events)
		for e := range owned.Events() {
			if e.Kind == "started" {
				s.events <- e
			}
			if e.Kind == "parent_exit" {
				s.events <- e
				s.events <- runner.Event{V: 1, Kind: "stdout_eof"}
				s.events <- runner.Event{V: 1, Kind: "stderr_eof"}
				return
			}
		}
	}()
	return s, nil
}
func TestRejectsParentExitMasqueradingAsEOF(t *testing.T) {
	f := os.Getenv("LIFECASE_FIXTURE")
	if f == "" {
		t.Skip("requires real native fixture")
	}
	r, e := Run(context.Background(), Options{Fixture: f, Runner: prematureEOFRunner{}, Scenarios: []string{"inherit"}})
	if e != nil {
		t.Fatal(e)
	}
	if !r.Failed() {
		t.Fatal("incorrect runner falsely passed inherited-pipe policy")
	}
	if !r.Results[0].DescendantClean {
		t.Fatal("mutation test leaked its fixture descendant")
	}
}

type lateErrorRunner struct{}
type lateErrorSession struct {
	runner.Session
	events chan runner.Event
}

func (s *lateErrorSession) Events() <-chan runner.Event { return s.events }
func (lateErrorRunner) Start(ctx context.Context, req runner.Request) (runner.Session, error) {
	owned, e := (runner.Native{}).Start(ctx, req)
	if e != nil {
		return nil, e
	}
	s := &lateErrorSession{Session: owned, events: make(chan runner.Event, 8)}
	go func() {
		defer close(s.events)
		for e := range owned.Events() {
			s.events <- e
		}
		s.events <- runner.Event{V: 1, Kind: "error", Message: "duplicate EOF after normal milestones"}
	}()
	return s, nil
}
func TestRejectsAdapterErrorAfterAllMilestones(t *testing.T) {
	f := os.Getenv("LIFECASE_FIXTURE")
	if f == "" {
		t.Skip("requires real native fixture")
	}
	r, e := Run(context.Background(), Options{Fixture: f, Runner: lateErrorRunner{}, Scenarios: []string{"exit"}})
	if e != nil {
		t.Fatal(e)
	}
	if !r.Failed() {
		t.Fatal("late adapter failure ignored")
	}
}

type delayedStartedRunner struct{}

func (delayedStartedRunner) Start(ctx context.Context, req runner.Request) (runner.Session, error) {
	owned, e := (runner.Native{}).Start(ctx, req)
	if e != nil {
		return nil, e
	}
	s := &lateErrorSession{Session: owned, events: make(chan runner.Event, 8)}
	go func() {
		defer close(s.events)
		for e := range owned.Events() {
			if e.Kind == "started" {
				time.Sleep(70 * time.Millisecond)
			}
			s.events <- e
		}
	}()
	return s, nil
}
func TestReadinessCanPrecedeStartedDelivery(t *testing.T) {
	f := os.Getenv("LIFECASE_FIXTURE")
	if f == "" {
		t.Skip("requires real native fixture")
	}
	r, e := Run(context.Background(), Options{Fixture: f, Runner: delayedStartedRunner{}, Scenarios: []string{"exit"}})
	if e != nil {
		t.Fatal(e)
	}
	if r.Failed() {
		t.Fatalf("transport latency caused false failure: %+v", r.Results)
	}
}

type startFailureRunner struct{}

func (startFailureRunner) Start(ctx context.Context, req runner.Request) (runner.Session, error) {
	owned, e := (runner.Native{}).Start(ctx, req)
	if e != nil {
		return nil, e
	}
	// Model cancellation discovered only after the owned fixture spawned its child.
	dir := ""
	for i, a := range req.Args {
		if a == "--dir" {
			dir = req.Args[i+1]
		}
	}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if _, e := os.Stat(filepath.Join(dir, "parent-ready.json")); e == nil {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	_ = owned.Close()
	return nil, errors.New("simulated startup transport failure")
}
func TestStartFailureCleansAlreadySpawnedDescendant(t *testing.T) {
	f := os.Getenv("LIFECASE_FIXTURE")
	if f == "" {
		t.Skip("requires real native fixture")
	}
	r, e := Run(context.Background(), Options{Fixture: f, Runner: startFailureRunner{}, Scenarios: []string{"inherit"}})
	if e != nil {
		t.Fatal(e)
	}
	if !r.Failed() || !r.Results[0].DescendantClean {
		t.Fatalf("failure cleanup incorrect: %+v", r.Results)
	}
}
