package handlers

import (
	"testing"
	"time"
)

func TestRequestedMonth(t *testing.T) {
	fallback := time.Date(2026, time.September, 3, 12, 0, 0, 0, time.UTC)
	if got := requestedMonth("2026-02", fallback); !got.Equal(time.Date(2026, time.February, 1, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("requestedMonth returned %s", got)
	}
	if got := requestedMonth("2026-2", fallback); !got.Equal(time.Date(2026, time.September, 1, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("invalid month should use fallback, got %s", got)
	}
}

func TestBuildCalendarDaysAlwaysReturnsSixWeeks(t *testing.T) {
	month := time.Date(2026, time.February, 1, 0, 0, 0, 0, time.UTC)
	days := buildCalendarDays(month, nil, month)
	if len(days) != 42 || days[0].Date != "2026-02-01" || days[41].Date != "2026-03-14" {
		t.Fatalf("unexpected calendar grid: %#v ... %#v", days[0], days[len(days)-1])
	}
	if !days[0].IsToday || !days[0].IsCurrentMonth || days[28].IsCurrentMonth {
		t.Fatal("calendar day flags were not set correctly")
	}
}
