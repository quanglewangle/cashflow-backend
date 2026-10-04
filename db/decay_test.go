package db

import (
	"testing"
	"time"
)

func TestCountBackAmount(t *testing.T) {
	end := time.Date(2026, 10, 14, 0, 0, 0, 0, time.UTC)
	day := func(m time.Month, d int) time.Time { return time.Date(2026, m, d, 0, 0, 0, 0, time.UTC) }
	cases := []struct {
		planned float64
		now     time.Time
		want    float64
	}{
		// Full £600/£150 buffer over a 30-day window: steps at end-21/-14/-7, zero on the statement date.
		{600, day(9, 14), 600},
		{600, day(9, 22), 600},
		{600, day(9, 23), 450},
		{600, day(9, 30), 300},
		{600, day(10, 6), 300},
		{600, day(10, 7), 150},
		{600, day(10, 13), 150},
		{600, day(10, 14), 0},
		{600, day(10, 20), 0},
		// Checkpoint-pro-rated buffer: capped at its planned amount until the count-back catches up.
		{257.14, day(10, 2), 257.14},
		{257.14, day(10, 7), 150},
		{257.14, day(10, 14), 0},
	}
	for _, c := range cases {
		if got := countBackAmount(c.planned, 150, end, c.now); got != c.want {
			t.Errorf("countBackAmount(%v, %s) = %v, want %v", c.planned, c.now.Format("2 Jan"), got, c.want)
		}
	}
}

func TestCountBackDailyAmount(t *testing.T) {
	// A £80/day holiday 10-14 Oct: end is the day after its last day.
	end := time.Date(2026, 10, 15, 0, 0, 0, 0, time.UTC)
	at := func(m time.Month, d, h int) time.Time { return time.Date(2026, m, d, h, 0, 0, 0, time.UTC) }
	cases := []struct {
		now  time.Time
		want float64
	}{
		{at(10, 1, 9), 400},
		{at(10, 10, 0), 400},
		{at(10, 10, 9), 400},
		{at(10, 11, 9), 320},
		{at(10, 14, 0), 80},
		{at(10, 14, 23), 80},
		{at(10, 15, 0), 0},
		{at(11, 1, 0), 0},
	}
	for _, c := range cases {
		if got := countBackDailyAmount(400, 80, end, c.now); got != c.want {
			t.Errorf("countBackDailyAmount(%s) = %v, want %v", c.now.Format("2 Jan 15:04"), got, c.want)
		}
	}
}

func TestDayRange(t *testing.T) {
	d := func(m time.Month, day int) time.Time { return time.Date(2026, m, day, 0, 0, 0, 0, time.UTC) }
	cases := []struct {
		first, last time.Time
		want        string
	}{
		{d(10, 10), d(10, 10), "10 Oct"},
		{d(10, 10), d(10, 14), "10–14 Oct"},
		{d(10, 28), d(11, 3), "28 Oct–3 Nov"},
	}
	for _, c := range cases {
		if got := dayRange(c.first, c.last); got != c.want {
			t.Errorf("dayRange = %q, want %q", got, c.want)
		}
	}
}
