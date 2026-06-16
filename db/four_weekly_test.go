package db

import (
	"testing"
	"time"
)

func date(s string) time.Time {
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		panic(err)
	}
	return t
}

func TestFourWeeklyOccurrences(t *testing.T) {
	anchor := date("2026-01-05") // a Monday, say

	cases := []struct {
		year, month int
		want        int
	}{
		{2026, 1, 1},  // the anchor's own month
		{2025, 12, 0}, // before the anchor starts at all
		{2026, 2, 1},  // anchor+28 = 2026-02-02
	}

	// Find the actual month that gets 2 occurrences within the next 14
	// cycles (52 weeks / 4 = 13 cycles/year, so within ~1 year one month
	// must absorb the extra occurrence) and assert the total across a full
	// year matches 13, rather than hardcoding which specific month it is.
	total := 0
	y, m := 2026, 1
	twoOccurrenceMonths := 0
	for i := 0; i < 12; i++ {
		n := fourWeeklyOccurrences(anchor, y, m)
		total += n
		if n == 2 {
			twoOccurrenceMonths++
		}
		if n < 0 || n > 2 {
			t.Errorf("%d-%02d: got %d occurrences, expected 0-2", y, m, n)
		}
		m++
		if m > 12 {
			m = 1
			y++
		}
	}
	if total != 13 {
		t.Errorf("expected 13 occurrences across 2026, got %d", total)
	}
	if twoOccurrenceMonths != 1 {
		t.Errorf("expected exactly 1 month with 2 occurrences in 2026, got %d", twoOccurrenceMonths)
	}

	for _, c := range cases {
		got := fourWeeklyOccurrences(anchor, c.year, c.month)
		if got != c.want {
			t.Errorf("%d-%02d: got %d, want %d", c.year, c.month, got, c.want)
		}
	}
}

func TestFourWeeklyOccurrencesNoInfiniteLoop(t *testing.T) {
	// Anchor decades away in either direction must still terminate quickly
	// and return 0 -- this is the exact class of bug that caused the
	// production incident (unbounded forward generation).
	anchor := date("2100-01-01")
	if got := fourWeeklyOccurrences(anchor, 2026, 6); got != 0 {
		t.Errorf("anchor far in the future: got %d, want 0", got)
	}

	anchor2 := date("1990-01-01")
	if got := fourWeeklyOccurrences(anchor2, 2026, 6); got < 0 || got > 2 {
		t.Errorf("anchor far in the past: got %d, want 0-2", got)
	}
}
