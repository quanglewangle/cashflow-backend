package db

import (
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// ---- Holidays ----
//
// A holiday is a planned stretch of extra card spending at a fixed amount
// per day, on top of the card's usual sundries buffer (see migration 021).
// It's materialized as one card-tagged one-off entry per payment period its
// days fall into -- a holiday spanning the statement day splits across two
// bills -- so the existing card-bill machinery (sumPurchasesForPeriod,
// GetCardPaymentBreakdown) and cash-forecast exclusion pick it up exactly
// like a sundries buffer. Each entry counts back a day at a time to zero
// on the day after its last holiday day (countBackDailyAmount).

type Holiday struct {
	ID           int64   `json:"id"`
	CreditCardID int64   `json:"credit_card_id"`
	Name         string  `json:"name"`
	StartDate    string  `json:"start_date"` // "YYYY-MM-DD", first holiday day
	EndDate      string  `json:"end_date"`   // "YYYY-MM-DD", last holiday day (inclusive)
	PerDay       float64 `json:"per_day"`
	// Total and Remaining are computed at read time: PerDay x every day,
	// and what its entries still hold after counting back as of now.
	Total     float64 `json:"total"`
	Remaining float64 `json:"remaining"`
}

const isoDate = "2006-01-02"

func GetHolidays() ([]Holiday, error) {
	rows, err := database.Query(`
		SELECT h.id, h.credit_card_id, h.name, to_char(h.start_date, 'YYYY-MM-DD'), to_char(h.end_date, 'YYYY-MM-DD'), h.per_day,
		       (h.end_date - h.start_date + 1) * h.per_day
		FROM holidays h ORDER BY h.start_date, h.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Holiday
	for rows.Next() {
		var h Holiday
		if err := rows.Scan(&h.ID, &h.CreditCardID, &h.Name, &h.StartDate, &h.EndDate, &h.PerDay, &h.Total); err != nil {
			return nil, err
		}
		out = append(out, h)
	}
	rows.Close()
	for i := range out {
		remaining, err := holidayRemaining(out[i].ID)
		if err != nil {
			return nil, err
		}
		out[i].Remaining = remaining
	}
	return out, nil
}

func holidayRemaining(id int64) (float64, error) {
	rows, err := database.Query(`
		SELECT planned_amount, actual_amount, decay_per_week, decay_start_date, decay_end_date, decay_per_day
		FROM entries WHERE holiday_id=$1`, id)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	var total float64
	for rows.Next() {
		var planned float64
		var actual, decayPerWeek, decayPerDay *float64
		var decayStart, decayEnd *time.Time
		if err := rows.Scan(&planned, &actual, &decayPerWeek, &decayStart, &decayEnd, &decayPerDay); err != nil {
			return 0, err
		}
		total += effectiveEntryAmount(planned, actual, decayPerWeek, decayStart, decayEnd, decayPerDay)
	}
	return total, rows.Err()
}

func validateHoliday(h Holiday) (start, end time.Time, err error) {
	if h.Name == "" {
		return start, end, errors.New("name required")
	}
	if h.PerDay <= 0 {
		return start, end, errors.New("per_day must be more than zero")
	}
	if start, err = time.Parse(isoDate, h.StartDate); err != nil {
		return start, end, errors.New("start_date must be YYYY-MM-DD")
	}
	if end, err = time.Parse(isoDate, h.EndDate); err != nil {
		return start, end, errors.New("end_date must be YYYY-MM-DD")
	}
	if end.Before(start) {
		return start, end, errors.New("end_date is before start_date")
	}
	return start, end, nil
}

func AddHoliday(h Holiday) (int64, error) {
	if _, _, err := validateHoliday(h); err != nil {
		return 0, err
	}
	var id int64
	err := database.QueryRow(`
		INSERT INTO holidays (credit_card_id, name, start_date, end_date, per_day)
		VALUES ($1,$2,$3,$4,$5) RETURNING id`,
		h.CreditCardID, h.Name, h.StartDate, h.EndDate, h.PerDay,
	).Scan(&id)
	if err != nil {
		return 0, err
	}
	h.ID = id
	return id, materializeHoliday(h)
}

func UpdateHoliday(id int64, h Holiday) error {
	if _, _, err := validateHoliday(h); err != nil {
		return err
	}
	res, err := database.Exec(`
		UPDATE holidays SET credit_card_id=$2, name=$3, start_date=$4, end_date=$5, per_day=$6 WHERE id=$1`,
		id, h.CreditCardID, h.Name, h.StartDate, h.EndDate, h.PerDay)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return sql.ErrNoRows
	}
	h.ID = id
	return materializeHoliday(h)
}

func DeleteHoliday(id int64) error {
	old, err := holidayPeriods(id)
	if err != nil {
		return err
	}
	if _, err := database.Exec(`DELETE FROM holidays WHERE id=$1`, id); err != nil {
		return err // its entries go with it (ON DELETE CASCADE)
	}
	return recalculateHolidayPeriods(old)
}

type cardPeriod struct {
	cardID      int64
	year, month int
}

// holidayPeriods lists the (card, payment period) pairs a holiday's entries
// currently sit in, so their bills can be recalculated after they change.
func holidayPeriods(id int64) ([]cardPeriod, error) {
	rows, err := database.Query(`
		SELECT DISTINCT credit_card_id, period_year, period_month FROM entries WHERE holiday_id=$1`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []cardPeriod
	for rows.Next() {
		var p cardPeriod
		if err := rows.Scan(&p.cardID, &p.year, &p.month); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// recalculateHolidayPeriods refreshes each affected card's stored bill from
// its earliest affected period onward -- later periods too, since
// sumUnpaidPriorCardBills nets an earlier bill out of a later checkpoint.
func recalculateHolidayPeriods(periods []cardPeriod) error {
	earliest := map[int64]cardPeriod{}
	for _, p := range periods {
		e, ok := earliest[p.cardID]
		if !ok || p.year < e.year || (p.year == e.year && p.month < e.month) {
			earliest[p.cardID] = p
		}
	}
	for cardID, p := range earliest {
		if err := recalculateCardEntry(cardID, p.year, p.month); err != nil {
			return err
		}
		if err := recalculateLaterCardEntries(cardID, p.year, p.month); err != nil {
			return err
		}
	}
	return nil
}

// materializeHoliday replaces a holiday's entries with one per payment
// period its days fall into, then refreshes the bills on both sides.
func materializeHoliday(h Holiday) error {
	start, end, err := validateHoliday(h)
	if err != nil {
		return err
	}
	card, err := getCreditCard(h.CreditCardID)
	if err != nil {
		return err
	}
	item, found, err := recurringItemForCard(card.ID)
	if err != nil {
		return err
	}
	if !found {
		return fmt.Errorf("%s has no monthly repayment item to add the holiday to", card.Name)
	}

	affected, err := holidayPeriods(h.ID)
	if err != nil {
		return err
	}
	if _, err := database.Exec(`DELETE FROM entries WHERE holiday_id=$1`, h.ID); err != nil {
		return err
	}

	for _, seg := range holidaySegments(card, start, end) {
		days := int(seg.last.Sub(seg.first).Hours()/24) + 1
		name := fmt.Sprintf("%s %s", h.Name, dayRange(seg.first, seg.last))
		if _, err := database.Exec(`
			INSERT INTO entries (category_id, period_year, period_month, name, item_type, planned_amount,
			                     credit_card_id, decay_per_day, decay_start_date, decay_end_date, holiday_id)
			VALUES ($1,$2,$3,$4,'expense',$5,$6,$7,$8,$9,$10)`,
			item.categoryID, seg.year, seg.month, name, h.PerDay*float64(days),
			card.ID, h.PerDay, seg.first, seg.last.AddDate(0, 0, 1), h.ID,
		); err != nil {
			return err
		}
		affected = append(affected, cardPeriod{card.ID, seg.year, seg.month})
	}
	return recalculateHolidayPeriods(affected)
}

type holidaySegment struct {
	year, month int // payment period
	first, last time.Time
}

// holidaySegments splits start..end (inclusive) by payment period.
// paymentPeriodFor only ever moves forward as the date does, so each
// payment period's days form one contiguous run.
func holidaySegments(card CreditCard, start, end time.Time) []holidaySegment {
	var segments []holidaySegment
	for d := start; !d.After(end); d = d.AddDate(0, 0, 1) {
		py, pm := paymentPeriodFor(card, d)
		if n := len(segments); n > 0 && segments[n-1].year == py && segments[n-1].month == pm {
			segments[n-1].last = d
			continue
		}
		segments = append(segments, holidaySegment{year: py, month: pm, first: d, last: d})
	}
	return segments
}

// dayRange formats an inclusive date range compactly: "10 Oct",
// "10–14 Oct", or "28 Oct–3 Nov".
func dayRange(first, last time.Time) string {
	switch {
	case first.Equal(last):
		return first.Format("2 Jan")
	case first.Month() == last.Month() && first.Year() == last.Year():
		return fmt.Sprintf("%d–%s", first.Day(), last.Format("2 Jan"))
	default:
		return first.Format("2 Jan") + "–" + last.Format("2 Jan")
	}
}
