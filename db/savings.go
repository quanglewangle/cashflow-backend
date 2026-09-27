package db

import (
	"math"
	"sort"
	"time"
)

// ---- Savings accounts ----
//
// A savings account (e.g. Marcus) sits alongside cash and the credit cards.
// Transfers are ordinary entries tagged with savings_account_id (see
// migration 019): item_type savings moves money in, income moves it out.
// Cash-side forecasting is untouched -- those entries already count there.
// The account's own balance is walked forward from its opening figure,
// with interest credited monthly on interest_day.

type SavingsAccount struct {
	ID             int64   `json:"id"`
	Name           string  `json:"name"`
	OpeningBalance float64 `json:"opening_balance"`
	OpeningDate    string  `json:"opening_date"`  // "YYYY-MM-DD"; balance as at the end of this day
	InterestRate   float64 `json:"interest_rate"` // AER, percent
	InterestDay    int     `json:"interest_day"`
	// CurrentBalance is computed at read time: opening balance plus every
	// transfer and interest credit dated after opening_date up to today.
	CurrentBalance float64 `json:"current_balance"`
}

// SavingsMonth summarises one calendar month of a savings account.
type SavingsMonth struct {
	PeriodYear     int     `json:"period_year"`
	PeriodMonth    int     `json:"period_month"`
	BroughtForward float64 `json:"brought_forward"`
	Deposits       float64 `json:"deposits"`
	Withdrawals    float64 `json:"withdrawals"`
	Interest       float64 `json:"interest"`
	CarriedForward float64 `json:"carried_forward"`
}

