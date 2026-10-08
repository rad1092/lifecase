// Package runner runs the owned fixture and external protocol-v1 adapters.
// It reports parent exit independently from output EOF. Sessions must be closed,
// even after all events arrive; callers own descendant cleanup via fixture stop.
package runner

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
)

const (
	ProtocolVersion = 1
	// 8192 bytes remain within the 64 KiB adapter line limit even when every
	// prefix byte requires JSON's six-byte Unicode escape representation.
	MaxCaptureLimit = 8192
	StdinWriteBytes = 262144
)

// Request describes one direct fixture launch. CaptureLimit bounds each retained
// prefix, not the number of bytes drained from the corresponding output stream.
type Request struct {
	V            int      `json:"v"`
	Op           string   `json:"op"`
	Fixture      string   `json:"fixture"`
	Args         []string `json:"args"`
	CaptureLimit int      `json:"capture_limit"`
}

// Event is one protocol-v1 observation. Prefix contains at most CaptureLimit
// bytes. ExitCode is the fixture's exit code; -1 denotes signal termination in Go.
type Event struct {
	V        int    `json:"v"`
	Kind     string `json:"kind"`
	PID      int    `json:"pid,omitempty"`
	ExitCode int    `json:"exit_code"`
	Bytes    int64  `json:"bytes"`
	Prefix   string `json:"prefix,omitempty"`
	Message  string `json:"message,omitempty"`
}

type Session interface {
	Events() <-chan Event
	Command(op string) error
	Close() error
}

type Runner interface {
	Start(context.Context, Request) (Session, error)
}

var ErrUnsupported = errors.New("operation unsupported on this platform")

func validateRequest(req Request) error {
	if req.V != ProtocolVersion || req.Op != "start" {
		return errors.New("request requires v=1 and op=start")
	}
	if !filepath.IsAbs(req.Fixture) {
		return errors.New("fixture executable must be an absolute path")
	}
	if req.CaptureLimit < 0 || req.CaptureLimit > MaxCaptureLimit {
		return fmt.Errorf("capture_limit must be between 0 and %d", MaxCaptureLimit)
	}
	if len(req.Args) > 128 {
		return errors.New("too many fixture arguments")
	}
	n := len(req.Fixture)
	for _, arg := range req.Args {
		n += len(arg)
	}
	if n > 16384 {
		return errors.New("fixture arguments exceed 16384 bytes")
	}
	return nil
}

// A session emits at most 18 events. The buffer lets cleanup finish even when a
// caller stops consuming; the mutex serializes producer completion and commands.
type eventStream struct {
	mu     sync.Mutex
	ch     chan Event
	closed bool
}

func newEventStream() *eventStream { return &eventStream{ch: make(chan Event, 32)} }

func (s *eventStream) emit(e Event) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.closed {
		e.V = ProtocolVersion
		s.ch <- e
	}
}

func (s *eventStream) finish() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.closed {
		s.closed = true
		close(s.ch)
	}
}
