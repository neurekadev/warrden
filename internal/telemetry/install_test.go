package telemetry

import (
	"encoding/hex"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
)

func TestLoadOrCreateInstallIDPersistsUUID(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "state", "install-id")
	first, err := LoadOrCreateInstallID(path)
	if err != nil {
		t.Fatal(err)
	}
	second, err := LoadOrCreateInstallID(path)
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatalf("installation ID changed from %q to %q", first, second)
	}
	if !validUUID(first) {
		t.Fatalf("installation ID %q is not a UUID", first)
	}
	raw, err := hex.DecodeString(strings.ReplaceAll(first, "-", ""))
	if err != nil {
		t.Fatal(err)
	}
	if raw[6]>>4 != 4 || raw[8]&0xc0 != 0x80 {
		t.Fatalf("installation ID %q is not an RFC 4122 version 4 UUID", first)
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if got := info.Mode().Perm(); got != 0o600 {
			t.Fatalf("installation ID mode=%#o, want 0600", got)
		}
	}
}

func TestLoadOrCreateInstallIDIsStableAcrossConcurrentCallers(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "install-id")
	const callers = 16
	results := make(chan string, callers)
	errors := make(chan error, callers)
	var wait sync.WaitGroup
	for range callers {
		wait.Add(1)
		go func() {
			defer wait.Done()
			installID, err := LoadOrCreateInstallID(path)
			if err != nil {
				errors <- err
				return
			}
			results <- installID
		}()
	}
	wait.Wait()
	close(results)
	close(errors)
	for err := range errors {
		t.Error(err)
	}
	want := ""
	for installID := range results {
		if want == "" {
			want = installID
		}
		if installID != want {
			t.Errorf("concurrent installation IDs differ: %q and %q", want, installID)
		}
	}
}

func TestLoadOrCreateInstallIDRefusesInvalidExistingValue(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "install-id")
	if err := os.WriteFile(path, []byte("not-a-uuid\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadOrCreateInstallID(path); err == nil || !strings.Contains(err.Error(), "not a valid UUID") {
		t.Fatalf("got %v, want invalid UUID error", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := string(data); got != "not-a-uuid\n" {
		t.Fatalf("invalid installation ID was replaced with %q", got)
	}
}

func TestValidUUIDRejectsMalformedValues(t *testing.T) {
	t.Parallel()
	for _, value := range []string{"", "1234", "00000000-0000-0000-0000-00000000000z", "000000000000-0000-0000-000000000000"} {
		if validUUID(value) {
			t.Errorf("validUUID(%q)=true", value)
		}
	}
}
