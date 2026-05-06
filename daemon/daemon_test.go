package daemon

import (
	"math"
	"testing"
)

func TestPidFile(t *testing.T) {
	path := t.TempDir() + "/test.pid"

	pidPath, err := PidPath()
	if err != nil {
		t.Fatalf("PidPath: %v", err)
	}
	if pidPath == "" {
		t.Errorf("PidPath returned empty")
	}

	if err := WritePid(path, 12345); err != nil {
		t.Fatalf("WritePid: %v", err)
	}

	pid, err := ReadPid(path)
	if err != nil {
		t.Fatalf("ReadPid: %v", err)
	}
	if pid != 12345 {
		t.Errorf("ReadPid = %d, want 12345", pid)
	}

	// IsRunning must not panic for a guaranteed non-existent PID
	IsRunning(math.MaxInt32)

	RemovePid(path)

	if _, err := ReadPid(path); err == nil {
		t.Errorf("ReadPid after RemovePid: expected error")
	}
}