package lockfile

import (
	"path/filepath"
	"testing"
	"time"
)

func TestAcquireExcludesASecondHolder(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "test.lock")

	release, err := Acquire(path)
	if err != nil {
		t.Fatalf("Acquire returned error: %v", err)
	}

	acquired := make(chan struct{})
	go func() {
		second, err := Acquire(path)
		if err != nil {
			t.Errorf("second Acquire returned error: %v", err)
			return
		}
		close(acquired)
		second()
	}()

	select {
	case <-acquired:
		t.Fatal("second Acquire succeeded while the lock was held")
	case <-time.After(50 * time.Millisecond):
	}

	release()
	select {
	case <-acquired:
	case <-time.After(2 * time.Second):
		t.Fatal("second Acquire did not succeed after release")
	}
}
