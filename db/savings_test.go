package db

import (
	"math"
	"testing"
	"time"
)

func d(y, m, day int) time.Time { return time.Date(y, time.Month(m), day, 0, 0, 0, 0, time.UTC) }

func TestReplaySavings(t *testing.T) {
	rate := math.Pow(1.0375, 1.0/12) - 1
	events := []savingsEvent{
		{d(2026, 10, 30), 1500},
		{d(2026, 10, 10), 100}, // same day as interest: credited after it
		{d(2026, 11, 3), -200},
	}
	res := replaySavings(12177, d(2026, 9, 27), 3.75, 10, events, 2026, 11, d(2026, 10, 15))

	if len(res.months) != 3 {
		t.Fatalf("want Sep..Nov (3 months), got %d", len(res.months))
	}
	sep, oct, nov := res.months[0], res.months[1], res.months[2]
	if sep.Interest != 0 || sep.CarriedForward != 12177 {
		t.Errorf("Sep: interest day 10 is before opening, want no change, got %+v", sep)
	}
	octInterest := round2(12177 * rate)
	if oct.Interest != octInterest || oct.Deposits != 1600 {
		t.Errorf("Oct: want interest %.2f deposits 1600, got %+v", octInterest, oct)
	}
	if want := round2(12177 + octInterest + 1600); oct.CarriedForward != want {
		t.Errorf("Oct c/f: want %.2f got %.2f", want, oct.CarriedForward)
	}
	if want := round2(12177 + octInterest + 100); res.currentBalance != want {
		t.Errorf("current (15 Oct): want %.2f got %.2f", want, res.currentBalance)
	}
	novInterest := round2((oct.CarriedForward - 200) * rate) // withdrawal on the 3rd precedes interest on the 10th
	if nov.Withdrawals != 200 || nov.Interest != novInterest ||
		nov.CarriedForward != round2(oct.CarriedForward-200+novInterest) {
		t.Errorf("Nov: got %+v, want interest %.2f", nov, novInterest)
	}
}

func TestReplaySavingsShortMonthInterestDay(t *testing.T) {
	res := replaySavings(1000, d(2027, 1, 31), 12, 31, nil, 2027, 2, d(2027, 1, 31))
	if res.months[1].Interest == 0 {
		t.Errorf("interest day 31 should still credit in February (on the 28th)")
	}
}
