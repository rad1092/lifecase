package kit

import (
	"syscall"
)

func processAlive(pid int) (bool, error) {
	h, e := syscall.OpenProcess(0x00100000, false, uint32(pid))
	if e != nil {
		if e == syscall.Errno(87) {
			return false, nil
		}
		return false, e
	}
	defer syscall.CloseHandle(h)
	s, e := syscall.WaitForSingleObject(h, 0)
	if e != nil {
		return false, e
	}
	return s == syscall.WAIT_TIMEOUT, nil
}
