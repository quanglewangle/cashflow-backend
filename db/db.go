package db

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"time"

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
	ID                    int64  `json:"id"`
	Name                  string `json:"name"`
	StatementDay          int    `json:"statement_day"`
	PaymentDueDay         int    `json:"payment_due_day"`
	PaymentDueMonthOffset int    `json:"payment_due_month_offset"`
}

type CardPurchase struct {
	ID           int64     `json:"id"`
	CreditCardID int64     `json:"credit_card_id"`
	Description  string    `json:"description"`
	Amount       float64   `json:"amount"`
	PurchaseDate time.Time `json:"purchase_date"`
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

type BalanceCheckpoint struct {
	ID          int64   `json:"id"`
	PeriodYear  int     `json:"period_year"`
	PeriodMonth int     `json:"period_month"`
	Balance     float64 `json:"balance"`
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
	rows, err := database.Query(`SELECT id, name, statement_day, payment_due_day, payment_due_month_offset FROM credit_cards ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []CreditCard
	for rows.Next() {
		var c CreditCard
		if err := rows.Scan(&c.ID, &c.Name, &c.StatementDay, &c.PaymentDueDay, &c.PaymentDueMonthOffset); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, nil
}

func getCreditCard(id int64) (CreditCard, error) {
	var c CreditCard
	err := database.QueryRow(
		`SELECT id, name, statement_day, payment_due_day, payment_due_month_offset FROM credit_cards WHERE id=$1`, id,
	).Scan(&c.ID, &c.Name, &c.StatementDay, &c.PaymentDueDay, &c.PaymentDueMonthOffset)
	return c, err
}

func AddCreditCard(c CreditCard) (int64, error) {
	if c.PaymentDueMonthOffset == 0 {
		c.PaymentDueMonthOffset = 1
	}
	var id int64
	err := database.QueryRow(
		`INSERT INTO credit_cards (name, statement_day, payment_due_day, payment_due_month_offset) VALUES ($1, $2, $3, $4) RETURNING id`,
		c.Name, c.StatementDay, c.PaymentDueDay, c.PaymentDueMonthOffset,
	).Scan(&id)
	return id, err
}

func UpdateCreditCard(id int64, c CreditCard) error {
	if c.PaymentDueMonthOffset == 0 {
		c.PaymentDueMonthOffset = 1
	}
	_, err := database.Exec(
		`UPDATE credit_cards SET name=$2, statement_day=$3, payment_due_day=$4, payment_due_month_offset=$5 WHERE id=$1`,
		id, c.Name, c.StatementDay, c.PaymentDueDay, c.PaymentDueMonthOffset,
	)
	return err
}

// ---- Card purchases ----
// Individual purchases on a card, attributed to a payment period (which
// month's bill they land on) using that card's statement_day and
// payment_due_month_offset -- this is the real per-transaction tracking
// requested instead of a flat monthly estimate.

// paymentPeriodFor works out which (year, month) a purchase's payment
// falls in: first the statement it belongs to (the next statement_day
// on/after the purchase date), then payment_due_month_offset months
// after that statement's month.
func paymentPeriodFor(card CreditCard, purchaseDate time.Time) (year, month int) {
	year, month = purchaseDate.Year(), int(purchaseDate.Month())
	if purchaseDate.Day() > card.StatementDay {
		year, month = nextPeriod(year, month)
	}
	for i := 0; i < card.PaymentDueMonthOffset; i++ {
		year, month = nextPeriod(year, month)
	}
	return year, month
}

// recurringItemForCard finds the (single, expected) monthly recurring item
// that represents this card's payment, so its generated entry can be kept
// in sync with actual purchases -- and so there's a default_amount to fall
// back to for periods nothing has been logged against yet.
func recurringItemForCard(cardID int64) (id int64, defaultAmount float64, found bool, err error) {
	var amount sql.NullFloat64
	err = database.QueryRow(`
		SELECT id, default_amount FROM recurring_items
		WHERE credit_card_id = $1 AND frequency = 'monthly' AND active = TRUE
		ORDER BY id LIMIT 1`, cardID,
	).Scan(&id, &amount)
	if err == sql.ErrNoRows {
		return 0, 0, false, nil
	}
	return id, amount.Float64, err == nil, err
}

// recalculateCardEntry sums this card's logged purchases for (year, month)
// and writes that total into the matching entry's planned_amount,
// generating the entry first if it doesn't exist yet. If nothing has been
// logged for that period at all, it falls back to the recurring item's
// flat default_amount estimate instead of forcing the entry to zero --
// otherwise a month you haven't started tracking purchases for yet would
// silently look like it costs nothing. actual_amount/status (once you've
// actually paid the bill) are always left untouched.
func recalculateCardEntry(cardID int64, year, month int) error {
	recItemID, defaultAmount, found, err := recurringItemForCard(cardID)
	if err != nil {
		return err
	}
	if !found {
		return nil // no recurring item wired to this card -- nothing to keep in sync
	}
	if _, err := GeneratePeriodEntries(year, month); err != nil {
		return err
	}

	// Each purchase's payment period depends on paymentPeriodFor, which
	// isn't expressible as plain SQL, so sum in Go rather than via SUM().
	total, count, err := sumPurchasesForPeriod(cardID, year, month)
	if err != nil {
		return err
	}
	if count == 0 {
		total = defaultAmount
	}

	_, err = database.Exec(`
		UPDATE entries SET planned_amount = $1
		WHERE recurring_item_id = $2 AND period_year = $3 AND period_month = $4`,
		total, recItemID, year, month,
	)
	return err
}

func sumPurchasesForPeriod(cardID int64, year, month int) (total float64, count int, err error) {
	card, err := getCreditCard(cardID)
	if err != nil {
		return 0, 0, err
	}
	rows, err := database.Query(`
		SELECT amount, purchase_date FROM card_purchases WHERE credit_card_id = $1`, cardID)
	if err != nil {
		return 0, 0, err
	}
	defer rows.Close()

	for rows.Next() {
		var amount float64
		var purchaseDate time.Time
		if err := rows.Scan(&amount, &purchaseDate); err != nil {
			return 0, 0, err
		}
		py, pm := paymentPeriodFor(card, purchaseDate)
		if py == year && pm == month {
			total += amount
			count++
		}
	}
	return total, count, nil
}

func GetCardPurchases(cardID int64) ([]CardPurchase, error) {
	rows, err := database.Query(`
		SELECT id, credit_card_id, description, amount, purchase_date
		FROM card_purchases WHERE credit_card_id = $1 ORDER BY purchase_date DESC, id DESC`, cardID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []CardPurchase
	for rows.Next() {
		var p CardPurchase
		if err := rows.Scan(&p.ID, &p.CreditCardID, &p.Description, &p.Amount, &p.PurchaseDate); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, nil
}

func AddCardPurchase(p CardPurchase) (int64, error) {
	var id int64
	err := database.QueryRow(`
		INSERT INTO card_purchases (credit_card_id, description, amount, purchase_date)
		VALUES ($1, $2, $3, $4) RETURNING id`,
		p.CreditCardID, p.Description, p.Amount, p.PurchaseDate,
	).Scan(&id)
	if err != nil {
		return 0, err
	}

	card, err := getCreditCard(p.CreditCardID)
	if err != nil {
		return id, err
	}
	year, month := paymentPeriodFor(card, p.PurchaseDate)
	return id, recalculateCardEntry(p.CreditCardID, year, month)
}

func DeleteCardPurchase(id int64) error {
	var cardID int64
	var purchaseDate time.Time
	err := database.QueryRow(`SELECT credit_card_id, purchase_date FROM card_purchases WHERE id=$1`, id).
		Scan(&cardID, &purchaseDate)
	if err == sql.ErrNoRows {
		return nil
	}
	if err != nil {
		return err
	}

	if _, err := database.Exec(`DELETE FROM card_purchases WHERE id=$1`, id); err != nil {
		return err
	}

	card, err := getCreditCard(cardID)
	if err != nil {
		return err
	}
	year, month := paymentPeriodFor(card, purchaseDate)
	return recalculateCardEntry(cardID, year, month)
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

// ---- Balance checkpoints ----

func GetCheckpoints() ([]BalanceCheckpoint, error) {
	rows, err := database.Query(`
		SELECT id, period_year, period_month, balance FROM balance_checkpoints
		ORDER BY period_year, period_month`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []BalanceCheckpoint
	for rows.Next() {
		var c BalanceCheckpoint
		if err := rows.Scan(&c.ID, &c.PeriodYear, &c.PeriodMonth, &c.Balance); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, nil
}

// AddCheckpoint records (or replaces) the known balance for a period, e.g.
// after checking the real bank app. Forecast uses the latest checkpoint
// at/before a period as its base, so this is how drift gets corrected.
func AddCheckpoint(year, month int, balance float64) (int64, error) {
	var id int64
	err := database.QueryRow(`
		INSERT INTO balance_checkpoints (period_year, period_month, balance)
		VALUES ($1, $2, $3)
		ON CONFLICT (period_year, period_month) DO UPDATE SET balance = $3
		RETURNING id`,
		year, month, balance,
	).Scan(&id)
	return id, err
}

// latestCheckpointAtOrBefore finds the most recent checkpoint at or before
// (year, month) to use as the forecast's starting point.
func latestCheckpointAtOrBefore(year, month int) (BalanceCheckpoint, bool, error) {
	var c BalanceCheckpoint
	err := database.QueryRow(`
		SELECT id, period_year, period_month, balance FROM balance_checkpoints
		WHERE period_year < $1 OR (period_year = $1 AND period_month <= $2)
		ORDER BY period_year DESC, period_month DESC
		LIMIT 1`, year, month,
	).Scan(&c.ID, &c.PeriodYear, &c.PeriodMonth, &c.Balance)
	if err == sql.ErrNoRows {
		return BalanceCheckpoint{}, false, nil
	}
	return c, err == nil, err
}

// ---- Forecast ----

// periodNet returns income, expense, savings totals for one period, using
// actual_amount where the entry is incurred and planned_amount otherwise.
// Materializes the period's entries from templates first (idempotent) so
// forecasting a month nobody has opened yet still reflects the templates,
// instead of silently showing zero until someone visits it.
func periodNet(year, month int) (income, expense, savings float64, err error) {
	if _, err := GeneratePeriodEntries(year, month); err != nil {
		return 0, 0, 0, err
	}

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
	checkpoint, found, err := latestCheckpointAtOrBefore(year, month)
	if err != nil {
		return ForecastSummary{}, err
	}
	if !found {
		return ForecastSummary{}, errors.New("no balance checkpoint at or before this period -- add one via POST /checkpoints")
	}

	balance := checkpoint.Balance
	y, m := checkpoint.PeriodYear, checkpoint.PeriodMonth
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
	checkpoint, found, err := latestCheckpointAtOrBefore(fromYear, fromMonth)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, errors.New("no balance checkpoint at or before this period -- add one via POST /checkpoints")
	}

	balance := checkpoint.Balance
	y, m := checkpoint.PeriodYear, checkpoint.PeriodMonth
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
