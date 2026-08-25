package matcher

import (
	"reflect"
	"testing"
)

func TestApplicationSpecificPatterns(t *testing.T) {
	t.Parallel()
	tests := []struct {
		key, kind string
		want      []string
	}{
		{"NOT_QUALITY_UPGRADE", "radarr", []string{"Not an upgrade for existing movie"}},
		{"NOT_QUALITY_UPGRADE", "lidarr", []string{"Not an upgrade for existing track file", "Not an upgrade for existing album file"}},
		{"DANGEROUS_FILE", "lidarr", []string{"Found executable file"}},
		{"MATCHED_VIA_GRAB_HISTORY", "whisparr", []string{"Found matching series via grab history", "Found matching movie via grab history"}},
		{"EPISODE_UNEXPECTED_FOLDER", "sonarr", []string{"was unexpected considering the", "were unexpected considering the"}},
		{"ALBUM_ALREADY_IMPORTED", "sonarr", nil},
		{"SAMPLE", "whisparr", []string{"Sample"}},
		{"SAMPLE_INDETERMINATE", "whisparr", []string{"Unable to determine if file is a sample"}},
	}
	for _, test := range tests {
		if got := Patterns(test.key, test.kind); !reflect.DeepEqual(got, test.want) {
			t.Errorf("Patterns(%q, %q) = %v, want %v", test.key, test.kind, got, test.want)
		}
	}
}

func TestPatternSlicesAreOwnedByCaller(t *testing.T) {
	t.Parallel()
	patterns := Patterns("SAMPLE", "sonarr")
	patterns[0] = "changed"
	if got := Patterns("SAMPLE", "sonarr")[0]; got != "Sample" {
		t.Fatalf("global matcher state was mutated: %q", got)
	}
}
