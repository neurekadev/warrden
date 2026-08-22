package output

import (
	"bytes"
	"fmt"
	"strings"
)

// QueueItem is one displayed queue-cleanup result.
type QueueItem struct {
	Title     string
	Rule      string
	Blocklist bool
}

// QueueResult writes one queue job's stats and results.
func (w *Writer) QueueResult(instance string, total, warnings, matched int, items []QueueItem, dryRun bool) {
	if w.level > Info {
		return
	}
	var buffer bytes.Buffer
	fmt.Fprintf(&buffer, "[%s INFO] [%s.queue]\n", w.timestamp(), strings.ToLower(instance))
	if matched == 0 {
		fmt.Fprintf(&buffer, " └─ Stats:\n    • Total Queue:   %d\n    • Result:        No warning queue items detected\n", total)
	} else {
		removed, blocked := 0, 0
		for _, item := range items {
			if item.Blocklist {
				blocked++
			} else {
				removed++
			}
		}
		fmt.Fprintf(&buffer, " ├─ Stats:\n │  • Total Queue:   %d\n │  • Warnings:      %d\n │  • Matched:       %d\n │  • Result:        %s\n └─ Results:\n", total, warnings, matched, queueResultText(dryRun, removed, blocked))
		for _, item := range items {
			fmt.Fprintf(&buffer, "    • %s  %s\n", item.Title, item.Rule)
		}
		if len(items) < matched {
			fmt.Fprintf(&buffer, "    +%d more\n", matched-len(items))
		}
	}
	buffer.WriteByte('\n')
	w.write(buffer.Bytes())
}

func queueResultText(dryRun bool, removed, blocked int) string {
	switch {
	case dryRun && removed > 0 && blocked > 0:
		return fmt.Sprintf("Would remove %d, Would blocklist %d", removed, blocked)
	case !dryRun && removed > 0 && blocked > 0:
		return fmt.Sprintf("Removed %d, Blocklisted %d", removed, blocked)
	case dryRun && removed > 0:
		return fmt.Sprintf("Would remove %d", removed)
	case !dryRun && removed > 0:
		return fmt.Sprintf("Removed %d", removed)
	case dryRun && blocked > 0:
		return fmt.Sprintf("Would blocklist %d", blocked)
	case !dryRun && blocked > 0:
		return fmt.Sprintf("Blocklisted %d", blocked)
	default:
		return ""
	}
}

// ClearCount is the number of cooldown entries cleared for an instance.
type ClearCount struct {
	Instance string
	Count    int
}

// ClearResult writes a clear-missing or clear-upgrades result.
func (w *Writer) ClearResult(label, category string, counts []ClearCount) {
	if w.level > Info {
		return
	}
	var buffer bytes.Buffer
	fmt.Fprintf(&buffer, "[%s INFO] [%s]\n ├─ Type:       %s\n", w.timestamp(), label, category)
	if len(counts) == 1 {
		fmt.Fprintf(&buffer, " └─ Cleared:    %d %s\n", counts[0].Count, entries(counts[0].Count))
	} else {
		total := 0
		for _, count := range counts {
			total += count.Count
			fmt.Fprintf(&buffer, " ├─ %s:     %d %s\n", count.Instance, count.Count, entries(count.Count))
		}
		fmt.Fprintf(&buffer, " └─ Cleared:    %d %s\n", total, entries(total))
	}
	buffer.WriteByte('\n')
	w.write(buffer.Bytes())
}

func entries(count int) string {
	if count == 1 {
		return "entry"
	}
	return "entries"
}
