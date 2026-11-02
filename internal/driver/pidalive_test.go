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
}
