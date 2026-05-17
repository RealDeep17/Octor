package services

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestCleanerDropsExpiredTouchedCache(t *testing.T) {
	dir := t.TempDir()
	oldHash := "oldhash"
	newHash := "newhash"
	oldDir := filepath.Join(dir, oldHash)
	newDir := filepath.Join(dir, newHash)
	if err := os.Mkdir(oldDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(newDir, 0755); err != nil {
		t.Fatal(err)
	}
	oldTouch := filepath.Join(dir, oldHash+".touch")
	newTouch := filepath.Join(dir, newHash+".touch")
	if err := os.WriteFile(oldTouch, nil, 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(newTouch, nil, 0644); err != nil {
		t.Fatal(err)
	}
	oldTime := time.Now().Add(-25 * time.Hour)
	newTime := time.Now()
	if err := os.Chtimes(oldTouch, oldTime, oldTime); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(newTouch, newTime, newTime); err != nil {
		t.Fatal(err)
	}

	cleaner := NewCleaner(dir, "25%", "35%", 23*time.Hour, time.Minute)
	if err := cleaner.dropExpired(); err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(oldDir); !os.IsNotExist(err) {
		t.Fatalf("expected expired cache dir to be removed, got err=%v", err)
	}
	if _, err := os.Stat(oldTouch); !os.IsNotExist(err) {
		t.Fatalf("expected expired touch file to be removed, got err=%v", err)
	}
	if _, err := os.Stat(newDir); err != nil {
		t.Fatalf("expected fresh cache dir to remain: %v", err)
	}
	if _, err := os.Stat(newTouch); err != nil {
		t.Fatalf("expected fresh touch file to remain: %v", err)
	}
}

func TestCleanerUsesDirModTimeWhenTouchMissing(t *testing.T) {
	dir := t.TempDir()
	hash := "oldhash"
	cacheDir := filepath.Join(dir, hash)
	if err := os.Mkdir(cacheDir, 0755); err != nil {
		t.Fatal(err)
	}
	oldTime := time.Now().Add(-25 * time.Hour)
	if err := os.Chtimes(cacheDir, oldTime, oldTime); err != nil {
		t.Fatal(err)
	}

	cleaner := NewCleaner(dir, "25%", "35%", 23*time.Hour, time.Minute)
	if err := cleaner.dropExpired(); err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(cacheDir); !os.IsNotExist(err) {
		t.Fatalf("expected expired cache dir without touch file to be removed, got err=%v", err)
	}
}
