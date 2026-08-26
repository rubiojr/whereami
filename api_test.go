package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestPruneDiskEnforcesEntryLimitWithoutTTL(t *testing.T) {
	dir := t.TempDir()
	oldPath := filepath.Join(dir, "old.png")
	newPath := filepath.Join(dir, "new.png")
	for _, path := range []string{oldPath, newPath} {
		if err := os.WriteFile(path, []byte("tile"), 0o644); err != nil {
			t.Fatalf("write tile fixture: %v", err)
		}
	}
	now := time.Now()
	if err := os.Chtimes(oldPath, now.Add(-time.Hour), now.Add(-time.Hour)); err != nil {
		t.Fatalf("set old tile time: %v", err)
	}
	if err := os.Chtimes(newPath, now, now); err != nil {
		t.Fatalf("set new tile time: %v", err)
	}

	proxy := tileProxy{
		diskDir:    dir,
		diskTTL:    0,
		maxEntries: 1,
		maxBytes:   1024,
	}
	proxy.pruneDisk()

	if _, err := os.Stat(oldPath); !os.IsNotExist(err) {
		t.Errorf("old tile still exists after pruning: %v", err)
	}
	if _, err := os.Stat(newPath); err != nil {
		t.Errorf("new tile was removed: %v", err)
	}
}
