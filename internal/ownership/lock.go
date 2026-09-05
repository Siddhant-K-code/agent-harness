// Package ownership provides the local controller's exclusive process lock.
// Leases describe liveness; the OS lock prevents takeover of a paused worker.
package ownership

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
)

var ErrActive = errors.New("a controller still owns this run")

type Lock struct{ file *os.File }

func Acquire(root string) (*Lock, error) {
	if err := os.MkdirAll(root, 0700); err != nil {
		return nil, err
	}
	fd, err := syscall.Open(filepath.Join(root, "worker.lock"), syscall.O_CREAT|syscall.O_RDWR|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0600)
	if err != nil {
		return nil, err
	}
	f := os.NewFile(uintptr(fd), "worker.lock")
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		f.Close()
		return nil, errors.New("invalid worker lock")
	}
	if err := syscall.Flock(fd, syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, ErrActive
		}
		return nil, err
	}
	return &Lock{file: f}, nil
}

func (l *Lock) Close() error { return l.file.Close() }
