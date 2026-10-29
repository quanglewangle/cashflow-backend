package db

import (
	"testing"
	"time"
)

func TestRefundPriorPeriod(t *testing.T) {
	// Visacard: statement on the 14th, paid on the 9th of the following month.
	card := CreditCard{StatementDay: 14, PaymentDueDay: 9, PaymentDueMonthOffset: 1}
	day := func(y int, m time.Month, d int) time.Time { return time.Date(y, m, d, 0, 0, 0, 0, time.UTC) }
	cases := []struct {
		date         time.Time
		wantY, wantM int
		wantOK       bool
	}{
		// After the 14 Oct statement, before its 9 Nov payment: comes off November's bill.
		{day(2026, 10, 15), 2026, 11, true},
		{day(2026, 11, 2), 2026, 11, true},
		{day(2026, 11, 9), 2026, 11, true},
		// After the payment date: too late for November.
		{day(2026, 11, 10), 2026, 11, false},
		// Before the statement closes it's already on the upcoming bill; the prior one was paid 9 Oct.
		{day(2026, 10, 14), 2026, 10, false},
		// Year boundary.
		{day(2026, 12, 20), 2027, 1, true},
	}
	for _, c := range cases {
		y, m, ok := refundPriorPeriod(card, c.date)
		if y != c.wantY || m != c.wantM || ok != c.wantOK {
			t.Errorf("refundPriorPeriod(%s) = %d-%02d %v, want %d-%02d %v",
				c.date.Format("2 Jan 2006"), y, m, ok, c.wantY, c.wantM, c.wantOK)
		}
	}
}
