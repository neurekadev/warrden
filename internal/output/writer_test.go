package output

import (
	"bytes"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/neurekadev/warrden/internal/config"
)

func TestWriterExactLogContract(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		write func(*Writer)
		want  string
	}{
		{
			name: "debug",
			write: func(writer *Writer) {
				writer.Debug("warden.config", "Loaded config from data/config.yaml")
			},
			want: "\x1b[90m[07:45:01 DEBUG] [warden.config]\x1b[0m\n └─ Loaded config from data/config.yaml\n\n",
		},
		{
			name: "warning detail",
			write: func(writer *Writer) {
				writer.Warn("series.missing", "Search trigger failed", "HttpRequestException: Connection refused")
			},
			want: "\x1b[33m[07:45:01 WARN] [series.missing]\x1b[0m\n ├─ Search trigger failed\n └─ HttpRequestException: Connection refused\n\n",
		},
		{
			name: "error",
			write: func(writer *Writer) {
				writer.Error("warden.scheduler", "Scheduled task error", errors.New("boom"))
			},
			want: "\x1b[31m[07:45:01 ERROR] [warden.scheduler]\x1b[0m\n ├─ Scheduled task error\n └─ *errors.errorString: boom\n\n",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			var buffer bytes.Buffer
			writer := New(&buffer, Debug, time.UTC, nil)
			writer.now = fixedClock
			test.write(writer)
			if got := buffer.String(); got != test.want {
				t.Fatalf("output mismatch\ngot:  %q\nwant: %q", got, test.want)
			}
		})
	}
}

func TestWriterRespectsLevel(t *testing.T) {
	t.Parallel()
	var buffer bytes.Buffer
	writer := New(&buffer, Warning, time.UTC, nil)
	writer.Debug("ctx", "debug")
	writer.Search("Instance", "Missing Search", 1).Header()
	writer.Warn("ctx", "warning")
	if strings.Contains(buffer.String(), "debug") || strings.Contains(buffer.String(), "INFO") {
		t.Fatalf("suppressed output was written: %q", buffer.String())
	}
	if !strings.Contains(buffer.String(), "WARN") {
		t.Fatalf("warning was suppressed: %q", buffer.String())
	}
}

func TestSearchExactStatsBeforeResults(t *testing.T) {
	t.Parallel()
	var buffer bytes.Buffer
	writer := New(&buffer, Info, time.UTC, nil)
	writer.now = fixedClock
	search := writer.Search("Sonarr", "Missing Search", 3)
	search.Header()
	search.Phase("Cleaning cooldown entries")
	search.Phase("Fetching wanted episodes")
	search.Phase("Applying cooldown filters")
	search.Phase("Searching 2 items")
	search.Stats(10, 2, 8, 2, false, "")
	search.Results()
	search.Item("The Boys (2019) - S01E01 - The Name of the Game")
	search.Item("Game of Thrones (2011) - S01E01 - Winter Is Coming")
	search.Trailer()
	want := "[07:45:01 INFO] [sonarr.missing]\n" +
		" ├─ Cleaning cooldown entries\n" +
		" ├─ Fetching wanted episodes\n" +
		" ├─ Applying cooldown filters\n" +
		" ├─ Searching 2 items\n" +
		" ├─ Stats:\n" +
		" │  • Total Items:   10\n" +
		" │  • On Cooldown:   2\n" +
		" │  • Eligible:      8\n" +
		" │  • Search Limit:  3\n" +
		" │  • Result:        Searched 2\n" +
		" └─ Results:\n" +
		"    • The Boys (2019) - S01E01 - The Name of the Game\n" +
		"    • Game of Thrones (2011) - S01E01 - Winter Is Coming\n\n"
	if got := buffer.String(); got != want {
		t.Fatalf("output mismatch\ngot:  %q\nwant: %q", got, want)
	}
}

func TestSearchNoItemsUsesCompleteStats(t *testing.T) {
	t.Parallel()
	var buffer bytes.Buffer
	writer := New(&buffer, Info, time.UTC, nil)
	writer.now = fixedClock
	search := writer.Search("Radarr", "Upgrade Search", 2)
	search.Header()
	search.Stats(0, 0, 0, 0, true, "")
	want := "[07:45:01 INFO] [radarr.upgrade]\n" +
		" └─ Stats:\n" +
		"    • Total Items:   0\n" +
		"    • On Cooldown:   0\n" +
		"    • Eligible:      0\n" +
		"    • Search Limit:  2\n" +
		"    • Result:        No wanted items found\n"
	if got := buffer.String(); got != want {
		t.Fatalf("output mismatch\ngot:  %q\nwant: %q", got, want)
	}
}

func TestQueueOutputExactMixedActions(t *testing.T) {
	t.Parallel()
	var buffer bytes.Buffer
	writer := New(&buffer, Info, time.UTC, nil)
	writer.now = fixedClock
	writer.QueueResult("Sonarr", 100, 10, 2, []QueueItem{
		{Title: "First", Rule: "SAMPLE"},
		{Title: "Second", Rule: "NO_FILES_ELIGIBLE", Blocklist: true},
	}, false)
	want := "[07:45:01 INFO] [sonarr.queue]\n" +
		" ├─ Stats:\n" +
		" │  • Total Queue:   100\n" +
		" │  • Warnings:      10\n" +
		" │  • Matched:       2\n" +
		" │  • Result:        Removed 1, Blocklisted 1\n" +
		" └─ Results:\n" +
		"    • First  SAMPLE\n" +
		"    • Second  NO_FILES_ELIGIBLE\n\n"
	if got := buffer.String(); got != want {
		t.Fatalf("output mismatch\ngot:  %q\nwant: %q", got, want)
	}
}

func TestQueueOutputReportsUndisplayedMatches(t *testing.T) {
	t.Parallel()
	var buffer bytes.Buffer
	writer := New(&buffer, Info, time.UTC, nil)
	writer.now = fixedClock
	writer.QueueResult("Sonarr", 10, 4, 3, []QueueItem{{Title: "First", Rule: "SAMPLE"}}, false)
	if !strings.Contains(buffer.String(), "    +2 more\n") {
		t.Fatalf("missing undisplayed match count:\n%s", buffer.String())
	}
}

func TestRuntimeOutputPreservesUTCDisplayContract(t *testing.T) {
	t.Parallel()
	var buffer bytes.Buffer
	now := fixedClock()
	writeRuntime(&buffer, " ├─", " │  ", config.Options{DryRun: true}, time.UTC, now)
	text := buffer.String()
	for _, want := range []string{"Version           dev", "Timezone          Etc/UTC (CUT)", "UTC Offset        +00:00", "Dry Run           true"} {
		if !strings.Contains(text, want) {
			t.Errorf("missing %q:\n%s", want, text)
		}
	}
}

func fixedClock() time.Time {
	return time.Date(2026, time.January, 2, 7, 45, 1, 0, time.UTC)
}
