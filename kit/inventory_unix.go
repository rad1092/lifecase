//go:build !windows

package kit

import (
	"errors"
	"fmt"
	"os"
	"runtime"
	"strings"
	"syscall"
)

func processAlive(pid int) (bool, error) {
	if runtime.GOOS == "linux" {
		b, e := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
		if os.IsNotExist(e) {
			return false, nil
		}
		if e == nil {
			end := strings.LastIndex(string(b), ") ")
			if end >= 0 && len(b) > end+2 && b[end+2] == 'Z' {
				return false, nil
			}
		}
	}
	err := syscall.Kill(pid, 0)
	if errors.Is(err, syscall.ESRCH) {
		return false, nil
	}
	return err == nil, err
}
