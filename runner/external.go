package runner

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const maxProtocolLine = 65536

// External runs a trusted adapter executable with one JSON Lines session.
// Its stdout is a strictly validated control stream; stderr is bounded diagnostic
// capture. Closing asks the adapter to clean its fixture up by sending input EOF.
type External struct {
	Executable string
	Args       []string
}

type externalSession struct {
	stream     *eventStream
	cmd        *exec.Cmd
	stdin      io.WriteCloser
	stdout     *os.File
	processEnd chan struct{}
	scanEnd    chan struct{}
	finished   chan struct{}
	closing    atomic.Bool
	writeMu    sync.Mutex
	stopOnce   sync.Once
	closeOnce  sync.Once
	errorOnce  sync.Once
	closeErr   error
}

type diagnosticCapture struct {
	mu     sync.Mutex
	prefix []byte
	bytes  int64
}

func (b *diagnosticCapture) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.bytes += int64(len(p))
	if remaining := 4096 - len(b.prefix); remaining > 0 {
		b.prefix = append(b.prefix, p[:min(remaining, len(p))]...)
	}
	return len(p), nil
}

func (b *diagnosticCapture) summary() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return fmt.Sprintf("stderr bytes=%d prefix=%q", b.bytes, b.prefix)
}

func (r External) Start(ctx context.Context, req Request) (Session, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := validateRequest(req); err != nil {
		return nil, err
	}
	cmd := exec.Command(r.Executable, r.Args...)
	cmd.WaitDelay = time.Second
	in, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	outR, outW, err := os.Pipe()
	if err != nil {
		_ = in.Close()
		return nil, err
	}
	diagnostics := &diagnosticCapture{}
	cmd.Stdout, cmd.Stderr = outW, diagnostics
	if err = ctx.Err(); err == nil {
		err = cmd.Start()
	}
	_ = outW.Close()
	if err != nil {
		_ = in.Close()
		_ = outR.Close()
		return nil, err
	}
	s := &externalSession{stream: newEventStream(), cmd: cmd, stdin: in, stdout: outR,
		processEnd: make(chan struct{}), scanEnd: make(chan struct{}), finished: make(chan struct{})}
	go s.scan(req.CaptureLimit)
	go func() {
		err := cmd.Wait()
		close(s.processEnd)
		if err != nil && !s.closing.Load() {
			s.fail(fmt.Errorf("adapter exited: %w (%s)", err, diagnostics.summary()))
		}
		select {
		case <-s.scanEnd:
		case <-time.After(time.Second):
			if !s.closing.Load() {
				s.fail(errors.New("adapter stdout did not close after adapter exit"))
			}
			_ = outR.Close()
			<-s.scanEnd
		}
		_ = in.Close()
		s.stream.finish()
		close(s.finished)
	}()
	go func() {
		select {
		case <-ctx.Done():
			_ = s.Close()
		case <-s.finished:
		}
	}()
	if err := s.write(ctx, req); err != nil {
		_ = s.Close()
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, ctxErr
		}
		return nil, fmt.Errorf("write adapter start request: %w", err)
	}
	if err := ctx.Err(); err != nil {
		_ = s.Close()
		return nil, err
	}
	return s, nil
}

func (s *externalSession) Events() <-chan Event { return s.stream.ch }

func (s *externalSession) fail(err error) {
	s.errorOnce.Do(func() { s.stream.emit(Event{Kind: "error", Message: err.Error()}) })
	s.stop()
}

// stop cannot wait on the reader that may be calling it. First give the adapter
// input EOF to clean its own child up, then kill only the adapter handle we own.
func (s *externalSession) stop() {
	s.stopOnce.Do(func() {
		_ = s.stdin.Close()
		go func() {
			select {
			case <-s.processEnd:
			case <-time.After(500 * time.Millisecond):
				_ = s.cmd.Process.Kill()
			}
			select {
			case <-s.scanEnd:
			case <-time.After(500 * time.Millisecond):
				_ = s.stdout.Close()
			}
		}()
	})
}

