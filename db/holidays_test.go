package db

import (
	"testing"
	"time"
)

func TestHolidaySegments(t *testing.T) {
	visa := CreditCard{StatementDay: 14, PaymentDueMonthOffset: 1}
	d := func(m time.Month, day int) time.Time { return time.Date(2026, m, day, 0, 0, 0, 0, time.UTC) }

	// Wholly before the statement day: one segment, paid in November.
	got := holidaySegments(visa, d(10, 3), d(10, 10))
	if len(got) != 1 || got[0].year != 2026 || got[0].month != 11 || !got[0].first.Equal(d(10, 3)) || !got[0].last.Equal(d(10, 10)) {
		t.Errorf("before statement: got %+v", got)
	}

	// Spanning it: the 14th is still on the November bill, the 15th onward on December's.
	got = holidaySegments(visa, d(10, 10), d(10, 20))
	if len(got) != 2 {
		t.Fatalf("spanning statement: got %d segments, want 2: %+v", len(got), got)
	}
	if got[0].month != 11 || !got[0].first.Equal(d(10, 10)) || !got[0].last.Equal(d(10, 14)) {
		t.Errorf("first segment = %+v", got[0])
	}
	if got[1].month != 12 || !got[1].first.Equal(d(10, 15)) || !got[1].last.Equal(d(10, 20)) {
		t.Errorf("second segment = %+v", got[1])
	}

	// A single day.
	got = holidaySegments(visa, d(10, 14), d(10, 14))
	if len(got) != 1 || got[0].month != 11 {
		t.Errorf("single day: got %+v", got)
	}
}
