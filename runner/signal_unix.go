//go:build !windows

package runner

import (
	"os"
	"syscall"
)

func terminate(p *os.Process) error { return p.Signal(syscall.SIGTERM) }
