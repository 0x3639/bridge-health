package localstate

import (
	"os"
	"path/filepath"
	"testing"
)

func TestStateOwnershipAndExclusiveWriter(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state")
	a, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if b, err := Open(path); err == nil {
		b.Close()
		t.Fatal("concurrent writer acquired lock")
	}
	if err = a.Close(); err != nil {
		t.Fatal(err)
	}
	b, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	b.Close()
	if err = os.Chmod(path, 0755); err != nil {
		t.Fatal(err)
	}
	if c, err := Open(path); err == nil {
		c.Close()
		t.Fatal("accepted nonprivate state directory")
	}
}