func (s *externalSession) scan(captureLimit int) {
	defer close(s.scanEnd)
	defer s.stdout.Close()
	scanner := bufio.NewScanner(s.stdout)
	scanner.Buffer(make([]byte, 4096), maxProtocolLine)
	seen := make(map[string]bool)
	n := 0
	for scanner.Scan() {
		n++
		if n > 16 {
			s.fail(errors.New("adapter exceeded 16 protocol events"))
			return
		}
		e, err := decodeEvent(scanner.Bytes(), captureLimit, seen)
		if err != nil {
			s.fail(fmt.Errorf("adapter protocol: %w", err))
			return
		}
		if e.Kind == "error" {
			s.fail(fmt.Errorf("adapter: %s", e.Message))
			return
		}
		s.stream.emit(e)
	}
	if s.closing.Load() {
		return
	}
	if err := scanner.Err(); err != nil {
		s.fail(fmt.Errorf("adapter protocol read: %w", err))
		return
	}
	for _, kind := range []string{"started", "parent_exit", "stdout_eof", "stderr_eof"} {
		if !seen[kind] {
			s.fail(fmt.Errorf("adapter ended without %s", kind))
			return
		}
	}
}

func decodeEvent(line []byte, captureLimit int, seen map[string]bool) (Event, error) {
	var e Event
	d := json.NewDecoder(bytes.NewReader(line))
	d.DisallowUnknownFields()
	if err := d.Decode(&e); err != nil {
		return e, err
	}
	var extra any
	if err := d.Decode(&extra); !errors.Is(err, io.EOF) {
		return e, errors.New("event must be one JSON object")
	}
	if e.V != ProtocolVersion {
		return e, errors.New("event requires v=1")
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(line, &fields); err != nil {
		return e, err
	}
	requiredNumber := func(name string) bool {
		value, present := fields[name]
		return present && string(value) != "null"
	}
	switch e.Kind {
	case "started", "parent_exit", "stdout_eof", "stderr_eof":
		if seen[e.Kind] {
			return e, fmt.Errorf("duplicate %s", e.Kind)
		}
		if e.Kind != "started" && !seen["started"] {
			return e, errors.New("started must precede lifecycle events")
		}
		if e.Kind == "started" && e.PID <= 0 {
			return e, errors.New("started requires a positive pid")
		}
		if e.Kind == "parent_exit" && !requiredNumber("exit_code") {
			return e, errors.New("parent_exit requires an explicit exit_code")
		}
		if strings.HasSuffix(e.Kind, "_eof") && !requiredNumber("bytes") {
			return e, errors.New("output EOF requires an explicit byte count")
		}
		if strings.HasSuffix(e.Kind, "_eof") && (e.Bytes < 0 || len(e.Prefix) > captureLimit || int64(len(e.Prefix)) > e.Bytes) {
			return e, errors.New("invalid output count or oversized prefix")
		}
		seen[e.Kind] = true
	case "error", "unsupported":
		if e.Message == "" {
			return e, errors.New("diagnostic event requires a message")
		}
	default:
		return e, fmt.Errorf("unknown event kind %q", e.Kind)
	}
	return e, nil
}

func (s *externalSession) write(ctx context.Context, v any) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	if s.closing.Load() {
		return os.ErrClosed
	}
	p, err := json.Marshal(v)
	if err != nil {
		return err
	}
	p = append(p, '\n')
	done := make(chan error, 1)
	go func() {
		_, err := s.stdin.Write(p)
		done <- err
	}()
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		_ = s.stdin.Close()
		return ctx.Err()
	case <-time.After(500 * time.Millisecond):
		_ = s.stdin.Close()
		return errors.New("adapter input write exceeded deadline")
	}
}

func (s *externalSession) Command(op string) error {
	switch op {
	case "kill", "signal", "close_stdin", "write_stdin":
	default:
		return fmt.Errorf("unknown command %q", op)
	}
	command := struct {
		V     int    `json:"v"`
		Op    string `json:"op"`
		Bytes int    `json:"bytes,omitempty"`
	}{V: ProtocolVersion, Op: op}
	if op == "write_stdin" {
		command.Bytes = StdinWriteBytes
	}
	return s.write(context.Background(), command)
}

func (s *externalSession) Close() error {
	s.closeOnce.Do(func() {
		s.closing.Store(true)
		s.stop()
		select {
		case <-s.finished:
		case <-time.After(2500 * time.Millisecond):
			s.closeErr = errors.New("adapter did not reap within cleanup deadline")
		}
	})
	return s.closeErr
}
