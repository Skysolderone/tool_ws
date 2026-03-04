package agent

import (
	"testing"
	"time"
)

func TestNextMidnight(t *testing.T) {
	loc := time.FixedZone("UTC+8", 8*3600)
	tests := []struct {
		name string
		now  time.Time
		want time.Time
	}{
		{
			name: "before midnight",
			now:  time.Date(2026, 3, 4, 23, 59, 59, 0, loc),
			want: time.Date(2026, 3, 5, 0, 0, 0, 0, loc),
		},
		{
			name: "at midnight should schedule next day",
			now:  time.Date(2026, 3, 4, 0, 0, 0, 0, loc),
			want: time.Date(2026, 3, 5, 0, 0, 0, 0, loc),
		},
		{
			name: "during day",
			now:  time.Date(2026, 3, 4, 10, 30, 0, 0, loc),
			want: time.Date(2026, 3, 5, 0, 0, 0, 0, loc),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := nextMidnight(tt.now)
			if !got.Equal(tt.want) {
				t.Fatalf("nextMidnight() = %s, want %s", got.Format(time.RFC3339), tt.want.Format(time.RFC3339))
			}
		})
	}
}
