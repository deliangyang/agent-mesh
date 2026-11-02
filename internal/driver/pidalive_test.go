package driver

import (
	"os"
	"testing"
)

func TestPIDAlive(t *testing.T) {
	if !PIDAlive(os.Getpid()) {
		t.Fatalf("current pid should be alive")
	}
	if PIDAlive(0) || PIDAlive(-1) {
		t.Fatalf("invalid pid should be dead")
	}
	// A pid in the Windows reserved range that is almost never a live process.
	if PIDAlive(1) && os.Getpid() != 1 {
		// On Unix pid 1 is init/launchd; skip. On Windows it should be false.
		if os.PathSeparator == '\\' {
			t.Fatalf("pid 1 should not look alive on Windows")
		}
	}
}
