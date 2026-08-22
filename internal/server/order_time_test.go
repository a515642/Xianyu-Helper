package server

import (
	"testing"
	"time"
)

func TestNormalizeOrderTimestamp(t *testing.T) {
	loc := time.FixedZone("UTC+8", 8*60*60)
	cases := []struct{ name, raw, want string }{
		{"legacy postgres timestamp", "2026-08-22T21:41:17.905562Z", "2026-08-22T21:41:17.905562+08:00"},
		{"bare timestamp", "2026-08-22 21:41:17.905562", "2026-08-22T21:41:17.905562+08:00"},
		{"explicit utc", "2026-08-22T13:41:17.905562+00:00", "2026-08-22T13:41:17.905562Z"},
		{"explicit offset", "2026-08-22T21:41:17.905562+08:00", "2026-08-22T21:41:17.905562+08:00"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := normalizeOrderTimestamp(tc.raw, loc); got != tc.want {
				t.Fatalf("got %q want %q", got, tc.want)
			}
		})
	}
}
