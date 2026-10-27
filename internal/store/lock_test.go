package store

import (
	"errors"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/li-ziyin/memorytree/internal/index"
)

func lockPath(root string) string {
	return filepath.Join(root, ".index", lockName)
}

func TestAddEntryBlockedByFreshLock(t *testing.T) {
	restore := setLockRetryWindow(t, 200*time.Millisecond)
	defer restore()
	root := newRepo(t)
	if err := os.MkdirAll(filepath.Dir(lockPath(root)), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(lockPath(root), []byte("held"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := AddEntry(root, validInput())
	if !errors.Is(err, ErrWriteLocked) {
		t.Fatalf("err = %v, want ErrWriteLocked", err)
	}
	if n := countFiles(t, filepath.Join(root, "global", "memories")); n != 0 {
		t.Errorf("blocked write left %d file(s)", n)
	}
}

// setLockRetryWindow shortens the contention window for one test and
// returns a restore func, so a permanently held lock fails fast instead of
// after the production 20 seconds.
func setLockRetryWindow(t *testing.T, d time.Duration) func() {
	t.Helper()
	old := lockRetryWindow
	lockRetryWindow = d
	return func() { lockRetryWindow = old }
}

// TestAddEntryWaitsOutTransientHolder is the M4 retry acceptance for the
// friendly case: the write waits for a concurrent holder to release and
// then succeeds on its single retry, without any caller-side logic.
func TestAddEntryWaitsOutTransientHolder(t *testing.T) {
	restore := setLockRetryWindow(t, 5*time.Second)
	defer restore()
	root := newRepo(t)
	path := lockPath(root)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("briefly held"), 0o600); err != nil {
		t.Fatal(err)
	}
	go func() {
		time.Sleep(300 * time.Millisecond)
		os.Remove(path)
	}()
	if _, err := AddEntry(root, validInput()); err != nil {
		t.Fatalf("write did not survive a transient lock holder: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("lock file survived a successful write: %v", err)
	}
}

func TestAddEntryStealsStaleLock(t *testing.T) {
	root := newRepo(t)
	path := lockPath(root)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("crashed"), 0o600); err != nil {
		t.Fatal(err)
	}
	stale := time.Now().Add(-lockStaleAfter - time.Minute)
	if err := os.Chtimes(path, stale, stale); err != nil {
		t.Fatal(err)
	}
	if _, err := AddEntry(root, validInput()); err != nil {
		t.Fatalf("stale lock not stolen: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("lock file survived a successful write: %v", err)
	}
}

// TestConcurrentAddsLoseNothing is the M2 concurrency acceptance, tightened
// for M4: parallel writers make exactly one attempt each — the bounded
// lock retry must absorb the contention — and every entry lands exactly
// once in a repository that still validates clean.
func TestConcurrentAddsLoseNothing(t *testing.T) {
	root := newRepo(t)
	const writers, each = 8, 3

	var wg sync.WaitGroup
	var landed atomic.Int64
	var mu sync.Mutex
	paths := map[string]int{}
	for w := 0; w < writers; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for i := 0; i < each; i++ {
				in := validInput()
				in.Title = "并发写入条目"
				in.Summary = "并发验收条目 w-i。"
				res, err := AddEntry(root, in)
				if err != nil {
					t.Errorf("writer %d: %v", w, err)
					return
				}
				mu.Lock()
				paths[res.Path]++
				mu.Unlock()
				landed.Add(1)
			}
		}(w)
	}
	wg.Wait()

	if got := landed.Load(); got != writers*each {
		t.Fatalf("landed = %d, want %d", got, writers*each)
	}
	if n := countFiles(t, filepath.Join(root, "global", "memories")); n != writers*each {
		t.Errorf("files = %d, want %d (unique returned paths: %d)", n, writers*each, len(paths))
	}
	if len(paths) != writers*each {
		t.Errorf("duplicate paths returned: %d unique of %d adds", len(paths), writers*each)
	}
	if _, err := index.Build(root, index.BuildOptions{}); err != nil {
		t.Fatalf("repository invalid after concurrent writes: %v", err)
	}
}
