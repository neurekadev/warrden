package app

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFixedRuntimePaths(t *testing.T) {
	t.Parallel()
	checks := map[string]string{
		filepath.ToSlash(configPath):         "data/config.yaml",
		filepath.ToSlash(databasePath):       "data/warrden.db",
		filepath.ToSlash(legacyDatabasePath): "data/warden.db",
		filepath.ToSlash(installIDPath):      "data/install-id",
	}
	for got, want := range checks {
		if got != want {
			t.Errorf("path=%q, want %q", got, want)
		}
	}
}

func TestMigrateLegacyDatabaseMovesDatabaseAndSidecars(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	oldPath := filepath.Join(directory, "warden.db")
	newPath := filepath.Join(directory, "warrden.db")
	for _, suffix := range []string{"", "-wal", "-shm"} {
		if err := os.WriteFile(oldPath+suffix, []byte("content"+suffix), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	migrated, err := migrateLegacyDatabase(oldPath, newPath)
	if err != nil {
		t.Fatal(err)
	}
	if !migrated {
		t.Fatal("legacy database was not reported as migrated")
	}
	for _, suffix := range []string{"", "-wal", "-shm"} {
		if _, err := os.Stat(oldPath + suffix); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("legacy file %q still exists: %v", oldPath+suffix, err)
		}
		content, err := os.ReadFile(newPath + suffix)
		if err != nil {
			t.Fatal(err)
		}
		if got, want := string(content), "content"+suffix; got != want {
			t.Fatalf("%s=%q, want %q", suffix, got, want)
		}
	}
}

func TestMigrateLegacyDatabaseRefusesAmbiguousFiles(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		old    []string
		new    []string
		wanted string
	}{
		{name: "both databases", old: []string{""}, new: []string{""}, wanted: "both legacy and current"},
		{name: "orphan legacy sidecar", old: []string{"-wal"}, wanted: "legacy database sidecar"},
		{name: "orphan current sidecar", new: []string{"-shm"}, wanted: "current database sidecar"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			directory := t.TempDir()
			oldPath := filepath.Join(directory, "warden.db")
			newPath := filepath.Join(directory, "warrden.db")
			for _, suffix := range test.old {
				if err := os.WriteFile(oldPath+suffix, []byte("old"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			for _, suffix := range test.new {
				if err := os.WriteFile(newPath+suffix, []byte("new"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			migrated, err := migrateLegacyDatabase(oldPath, newPath)
			if migrated || err == nil || !strings.Contains(err.Error(), test.wanted) {
				t.Fatalf("migrated=%t err=%v, want %q", migrated, err, test.wanted)
			}
		})
	}
}

func TestMigrateLegacyDatabaseRollsBackPartialRename(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	oldPath := filepath.Join(directory, "warden.db")
	newPath := filepath.Join(directory, "warrden.db")
	for _, suffix := range []string{"", "-wal"} {
		if err := os.WriteFile(oldPath+suffix, []byte("content"+suffix), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	forwardCalls := 0
	rename := func(oldName, newName string) error {
		if strings.HasPrefix(oldName, oldPath) {
			forwardCalls++
			if forwardCalls == 2 {
				return errors.New("controlled failure")
			}
		}
		return os.Rename(oldName, newName)
	}
	migrated, err := migrateLegacyDatabaseWith(oldPath, newPath, rename)
	if migrated || err == nil || !strings.Contains(err.Error(), "controlled failure") {
		t.Fatalf("migrated=%t err=%v", migrated, err)
	}
	for _, suffix := range []string{"", "-wal"} {
		if _, err := os.Stat(oldPath + suffix); err != nil {
			t.Fatalf("legacy file %q was not restored: %v", oldPath+suffix, err)
		}
		if _, err := os.Stat(newPath + suffix); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("current file %q remains after rollback: %v", newPath+suffix, err)
		}
	}
}
