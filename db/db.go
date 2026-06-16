package db

import (
	"database/sql"
	"fmt"
	"os"

	_ "github.com/lib/pq"
)

var database *sql.DB
var dbOpen bool

// OpenDatabase connects to the "cashflow" Postgres database. Same approach
// as qrzlook: peer auth over the local unix socket, no password needed.
// Override with CASHFLOW_DB_DSN for testing against a different instance.
func OpenDatabase() {
	if dbOpen {
		return
	}
	dsn := os.Getenv("CASHFLOW_DB_DSN")
	if dsn == "" {
		dsn = "host=/var/run/postgresql dbname=cashflow user=peter sslmode=disable"
	}
	var err error
	database, err = sql.Open("postgres", dsn)
	if err != nil {
		fmt.Println("db open error:", err)
		return
	}
	if err := database.Ping(); err != nil {
		fmt.Println("db ping error:", err)
		return
	}
	dbOpen = true
}

// ---- Models ----

type Category struct {
	ID        int64  `json:"id"`
	Name      string `json:"name"`
	ItemType  string `json:"item_type"` // income | expense | savings
	SortOrder int    `json:"sort_order"`
}

type CreditCard struct {
	ID            int64  `json:"id"`
	Name          string `json:"name"`
	StatementDay  int    `json:"statement_day"`
	PaymentDueDay int    `json:"payment_due_day"`
}

type RecurringItem struct {
	ID            int64    `json:"id"`
	CategoryID    int64    `json:"category_id"`
	Name          string   `json:"name"`
	ItemType      string   `json:"item_type"`
	Frequency     string   `json:"frequency"` // monthly | annual | irregular
	DefaultAmount *float64 `json:"default_amount"`
	DueDay        *int     `json:"due_day"`
	TargetMonth   *int     `json:"target_month"`
	CreditCardID  *int64   `json:"credit_card_id"`
	Active        bool     `json:"active"`
	Notes         *string  `json:"notes"`
}

type Entry struct {
	ID              int64    `json:"id"`
	RecurringItemID *int64   `json:"recurring_item_id"`
	CategoryID      int64    `json:"category_id"`
	PeriodYear      int      `json:"period_year"`
	PeriodMonth     int      `json:"period_month"`
	Name            string   `json:"name"`
	ItemType        string   `json:"item_type"`
	PlannedAmount   float64  `json:"planned_amount"`
	ActualAmount    *float64 `json:"actual_amount"`
	Status          string   `json:"status"` // planned | incurred
	CreditCardID    *int64   `json:"credit_card_id"`
}

type Settings struct {
	OpeningBalance float64 `json:"opening_balance"`
	OpeningYear    int     `json:"opening_year"`
	OpeningMonth   int     `json:"opening_month"`
}

type ForecastSummary struct {
	PeriodYear     int     `json:"period_year"`
	PeriodMonth    int     `json:"period_month"`
	BroughtForward float64 `json:"brought_forward"`
	Income         float64 `json:"income"`
	Expense        float64 `json:"expense"`
	Savings        float64 `json:"savings"`
	CarriedForward float64 `json:"carried_forward"`
}

// ---- Categories ----

