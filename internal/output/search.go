package output

import (
	"bytes"
	"fmt"
	"strings"
)

// Search is the progressive output for one search job.
type Search struct {
	writer     *Writer
	instance   string
	job        string
	maxResults int
	enabled    bool
}

// Search starts a search result writer.
func (w *Writer) Search(instance, job string, maxResults int) *Search {
	return &Search{writer: w, instance: instance, job: job, maxResults: maxResults, enabled: w.level <= Info}
}

// Header writes the search header.
func (s *Search) Header() {
	if !s.enabled {
		return
	}
	label := strings.ToLower(s.job)
	switch label {
	case "missing search":
		label = "missing"
	case "upgrade search":
		label = "upgrade"
	}
	s.writer.write([]byte(fmt.Sprintf("[%s INFO] [%s.%s]\n", s.writer.timestamp(), strings.ToLower(s.instance), label)))
}

// Phase writes one intermediate phase.
func (s *Search) Phase(phase string) {
	if !s.enabled {
		return
	}
	s.writer.write([]byte(fmt.Sprintf(" ├─ %s\n", phase)))
}

// Stats writes the complete search statistics.
func (s *Search) Stats(total, cooldown, eligible, searched int, last bool, override string) {
	if !s.enabled {
		return
	}
	prefix, child := " ├─", " │ "
	if last {
		prefix, child = " └─", "   "
	}
	result := override
	if result == "" {
		switch {
		case total == 0:
			result = "No wanted items found"
		case searched == 0:
			result = "No search performed"
		default:
			result = fmt.Sprintf("Searched %d", searched)
		}
	}
	var buffer bytes.Buffer
	fmt.Fprintf(&buffer, "%s Stats:\n%s • Total Items:   %d\n%s • On Cooldown:   %d\n%s • Eligible:      %d\n%s • Search Limit:  %d\n%s • Result:        %s\n", prefix, child, total, child, cooldown, child, eligible, child, s.maxResults, child, result)
	s.writer.write(buffer.Bytes())
}

// Results starts the result item section.
func (s *Search) Results() {
	if s.enabled {
		s.writer.write([]byte(" └─ Results:\n"))
	}
}

// Item writes one searched item.
func (s *Search) Item(title string) {
	if s.enabled {
		s.writer.write([]byte(fmt.Sprintf("    • %s\n", title)))
	}
}

// Trailer terminates search output.
func (s *Search) Trailer() {
	if s.enabled {
		s.writer.write([]byte("\n"))
	}
}
