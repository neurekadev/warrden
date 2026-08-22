package identity

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestConfiguredIDs(t *testing.T) {
	tests := []struct {
		name string
		puid *string
		pgid *string
		want ids
		err  string
	}{
		{name: "defaults", want: ids{uid: 1000, gid: 1000}},
		{name: "explicit", puid: stringPtr("123"), pgid: stringPtr("456"), want: ids{uid: 123, gid: 456}},
		{name: "root", puid: stringPtr("0"), pgid: stringPtr("0"), want: ids{}},
		{name: "missing PGID", puid: stringPtr("1000"), err: "must be set together"},
		{name: "mixed root", puid: stringPtr("0"), pgid: stringPtr("1000"), err: "both be non-zero or both be 0"},
		{name: "negative", puid: stringPtr("-1"), pgid: stringPtr("1000"), err: "PUID must be a non-negative integer"},
		{name: "invalid", puid: stringPtr("user"), pgid: stringPtr("1000"), err: "PUID must be a non-negative integer"},
		{name: "overflow", puid: stringPtr("4294967296"), pgid: stringPtr("1000"), err: "PUID must be a non-negative integer"},
		{name: "reserved maximum", puid: stringPtr("4294967295"), pgid: stringPtr("1000"), err: "PUID must be a non-negative integer"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if test.puid == nil {
				t.Setenv("PUID", "")
				t.Setenv("PGID", "")
				// Setenv cannot model absence; clear the variables after registering cleanup.
				t.Chdir(t.TempDir())
				unsetForTest(t, "PUID")
				unsetForTest(t, "PGID")
			} else {
				t.Setenv("PUID", *test.puid)
				if test.pgid == nil {
					unsetForTest(t, "PGID")
				} else {
					t.Setenv("PGID", *test.pgid)
				}
			}
			got, err := configuredIDs()
			if test.err != "" {
				if err == nil || !contains(err.Error(), test.err) {
					t.Fatalf("got %v, want error containing %q", err, test.err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got != test.want {
				t.Fatalf("got %+v, want %+v", got, test.want)
			}
		})
	}
}

func stringPtr(value string) *string { return &value }

func TestOwnershipPathRefusesFilesystemRootForManagedIdentity(t *testing.T) {
	root := filepath.VolumeName(t.TempDir()) + string(filepath.Separator)
	if _, err := ownershipPath(root, ids{uid: 1000, gid: 1000}); err == nil || !strings.Contains(err.Error(), "filesystem root") {
		t.Fatalf("got %v, want filesystem-root refusal", err)
	}
	if _, err := ownershipPath(root, ids{}); err == nil || !strings.Contains(err.Error(), "filesystem root") {
		t.Fatalf("explicit root identity got %v, want filesystem-root refusal", err)
	}
}