func GetCategories() ([]Category, error) {
	rows, err := database.Query(`SELECT id, name, item_type, sort_order FROM categories ORDER BY sort_order, name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Category
	for rows.Next() {
		var c Category
		if err := rows.Scan(&c.ID, &c.Name, &c.ItemType, &c.SortOrder); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, nil
}

func AddCategory(c Category) (int64, error) {
	var id int64
	err := database.QueryRow(
		`INSERT INTO categories (name, item_type, sort_order) VALUES ($1, $2, $3) RETURNING id`,
		c.Name, c.ItemType, c.SortOrder,
	).Scan(&id)
	return id, err
}

// ---- Credit cards ----

func GetCreditCards() ([]CreditCard, error) {
	rows, err := database.Query(`SELECT id, name, statement_day, payment_due_day FROM credit_cards ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []CreditCard
	for rows.Next() {
		var c CreditCard
		if err := rows.Scan(&c.ID, &c.Name, &c.StatementDay, &c.PaymentDueDay); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, nil
}

func AddCreditCard(c CreditCard) (int64, error) {
	var id int64
	err := database.QueryRow(
		`INSERT INTO credit_cards (name, statement_day, payment_due_day) VALUES ($1, $2, $3) RETURNING id`,
		c.Name, c.StatementDay, c.PaymentDueDay,
	).Scan(&id)
	return id, err
}

func UpdateCreditCard(id int64, c CreditCard) error {
	_, err := database.Exec(
		`UPDATE credit_cards SET name=$2, statement_day=$3, payment_due_day=$4 WHERE id=$1`,
		id, c.Name, c.StatementDay, c.PaymentDueDay,
	)
	return err
}

// ---- Recurring items (templates) ----

func GetRecurringItems() ([]RecurringItem, error) {
	rows, err := database.Query(`
		SELECT id, category_id, name, item_type, frequency, default_amount,
		       due_day, target_month, credit_card_id, active, notes
		FROM recurring_items ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []RecurringItem
	for rows.Next() {
		var r RecurringItem
		if err := rows.Scan(&r.ID, &r.CategoryID, &r.Name, &r.ItemType, &r.Frequency,
			&r.DefaultAmount, &r.DueDay, &r.TargetMonth, &r.CreditCardID, &r.Active, &r.Notes); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, nil
}

func AddRecurringItem(r RecurringItem) (int64, error) {
	var id int64
	err := database.QueryRow(`
		INSERT INTO recurring_items
			(category_id, name, item_type, frequency, default_amount, due_day, target_month, credit_card_id, active, notes)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10) RETURNING id`,
		r.CategoryID, r.Name, r.ItemType, r.Frequency, r.DefaultAmount,
		r.DueDay, r.TargetMonth, r.CreditCardID, r.Active, r.Notes,
	).Scan(&id)
	return id, err
}

func UpdateRecurringItem(id int64, r RecurringItem) error {
	_, err := database.Exec(`
		UPDATE recurring_items SET
			category_id=$2, name=$3, item_type=$4, frequency=$5, default_amount=$6,
			due_day=$7, target_month=$8, credit_card_id=$9, active=$10, notes=$11
		WHERE id=$1`,
		id, r.CategoryID, r.Name, r.ItemType, r.Frequency, r.DefaultAmount,
		r.DueDay, r.TargetMonth, r.CreditCardID, r.Active, r.Notes,
	)
	return err
}

func DeleteRecurringItem(id int64) error {
	_, err := database.Exec(`DELETE FROM recurring_items WHERE id=$1`, id)
	return err
}

// ---- Entries ----

func GetEntries(year, month int) ([]Entry, error) {
	rows, err := database.Query(`
		SELECT id, recurring_item_id, category_id, period_year, period_month,
		       name, item_type, planned_amount, actual_amount, status, credit_card_id
		FROM entries WHERE period_year=$1 AND period_month=$2 ORDER BY id`, year, month)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Entry
	for rows.Next() {
		var e Entry
		if err := rows.Scan(&e.ID, &e.RecurringItemID, &e.CategoryID, &e.PeriodYear, &e.PeriodMonth,
			&e.Name, &e.ItemType, &e.PlannedAmount, &e.ActualAmount, &e.Status, &e.CreditCardID); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, nil
}

func AddEntry(e Entry) (int64, error) {
	var id int64
	err := database.QueryRow(`
		INSERT INTO entries
			(recurring_item_id, category_id, period_year, period_month, name, item_type,
			 planned_amount, actual_amount, status, credit_card_id)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10) RETURNING id`,
		e.RecurringItemID, e.CategoryID, e.PeriodYear, e.PeriodMonth, e.Name, e.ItemType,
		e.PlannedAmount, e.ActualAmount, e.Status, e.CreditCardID,
	).Scan(&id)
	return id, err
}

func UpdateEntry(id int64, e Entry) error {
	_, err := database.Exec(`
		UPDATE entries SET
			category_id=$2, name=$3, item_type=$4, planned_amount=$5,
			actual_amount=$6, status=$7, credit_card_id=$8
		WHERE id=$1`,
		id, e.CategoryID, e.Name, e.ItemType, e.PlannedAmount, e.ActualAmount, e.Status, e.CreditCardID,
	)
	return err
}

func DeleteEntry(id int64) error {
	_, err := database.Exec(`DELETE FROM entries WHERE id=$1`, id)
	return err
}

// GeneratePeriodEntries creates entries for (year, month) from active
// recurring_items templates, if they don't already exist. Safe to call
// repeatedly (idempotent via the entries.recurring_item_id/year/month
// unique constraint). Returns how many new entries were created.
//
//   - monthly items: generated every period.
//   - annual items: generated only when target_month matches, so they
//     reappear automatically the following year instead of being lost.
//   - irregular items: never auto-generated; added ad-hoc via AddEntry.
func GeneratePeriodEntries(year, month int) (int, error) {
	rows, err := database.Query(`
		SELECT id, category_id, name, item_type, frequency, default_amount, credit_card_id
		FROM recurring_items
		WHERE active = TRUE
		  AND (frequency = 'monthly' OR (frequency = 'annual' AND target_month = $1))`, month)
	if err != nil {
		return 0, err
	}
	defer rows.Close()

	type tmpl struct {
		id            int64
		categoryID    int64
		name          string
		itemType      string
		frequency     string
		defaultAmount *float64
		creditCardID  *int64
	}
	var templates []tmpl
	for rows.Next() {
		var t tmpl
		if err := rows.Scan(&t.id, &t.categoryID, &t.name, &t.itemType, &t.frequency, &t.defaultAmount, &t.creditCardID); err != nil {
			return 0, err
		}
		templates = append(templates, t)
	}

	created := 0
	for _, t := range templates {
		amount := 0.0
		if t.defaultAmount != nil {
			amount = *t.defaultAmount
		}
		res, err := database.Exec(`
			INSERT INTO entries (recurring_item_id, category_id, period_year, period_month, name, item_type, planned_amount, credit_card_id)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8)
			ON CONFLICT (recurring_item_id, period_year, period_month) DO NOTHING`,
			t.id, t.categoryID, year, month, t.name, t.itemType, amount, t.creditCardID,
		)
		if err != nil {
			return created, err
		}
		if n, _ := res.RowsAffected(); n > 0 {
			created++
		}
	}
	return created, nil
}

// ---- Settings ----

func GetSettings() (Settings, error) {
	var s Settings
	err := database.QueryRow(`SELECT opening_balance, opening_year, opening_month FROM settings WHERE id = TRUE`).
		Scan(&s.OpeningBalance, &s.OpeningYear, &s.OpeningMonth)
	return s, err
}

func PutSettings(s Settings) error {
	_, err := database.Exec(`
		INSERT INTO settings (id, opening_balance, opening_year, opening_month)
		VALUES (TRUE, $1, $2, $3)
		ON CONFLICT (id) DO UPDATE SET opening_balance=$1, opening_year=$2, opening_month=$3`,
		s.OpeningBalance, s.OpeningYear, s.OpeningMonth,
	)
	return err
}

// ---- Forecast ----

// periodNet returns income, expense, savings totals for one period, using
// actual_amount where the entry is incurred and planned_amount otherwise.
func periodNet(year, month int) (income, expense, savings float64, err error) {
	rows, err := database.Query(`
		SELECT item_type, COALESCE(actual_amount, planned_amount)
		FROM entries WHERE period_year=$1 AND period_month=$2`, year, month)
	if err != nil {
		return 0, 0, 0, err
	}
	defer rows.Close()
	for rows.Next() {
		var itemType string
		var amount float64
		if err := rows.Scan(&itemType, &amount); err != nil {
			return 0, 0, 0, err
		}
		switch itemType {
		case "income":
			income += amount
		case "expense":
			expense += amount
		case "savings":
			savings += amount
		}
	}
	return income, expense, savings, nil
}

// nextPeriod/prevPeriod step a (year, month) pair by one calendar month.
func nextPeriod(year, month int) (int, int) {
	month++
	if month > 12 {
		month = 1
		year++
	}
	return year, month
}

// Forecast computes the brought-forward/carried-forward summary for a
// single period by walking forward from the configured opening balance.
// (Cashflow history is short-lived in practice -- a handful of years at
// most -- so this is simple and always consistent, never stored/drifted.)
func Forecast(year, month int) (ForecastSummary, error) {
	settings, err := GetSettings()
	if err != nil {
		return ForecastSummary{}, err
	}

	balance := settings.OpeningBalance
	y, m := settings.OpeningYear, settings.OpeningMonth
	for y < year || (y == year && m < month) {
		inc, exp, sav, err := periodNet(y, m)
		if err != nil {
			return ForecastSummary{}, err
		}
		balance += inc - exp - sav
		y, m = nextPeriod(y, m)
	}

	inc, exp, sav, err := periodNet(year, month)
	if err != nil {
		return ForecastSummary{}, err
	}
	return ForecastSummary{
		PeriodYear:     year,
		PeriodMonth:    month,
		BroughtForward: balance,
		Income:         inc,
		Expense:        exp,
		Savings:        sav,
		CarriedForward: balance + inc - exp - sav,
	}, nil
}

// ForecastRange computes consecutive period summaries from (fromYear,
// fromMonth) for `count` months, in one forward pass.
func ForecastRange(fromYear, fromMonth, count int) ([]ForecastSummary, error) {
	settings, err := GetSettings()
	if err != nil {
		return nil, err
	}

	balance := settings.OpeningBalance
	y, m := settings.OpeningYear, settings.OpeningMonth
	for y < fromYear || (y == fromYear && m < fromMonth) {
		inc, exp, sav, err := periodNet(y, m)
		if err != nil {
			return nil, err
		}
		balance += inc - exp - sav
		y, m = nextPeriod(y, m)
	}

	out := make([]ForecastSummary, 0, count)
	for i := 0; i < count; i++ {
		inc, exp, sav, err := periodNet(y, m)
		if err != nil {
			return nil, err
		}
		carried := balance + inc - exp - sav
		out = append(out, ForecastSummary{
			PeriodYear: y, PeriodMonth: m,
			BroughtForward: balance, Income: inc, Expense: exp, Savings: sav,
			CarriedForward: carried,
		})
		balance = carried
		y, m = nextPeriod(y, m)
	}
	return out, nil
}
