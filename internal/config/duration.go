package config

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var durationPattern = regexp.MustCompile(`(?i)(\d+)\s*([dhms])`)

func parseDuration(input string) (time.Duration, error) {
	if strings.TrimSpace(input) == "" {
		return 0, fmt.Errorf("duration cannot be empty")
	}

	var total time.Duration
	matches := durationPattern.FindAllStringSubmatch(input, -1)
	if len(matches) == 0 {
		return 0, fmt.Errorf("invalid duration format: '%s'. Expected e.g. '30d', '12h', '1h30m', '90s'", input)
	}

	for _, match := range matches {
		value, err := strconv.ParseInt(match[1], 10, 32)
		if err != nil {
			return 0, fmt.Errorf("value was either too large or too small for an Int32")
		}

		var unit time.Duration
		switch strings.ToLower(match[2]) {
		case "d":
			unit = 24 * time.Hour
		case "h":
			unit = time.Hour
		case "m":
			unit = time.Minute
		case "s":
			unit = time.Second
		default:
			return 0, fmt.Errorf("unknown duration unit: '%s'", match[2])
		}

		if value > int64((1<<63-1)/unit) {
			return 0, fmt.Errorf("timeSpan overflowed because the duration is too long")
		}
		component := time.Duration(value) * unit
		if component > time.Duration(1<<63-1)-total {
			return 0, fmt.Errorf("timeSpan overflowed because the duration is too long")
		}
		total += component
	}

	if total == 0 {
		return 0, fmt.Errorf("duration '%s' resolves to zero", input)
	}
	return total, nil
}
