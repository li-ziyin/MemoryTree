package store

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"github.com/li-ziyin/memorytree/internal/index"
)

// ErrWriteLocked means another memoryd process holds the write lock. The
// caller may retry; no data was touched.
var ErrWriteLocked = errors.New("another memoryd write is in progress")

// lockStaleAfter bounds how long a crashed process can block writers. A
// var so tests can shorten it.
var lockStaleAfter = 10 * time.Minute

const lockName = "write.lock"

// writeLock is the single-writer lock for one data repository. It lives in
// the disposable .index directory and never survives into git.
type writeLock struct {
	path string
}

// acquireWriteLock creates .index/write.lock exclusively. On Windows,
// concurrent EXCL creates can surface as sharing violations instead of
// ErrExist; those are treated as contention and retried with a short
// backoff. A lock whose file is older than lockStaleAfter is considered a
// crash leftover and is stolen; anything fresher fails with ErrWriteLocked.
func acquireWriteLock(root string) (*writeLock, error) {
	dir := filepath.Join(root, index.DirName)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	path := filepath.Join(dir, lockName)
	var lastErr error
	for attempt := 0; attempt < 4; attempt++ {
		if attempt > 0 {
			time.Sleep(time.Duration(attempt) * 5 * time.Millisecond)
		}
		f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err == nil {
			_, werr := f.WriteString(now().Format(time.RFC3339))
			cerr := f.Close()
			if werr != nil {
				return nil, werr
			}
			if cerr != nil {
				return nil, cerr
			}
			return &writeLock{path: path}, nil
		}
		lastErr = err
		if errors.Is(err, fs.ErrExist) || isWindowsSharingViolation(err) {
			info, statErr := os.Stat(path)
			if statErr != nil {
				continue // lock vanished between create and stat; retry
			}
			if age := now().Sub(info.ModTime()); age < lockStaleAfter {
				return nil, fmt.Errorf("%w (held since %s)", ErrWriteLocked, info.ModTime().Format(time.RFC3339))
			}
			if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
				return nil, err
			}
			continue
		}
		return nil, err
	}
	if errors.Is(lastErr, fs.ErrExist) || isWindowsSharingViolation(lastErr) {
		return nil, ErrWriteLocked
	}
	return nil, lastErr
}

// isWindowsSharingViolation recognises ERROR_SHARING_VIOLATION (32) and
// ERROR_ACCESS_DENIED races from concurrent exclusive creates; Go's syscall
// package does not export the sharing-violation constant.
func isWindowsSharingViolation(err error) bool {
	var errno syscall.Errno
	if errors.As(err, &errno) {
		return errno == 32 || errno == syscall.ERROR_ACCESS_DENIED
	}
	return false
}

// release removes the lock. Failing to release only delays the next writer
// until the staleness window passes.
func (l *writeLock) release() error {
	if l == nil {
		return nil
	}
	return os.Remove(l.path)
}
