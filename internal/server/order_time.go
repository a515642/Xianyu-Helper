package server

import (
	"strings"
	"time"
)

const (
	orderTimeLayout      = "2006-01-02 15:04:05"
	orderTimeLayoutNanos = "2006-01-02 15:04:05.999999999"
)

// normalizeOrderTimestamp converts legacy database wall-clock values into an
// explicit RFC3339 timestamp. The orders table historically used timestamp
// without time zone, so values ending in Z from that column are treated as the
// configured application wall clock rather than as UTC instants.
func normalizeOrderTimestamp(raw string, loc *time.Location) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if loc == nil {
		loc = time.Local
	}
	// The PostgreSQL driver in the deployed schema can return a timestamp
	// without time zone as an ISO-looking value ending in Z. Treat that exact
	// legacy form as a wall clock. Explicit non-Z offsets remain authoritative.
	if strings.HasSuffix(raw, "Z") && strings.Contains(raw, "T") {
		wallClock := strings.TrimSuffix(raw, "Z")
		for _, layout := range []string{"2006-01-02T15:04:05.999999999", "2006-01-02T15:04:05"} {
			if parsed, err := time.ParseInLocation(layout, wallClock, loc); err == nil {
				return parsed.Format(time.RFC3339Nano)
			}
		}
	}
	for _, layout := range []string{orderTimeLayoutNanos, orderTimeLayout} {
		if parsed, err := time.ParseInLocation(layout, raw, loc); err == nil {
			return parsed.Format(time.RFC3339Nano)
		}
	}
	if parsed, err := time.Parse(time.RFC3339Nano, raw); err == nil {
		return parsed.Format(time.RFC3339Nano)
	}
	if parsed, err := time.Parse(time.RFC3339, raw); err == nil {
		return parsed.Format(time.RFC3339Nano)
	}
	return raw
}