func GetSavingsAccounts() ([]SavingsAccount, error) {
	rows, err := database.Query(`
		SELECT id, name, opening_balance, to_char(opening_date, 'YYYY-MM-DD'), interest_rate, interest_day
		FROM savings_accounts ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SavingsAccount
	for rows.Next() {
		var a SavingsAccount
		if err := rows.Scan(&a.ID, &a.Name, &a.OpeningBalance, &a.OpeningDate, &a.InterestRate, &a.InterestDay); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	rows.Close()
	now := time.Now()
	for i := range out {
		months, err := savingsWalk(out[i], now.Year(), int(now.Month()))
		if err != nil {
			return nil, err
		}
		out[i].CurrentBalance = months.currentBalance
	}
	return out, nil
}

func getSavingsAccount(id int64) (SavingsAccount, error) {
	var a SavingsAccount
	err := database.QueryRow(`
		SELECT id, name, opening_balance, to_char(opening_date, 'YYYY-MM-DD'), interest_rate, interest_day
		FROM savings_accounts WHERE id=$1`, id,
	).Scan(&a.ID, &a.Name, &a.OpeningBalance, &a.OpeningDate, &a.InterestRate, &a.InterestDay)
	return a, err
}

func AddSavingsAccount(a SavingsAccount) (int64, error) {
	if a.InterestDay == 0 {
		a.InterestDay = 1
	}
	var id int64
	err := database.QueryRow(`
		INSERT INTO savings_accounts (name, opening_balance, opening_date, interest_rate, interest_day)
		VALUES ($1, $2, $3, $4, $5) RETURNING id`,
		a.Name, a.OpeningBalance, a.OpeningDate, a.InterestRate, a.InterestDay,
	).Scan(&id)
	return id, err
}

func UpdateSavingsAccount(id int64, a SavingsAccount) error {
	if a.InterestDay == 0 {
		a.InterestDay = 1
	}
	_, err := database.Exec(`
		UPDATE savings_accounts SET name=$2, opening_balance=$3, opening_date=$4, interest_rate=$5, interest_day=$6
		WHERE id=$1`,
		id, a.Name, a.OpeningBalance, a.OpeningDate, a.InterestRate, a.InterestDay)
	return err
}

// SavingsProjection returns month-by-month figures for a savings account from
// its opening month through `months` months starting at the current month.
func SavingsProjection(id int64, months int) ([]SavingsMonth, error) {
	a, err := getSavingsAccount(id)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	end := time.Date(now.Year(), now.Month()+time.Month(months-1), 1, 0, 0, 0, 0, time.UTC)
	w, err := savingsWalk(a, end.Year(), int(end.Month()))
	if err != nil {
		return nil, err
	}
	return w.months, nil
}

type savingsEvent struct {
	date   time.Time
	amount float64 // + into the account, - out of it
}

type savingsWalkResult struct {
	months         []SavingsMonth
	currentBalance float64
}

// savingsWalk replays an account from opening_date to the end of
// (endYear, endMonth): each tagged entry lands on its incurred_date if it's
// been paid, otherwise its due day (1st if none); interest is credited on
// interest_day (clamped to short months) at the monthly equivalent of the
// AER, on the balance before that day's transfers.
func savingsWalk(a SavingsAccount, endYear, endMonth int) (savingsWalkResult, error) {
	var res savingsWalkResult
	opening, err := time.Parse("2006-01-02", a.OpeningDate)
	if err != nil {
		return res, err
	}
	rows, err := database.Query(`
		SELECT item_type, planned_amount, actual_amount, decay_per_week, decay_start_date,
		       status, incurred_date, period_year, period_month, COALESCE(due_day, 1)
		FROM entries
		WHERE savings_account_id=$1 AND item_type IN ('savings', 'income')`, a.ID)
	if err != nil {
		return res, err
	}
	defer rows.Close()
	var events []savingsEvent
	for rows.Next() {
		var itemType, status string
		var planned float64
		var actual, decay *float64
		var decayStart, incurred *time.Time
		var year, month, day int
		if err := rows.Scan(&itemType, &planned, &actual, &decay, &decayStart,
			&status, &incurred, &year, &month, &day); err != nil {
			return res, err
		}
		date := clampedDate(year, month, day)
		if status == "incurred" && incurred != nil {
			date = time.Date(incurred.Year(), incurred.Month(), incurred.Day(), 0, 0, 0, 0, time.UTC)
		}
		if !date.After(opening) {
			continue // already reflected in the opening balance
		}
		amount := effectiveEntryAmount(planned, actual, decay, decayStart)
		if itemType == "income" {
			amount = -amount
		}
		events = append(events, savingsEvent{date, amount})
	}
	if err := rows.Err(); err != nil {
		return res, err
	}
	now := time.Now()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	return replaySavings(a.OpeningBalance, opening, a.InterestRate, a.InterestDay, events, endYear, endMonth, today), nil
}

// replaySavings is savingsWalk's arithmetic, separated from the DB reads.
// events needn't be sorted, but must all fall after opening.
func replaySavings(openingBalance float64, opening time.Time, ratePercent float64, interestDay int,
	events []savingsEvent, endYear, endMonth int, today time.Time) savingsWalkResult {
	var res savingsWalkResult
	sort.SliceStable(events, func(i, j int) bool { return events[i].date.Before(events[j].date) })
	monthlyRate := math.Pow(1+ratePercent/100, 1.0/12) - 1

	balance := openingBalance
	res.currentBalance = balance
	next := 0
	for y, m := opening.Year(), int(opening.Month()); y < endYear || (y == endYear && m <= endMonth); {
		sm := SavingsMonth{PeriodYear: y, PeriodMonth: m, BroughtForward: round2(balance)}
		monthEnd := clampedDate(y, m, 31)
		interestDate := clampedDate(y, m, interestDay)
		interestDone := !interestDate.After(opening)
		apply := func(amount float64, date time.Time) {
			balance += amount
			if !date.After(today) {
				res.currentBalance = balance
			}
		}
		for {
			var ev *savingsEvent
			if next < len(events) && !events[next].date.After(monthEnd) {
				ev = &events[next]
			}
			if !interestDone && (ev == nil || !ev.date.Before(interestDate)) {
				interest := round2(balance * monthlyRate)
				sm.Interest += interest
				apply(interest, interestDate)
				interestDone = true
				continue
			}
			if ev == nil {
				break
			}
			if ev.amount >= 0 {
				sm.Deposits += ev.amount
			} else {
				sm.Withdrawals -= ev.amount
			}
			apply(ev.amount, ev.date)
			next++
		}
		sm.Deposits = round2(sm.Deposits)
		sm.Withdrawals = round2(sm.Withdrawals)
		sm.Interest = round2(sm.Interest)
		sm.CarriedForward = round2(balance)
		res.months = append(res.months, sm)
		m++
		if m > 12 {
			m = 1
			y++
		}
	}
	res.currentBalance = round2(res.currentBalance)
	return res
}

// clampedDate is (year, month, day) with day pulled back to the month's last
// day for short months (e.g. day 31 in September -> the 30th).
func clampedDate(year, month, day int) time.Time {
	last := time.Date(year, time.Month(month)+1, 0, 0, 0, 0, 0, time.UTC).Day()
	if day > last {
		day = last
	}
	if day < 1 {
		day = 1
	}
	return time.Date(year, time.Month(month), day, 0, 0, 0, 0, time.UTC)
}

func round2(v float64) float64 {
	return math.Round(v*100) / 100
}
