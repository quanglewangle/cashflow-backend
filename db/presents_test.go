package db

import (
	"math"
	"testing"
	"time"
)

func TestPresentHolds(t *testing.T) {
	visa := CreditCard{ID: 1, StatementDay: 14, PaymentDueMonthOffset: 1}
	cards := map[int64]CreditCard{1: visa}
	d := func(y int, m time.Month, day int) time.Time { return time.Date(y, m, day, 0, 0, 0, 0, time.UTC) }
	xmas := PresentOccasion{ID: 1, CreditCardID: 1, Name: "Christmas", Month: 12, Day: 25, Allowance: 500}
	bdays := PresentOccasion{ID: 2, CreditCardID: 1, Name: "Birthdays", Month: 3, Day: 10, Allowance: 300}
	occasions := []PresentOccasion{xmas, bdays}

	sum := func(holds []presentHold, id int64, from, to time.Time) float64 {
		var total float64
		for _, h := range holds {
			if h.occasionID == id && !h.first.Before(from) && !h.last.After(to) {
				total += h.amount
			}
		}
		return math.Round(total*100) / 100
	}

	// 1 Nov, nothing spent: Christmas's £500 is spread over 1 Nov..25 Dec
	// (55 days) across the Dec, Jan and Feb bills; birthdays' £300 over
	// 26 Dec..10 Mar, after Christmas.
	now := d(2026, 11, 1)
	holds := presentHolds(occasions, cards, nil, now)
	if got := sum(holds, 1, d(2026, 11, 1), d(2026, 12, 25)); got != 500 {
		t.Errorf("Christmas 2026 total = %v, want 500", got)
	}
	var dec float64
	for _, h := range holds {
		if h.occasionID == 1 && h.year == 2026 && h.month == 12 {
			dec += h.amount
		}
	}
	// 1..14 Nov is 14 of 55 days.
	if want := math.Floor(50000*14/55) / 100; math.Abs(dec-want) > 0.001 {
		t.Errorf("Christmas on the Dec bill = %v, want %v", dec, want)
	}
	if got := sum(holds, 2, d(2026, 12, 26), d(2027, 3, 10)); got != 300 {
		t.Errorf("Birthdays 2027 total = %v, want 300", got)
	}
	for _, h := range holds {
		if h.occasionID == 2 && h.first.Before(d(2026, 12, 26)) {
			t.Errorf("birthday allowance held before Christmas: %+v", h)
		}
	}
	// The year after is held too, so a 12-month forecast always carries both.
	if got := sum(holds, 1, d(2027, 3, 11), d(2027, 12, 25)); got != 500 {
		t.Errorf("Christmas 2027 total = %v, want 500", got)
	}

	// Spending: £120 on 5 Nov counts toward Christmas, £40 on 27 Dec toward
	// the birthdays, and £60 on 25 Dec itself still toward Christmas.
	purchases := []presentPurchase{
		{120, d(2026, 11, 5)}, {40, d(2026, 12, 27)}, {60, d(2026, 12, 25)},
	}
	holds = presentHolds(occasions, cards, purchases, now)
	if got := sum(holds, 1, d(2026, 11, 1), d(2026, 12, 25)); got != 320 {
		t.Errorf("Christmas after spending = %v, want 320", got)
	}
	if got := sum(holds, 2, d(2026, 12, 26), d(2027, 3, 10)); got != 260 {
		t.Errorf("Birthdays after spending = %v, want 260", got)
	}

	// Overspent: nothing held back.
	holds = presentHolds(occasions, cards, []presentPurchase{{650, d(2026, 12, 1)}}, now)
	if got := sum(holds, 1, d(2026, 11, 1), d(2026, 12, 25)); got != 0 {
		t.Errorf("overspent Christmas holds %v, want 0", got)
	}

	// On the day itself, only that day is left to hold it.
	holds = presentHolds(occasions, cards, nil, d(2026, 12, 25))
	if got := sum(holds, 1, d(2026, 12, 25), d(2026, 12, 25)); got != 500 {
		t.Errorf("Christmas Day = %v, want 500", got)
	}
}

func TestOccasionDateClampsLeapDay(t *testing.T) {
	if got := occasionDate(2, 29, 2027); got.Day() != 28 {
		t.Errorf("29 Feb 2027 = %v, want the 28th", got)
	}
}
