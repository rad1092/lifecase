package runner

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"sync/atomic"
	"time"
)

// Native exercises Go's os/exec using independent owned pipes. In particular,
// exec.Cmd.Wait cannot close these readers before a descendant releases them.
type Native struct{}

type nativeSession struct {
	stream       *eventStream
	cmd          *exec.Cmd
	stdin        *os.File
	stdout       *os.File
	stderr       *os.File
	finished     chan struct{}
	parentEnd    chan struct{}
	parentReaped atomic.Bool
	closing      atomic.Bool
	closeOnce    sync.Once
	closeErr     error
	errorOnce    sync.Once
	signalOnce   sync.Once
	commandMu    sync.Mutex
	writeDone    chan struct{}
}

func (Native) Start(ctx context.Context, req Request) (Session, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := validateRequest(req); err != nil {
		return nil, err
	}
	var files []*os.File
	pipe := func() (*os.File, *os.File, error) {
		r, w, err := os.Pipe()
		if err == nil {
			files = append(files, r, w)
		}
		return r, w, err
	}
	closeFiles := func() {
		for _, f := range files {
			_ = f.Close()
		}
	}
	inR, inW, err := pipe()
	if err != nil {
		return nil, err
	}
	outR, outW, err := pipe()
	if err != nil {
		closeFiles()
		return nil, err
	}
	errR, errW, err := pipe()
	if err != nil {
		closeFiles()
		return nil, err
	}
	cmd := exec.Command(req.Fixture, req.Args...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = inR, outW, errW
	if err = ctx.Err(); err == nil {
		err = cmd.Start()
	}
	if err != nil {
		closeFiles()
		return nil, err
	}
	_ = inR.Close()
	_ = outW.Close()
	_ = errW.Close()
	s := &nativeSession{stream: newEventStream(), cmd: cmd, stdin: inW,
		stdout: outR, stderr: errR, finished: make(chan struct{}), parentEnd: make(chan struct{})}
	s.stream.emit(Event{Kind: "started", PID: cmd.Process.Pid})
	var wg sync.WaitGroup
	wg.Add(3)
	go func() {
		defer wg.Done()
		err := cmd.Wait()
		// On Windows Wait releases the process handle. A subsequent Kill may
		// therefore return EINVAL rather than os.ErrProcessDone. Publish the
		// successful reap before the event, without readers racing ProcessState.
		if cmd.ProcessState != nil {
			s.parentReaped.Store(true)
		}
		close(s.parentEnd)
		_ = s.stdin.Close()
		if cmd.ProcessState != nil {
			s.stream.emit(Event{Kind: "parent_exit", ExitCode: cmd.ProcessState.ExitCode()})
		} else if err != nil {
			s.reportError(fmt.Errorf("wait: %w", err))
		}
	}()
	for _, output := range []struct {
		file *os.File
		kind string
	}{{outR, "stdout_eof"}, {errR, "stderr_eof"}} {
		go func(f *os.File, kind string) {
			defer wg.Done()
			s.drain(f, kind, req.CaptureLimit)
		}(output.file, output.kind)
	}
	go func() {
		wg.Wait()
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
	if err := ctx.Err(); err != nil {
		_ = s.Close()
		return nil, err
	}
	return s, nil
}

func (s *nativeSession) Events() <-chan Event { return s.stream.ch }

func (s *nativeSession) reportError(err error) {
	if !s.closing.Load() {
		s.errorOnce.Do(func() { s.stream.emit(Event{Kind: "error", Message: err.Error()}) })
	}
}

func (s *nativeSession) drain(f *os.File, kind string, limit int) {
	defer f.Close()
	buf := make([]byte, 32768)
	prefix := make([]byte, 0, limit)
	var total int64
	for {
		n, err := f.Read(buf)
		total += int64(n)
		if remaining := limit - len(prefix); remaining > 0 {
			prefix = append(prefix, buf[:min(n, remaining)]...)
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				// Closing a read handle is not pipe EOF and must never pass a contract.
				if !s.closing.Load() {
					s.stream.emit(Event{Kind: kind, Bytes: total, Prefix: string(prefix)})
				}
			} else {
				s.reportError(fmt.Errorf("%s read: %w", kind, err))
			}
			return
		}
	}
}

func (s *nativeSession) Command(op string) error {
	s.commandMu.Lock()
	defer s.commandMu.Unlock()
	if s.closing.Load() {
		return os.ErrClosed
	}
	switch op {
	case "kill":
		return s.controlParent(s.cmd.Process.Kill)
	case "signal":
		err := s.controlParent(func() error { return terminate(s.cmd.Process) })
		if errors.Is(err, ErrUnsupported) {
			s.signalOnce.Do(func() { s.stream.emit(Event{Kind: "unsupported", Message: "POSIX SIGTERM is unavailable on Windows"}) })
			return nil
		}
		return err
	case "close_stdin":
		err := s.stdin.Close()
		if errors.Is(err, os.ErrClosed) {
			return nil
		}
		return err
	case "write_stdin":
		if s.writeDone != nil {
			return errors.New("write_stdin may be requested only once per session")
		}
		// The fixture may never read. A blocked write must not block cancellation,
		// command processing, or output drains. Close/parent exit closes stdin.
		s.writeDone = make(chan struct{})
		go func() {
			defer close(s.writeDone)
			_, _ = s.stdin.Write(make([]byte, StdinWriteBytes))
		}()
		return nil
	default:
		return fmt.Errorf("unknown command %q", op)
	}
}

// controlParent makes commands idempotent after a confirmed reap. If Wait
// releases the handle between the check and the command, only independent wait
// completion can turn the command's error into success.
func (s *nativeSession) controlParent(control func() error) error {
	if s.parentReaped.Load() {
		return nil
	}
	err := control()
	if err == nil || errors.Is(err, os.ErrProcessDone) {
		return nil
	}
	if errors.Is(err, ErrUnsupported) {
		return err
	}
	select {
	case <-s.parentEnd:
	case <-time.After(100 * time.Millisecond):
	}
	if s.parentReaped.Load() {
		return nil
	}
	return err
}

func (s *nativeSession) Close() error {
	s.closeOnce.Do(func() {
		s.commandMu.Lock()
		s.closing.Store(true)
		if !s.parentReaped.Load() {
			err := s.cmd.Process.Kill()
			if err != nil && !errors.Is(err, os.ErrProcessDone) {
				s.closeErr = err
			}
		}
		_ = s.stdin.Close()
		_ = s.stdout.Close()
		_ = s.stderr.Close()
		writeDone := s.writeDone
		s.commandMu.Unlock()
		deadline := time.NewTimer(2 * time.Second)
		defer deadline.Stop()
		select {
		case <-s.finished:
		case <-deadline.C:
			s.closeErr = errors.Join(s.closeErr, errors.New("fixture did not reap within cleanup deadline"))
			return
		}
		if s.parentReaped.Load() {
			// A failed Kill racing Wait is harmless only now that reaping is
			// independently confirmed. Unexpected errors without proof remain.
			s.closeErr = nil
		} else {
			s.closeErr = errors.Join(s.closeErr, errors.New("fixture wait did not confirm parent exit"))
		}
		if writeDone != nil {
			select {
			case <-writeDone:
			case <-deadline.C:
				s.closeErr = errors.Join(s.closeErr, errors.New("fixture stdin write did not stop within cleanup deadline"))
			}
		}
	})
	return s.closeErr
}
