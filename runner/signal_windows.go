//go:build windows

package runner

import "os"

func terminate(_ *os.Process) error { return ErrUnsupported }
