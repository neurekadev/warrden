package output

import (
	"bytes"
	"fmt"
	"strings"
	"time"

	"code.neureka.dev/warrden/warrden/internal/config"
)

const (
	boxWidth = 58
	labelPad = 18
)

// Banner writes the complete startup banner atomically.
func (w *Writer) Banner(cfg *config.Config, opts config.Options, disabled func(string) string, warnings []string) {
	var buffer bytes.Buffer
	bar := strings.Repeat("━", boxWidth)
	fmt.Fprintf(&buffer, "┏%s┓\n┃%s┃\n┗%s┛\n\n", bar, center("wArrden", boxWidth), bar)
	now := w.now().In(w.location)
	ts := now.Format("15:04:05")
	fmt.Fprintf(&buffer, "[%s INFO] [system.startup]\n", ts)

	enabled := make([]config.Instance, 0)
	for _, instance := range cfg.Instances {
		if instance.Enabled {
			enabled = append(enabled, instance)
		}
	}
	total, index := len(enabled)+2, 0
	writeSection := func(content func(root, child string)) {
		last := index == total-1
		root, child := " ├─", " │  "
		if last {
			root, child = " └─", "    "
		}
		content(root, child)
		if !last {
			buffer.WriteString(" │\n")
		}
		index++
	}

	writeSection(func(root, child string) { writeRuntime(&buffer, root, child, opts, w.location, now) })
	for _, instance := range enabled {
		instance := instance
		writeSection(func(root, child string) { writeInstance(&buffer, root, child, instance, disabled(instance.Key())) })
	}
	writeSection(func(root, child string) { writeRules(&buffer, root, child, cfg.QueueRules) })

	if len(warnings) > 0 {
		fmt.Fprintf(&buffer, "\n\x1b[33m[%s WARN] [warden.config]\x1b[0m\n", ts)
		writeMessages(&buffer, warnings)
	}
	fmt.Fprintf(&buffer, "\n[%s INFO] [system.ready] wArrden initialized\n\n", ts)
	w.write(buffer.Bytes())
}

func writeRuntime(buffer *bytes.Buffer, root, child string, opts config.Options, location *time.Location, now time.Time) {
	fmt.Fprintf(buffer, "%s Runtime\n", root)
	abbr, offset := now.Zone()
	if abbr == "UTC" {
		abbr = "CUT"
	}
	version := opts.AppVersion
	if !opts.AppVersionSet {
		version = "dev"
	}
	tzID := opts.Timezone
	if strings.TrimSpace(tzID) == "" {
		tzID = location.String()
		if tzID == "UTC" {
			tzID = "Etc/UTC"
		}
	}
	sign := "+"
	if offset < 0 {
		sign, offset = "-", -offset
	}
	fmt.Fprintf(buffer, "%s ├─ %-*s%s\n", child, labelPad, "Version", version)
	fmt.Fprintf(buffer, "%s ├─ %-*s%s (%s)\n", child, labelPad, "Timezone", tzID, abbr)
	fmt.Fprintf(buffer, "%s ├─ %-*s%s\n", child, labelPad, "Local Time", now.Format("2006-01-02 15:04:05"))
	fmt.Fprintf(buffer, "%s ├─ %-*s%s%02d:%02d\n", child, labelPad, "UTC Offset", sign, offset/3600, offset%3600/60)
	fmt.Fprintf(buffer, "%s └─ %-*s%t\n", child, labelPad, "Dry Run", opts.DryRun)
}

func writeInstance(buffer *bytes.Buffer, root, child string, instance config.Instance, reason string) {
	fmt.Fprintf(buffer, "%s %s (%s)\n", root, instance.Name, instance.Key())
	lines := []string{fmt.Sprintf("%-*s%s", labelPad, "URL", instance.URLText)}
	appendJob := func(label string, job *config.Job) {
		if job == nil {
			return
		}
		value := "(disabled)"
		if job.Enabled {
			value = job.Cron
			if job.SearchType != "" {
				value += "  (" + string(job.SearchType) + ")"
			}
		}
		lines = append(lines, fmt.Sprintf("%-*s%s", labelPad, label, value))
	}
	appendJob("Queue Cleanup", instance.QueueCleanup)
	appendJob("Missing Search", instance.MissingSearch)
	appendJob("Upgrade Search", instance.UpgradeSearch)
	if reason != "" {
		lines = append(lines, fmt.Sprintf("%-*s\x1b[33mDISABLED — %s\x1b[0m", labelPad, "Status", reason))
		lines = append(lines, fmt.Sprintf("%-*sFix the API key and restart wArrden", labelPad, "Action"))
	}
	for index, line := range lines {
		branch := " ├─"
		if index == len(lines)-1 {
			branch = " └─"
		}
		fmt.Fprintf(buffer, "%s%s %s\n", child, branch, line)
	}
}

func writeRules(buffer *bytes.Buffer, root, child string, rules config.QueueRules) {
	fmt.Fprintf(buffer, "%s Queue Cleanup Rules\n", root)
	fmt.Fprintf(buffer, "%s ├─ %-*s%d matcher(s)\n", child, labelPad, "sonarr", len(rules.Sonarr))
	fmt.Fprintf(buffer, "%s ├─ %-*s%d matcher(s)\n", child, labelPad, "radarr", len(rules.Radarr))
	fmt.Fprintf(buffer, "%s ├─ %-*s%d matcher(s)\n", child, labelPad, "lidarr", len(rules.Lidarr))
	fmt.Fprintf(buffer, "%s └─ %-*s%d matcher(s)\n", child, labelPad, "whisparr", len(rules.Whisparr))
}

func writeMessages(buffer *bytes.Buffer, messages []string) {
	for index, message := range messages {
		branch := " ├─"
		if index == len(messages)-1 {
			branch = " └─"
		}
		fmt.Fprintf(buffer, "%s %s\n", branch, message)
	}
}

func center(text string, width int) string {
	padding := width - len(text)
	if padding <= 0 {
		return text
	}
	left := padding / 2
	return strings.Repeat(" ", left) + text + strings.Repeat(" ", width-left-len(text))
}
