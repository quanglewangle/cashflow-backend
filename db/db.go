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
	ParentID  *int64 `json:"parent_id"` // null = top-level
}

type CreditCard struct {
	ID                    int64  `json:"id"`
	Name                  string `json:"name"`
	StatementDay          int    `json:"statement_day"`
	PaymentDueDay         int    `json:"payment_due_day"`
	PaymentDueMonthOffset int    `json:"payment_due_month_offset"`
}

type CardPurchase struct {
	ID                  int64     `json:"id"`
	CreditCardID        int64     `json:"credit_card_id"`
	Description         string    `json:"description"`
	Amount              float64   `json:"amount"`
	PurchaseDate        time.Time `json:"purchase_date"`
	RecurringPurchaseID *int64    `json:"recurring_purchase_id"`
	CategoryID          *int64    `json:"category_id"`
}

// The template for a card subscription (Netflix, etc.) -- generates
// card_purchases (see generateRecurringCardPurchases) rather than entries
// directly, so its cost still goes through the normal statement-cycle
// attribution like any other purchase on the card.
type RecurringCardPurchase struct {
	ID           int64   `json:"id"`
	CreditCardID int64   `json:"credit_card_id"`
	Description  string  `json:"description"`
	Amount       float64 `json:"amount"`
	Frequency    string  `json:"frequency"` // monthly | annual | irregular
	DayOfMonth   int     `json:"day_of_month"`
	TargetMonth  *int    `json:"target_month"` // annual only
	Active       bool    `json:"active"`
}

type RecurringItem struct {
	ID            int64      `json:"id"`
	CategoryID    int64      `json:"category_id"`
	Name          string     `json:"name"`
	ItemType      string     `json:"item_type"`
	Frequency     string     `json:"frequency"` // monthly | three_monthly | annual | irregular | four_weekly
	DefaultAmount *float64   `json:"default_amount"`
	DueDay        *int       `json:"due_day"`
	TargetMonth   *int       `json:"target_month"`
	AnchorDate    *string    `json:"anchor_date"` // ISO date "YYYY-MM-DD"; monthly: don't generate before this month; four_weekly: reference occurrence
	CreditCardID  *int64     `json:"credit_card_id"`
	Active        bool       `json:"active"`
	Notes         *string    `json:"notes"`
}

type Entry struct {
	ID              int64      `json:"id"`
	RecurringItemID *int64     `json:"recurring_item_id"`
	CategoryID      int64      `json:"category_id"`
	PeriodYear      int        `json:"period_year"`
	PeriodMonth     int        `json:"period_month"`
	Name            string     `json:"name"`
	ItemType        string     `json:"item_type"`
	PlannedAmount   float64    `json:"planned_amount"`
	ActualAmount    *float64   `json:"actual_amount"`
	Status          string     `json:"status"` // planned | incurred
	CreditCardID    *int64     `json:"credit_card_id"`
	DueDay          *int       `json:"due_day"`
	DecayPerWeek    *float64   `json:"decay_per_week"`
	DecayStartDate  *time.Time `json:"decay_start_date"`
	// EffectiveAmount is PlannedAmount with any decay applied as of now --
	// computed at read time, never stored. Client display/balance math
	// should prefer this over PlannedAmount; edit dialogs should still
	// show PlannedAmount, the undecayed original.
	EffectiveAmount float64 `json:"effective_amount"`
}

// effectiveEntryAmount applies decay_per_week's weekly reduction to
// planned_amount, floored at zero, as of now -- frozen once actual_amount
// is set (a confirmed real amount is never an estimate to decay).
func effectiveEntryAmount(plannedAmount float64, actualAmount *float64, decayPerWeek *float64, decayStartDate *time.Time) float64 {
	if actualAmount != nil {
		return *actualAmount
	}
	if decayPerWeek == nil || decayStartDate == nil {
		return plannedAmount
	}
	weeksElapsed := int(time.Since(*decayStartDate).Hours() / 24 / 7)
	if weeksElapsed < 0 {
		weeksElapsed = 0
	}
	amount := plannedAmount - *decayPerWeek*float64(weeksElapsed)
	if amount < 0 {
		amount = 0
	}
	return amount
}

type BalanceCheckpoint struct {
	ID          int64   `json:"id"`
	PeriodYear  int     `json:"period_year"`
	PeriodMonth int     `json:"period_month"`
	PeriodDay   int     `json:"period_day"`
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
	rows, err := database.Query(`SELECT id, name, item_type, sort_order, parent_id FROM categories ORDER BY sort_order, name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Category
	for rows.Next() {
		var c Category
		if err := rows.Scan(&c.ID, &c.Name, &c.ItemType, &c.SortOrder, &c.ParentID); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, nil
}

func AddCategory(c Category) (int64, error) {
	var id int64
	err := database.QueryRow(
		`INSERT INTO categories (name, item_type, sort_order, parent_id) VALUES ($1, $2, $3, $4) RETURNING id`,
		c.Name, c.ItemType, c.SortOrder, c.ParentID,
	).Scan(&id)
	return id, err
}

func UpdateCategory(id int64, c Category) error {
	_, err := database.Exec(
		`UPDATE categories SET name=$2, item_type=$3, sort_order=$4, parent_id=$5 WHERE id=$1`,
		id, c.Name, c.ItemType, c.SortOrder, c.ParentID,
	)
	return err
}

// ---- Credit cards ----

func GetCreditCards() ([]CreditCard, error) {
	rows, err := database.Query(`SELECT id, name, statement_day, payment_due_day, payment_due_month_offset FROM credit_cards ORDER BY id`)
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
type cardRecurringItem struct {
	id            int64
	categoryID    int64
	name          string
	itemType      string
	defaultAmount float64
}

func recurringItemForCard(cardID int64) (item cardRecurringItem, found bool, err error) {
	var amount sql.NullFloat64
	err = database.QueryRow(`
		SELECT id, category_id, name, item_type, default_amount FROM recurring_items
		WHERE credit_card_id = $1 AND frequency = 'monthly' AND active = TRUE
		ORDER BY id LIMIT 1`, cardID,
	).Scan(&item.id, &item.categoryID, &item.name, &item.itemType, &amount)
	if err == sql.ErrNoRows {
		return cardRecurringItem{}, false, nil
	}
	item.defaultAmount = amount.Float64
	return item, err == nil, err
}

// recalculateCardEntry sums this card's logged purchases for (year, month)
// and upserts that total directly into the matching entry's planned_amount
// -- creating the entry itself if needed, scoped to just this one
// (recurring_item_id, year, month). If nothing has been logged for that
// period at all, it falls back to the recurring item's flat default_amount
// estimate instead of forcing the entry to zero -- otherwise a month you
// haven't started tracking purchases for yet would silently look like it
// costs nothing. actual_amount/status (once you've actually paid the bill)
// are always left untouched.
//
// Deliberately does NOT call GeneratePeriodEntries: that generates every
// recurring item AND every card's subscriptions for the period, and
// subscriptions call back into this function for *their own* future
// payment period -- which previously cascaded forward one month at a time,
// forever (a generated subscription recalculates next month's entry, which
// generated that month's subscriptions, recalculating the month after, ...).
// This function only ever touches the single entry it's responsible for.
func recalculateCardEntry(cardID int64, year, month int) error {
	item, found, err := recurringItemForCard(cardID)
	if err != nil {
		return err
	}
	if !found {
		return nil // no recurring item wired to this card -- nothing to keep in sync
	}

	// Each purchase's payment period depends on paymentPeriodFor, which
	// isn't expressible as plain SQL, so sum in Go rather than via SUM().
	total, hasData, oneOffTotal, err := sumPurchasesForPeriod(cardID, year, month)
	if err != nil {
		return err
	}
	if !hasData {
		total = item.defaultAmount
	}
	total += oneOffTotal

	_, err = database.Exec(`
		INSERT INTO entries (recurring_item_id, category_id, period_year, period_month, name, item_type, planned_amount, credit_card_id)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8)
		ON CONFLICT (recurring_item_id, period_year, period_month) DO UPDATE SET planned_amount = $7`,
		item.id, item.categoryID, year, month, item.name, item.itemType, total, cardID,
	)
	return err
}

// sumPurchasesForPeriod totals this card's purchases whose payment period is
// (year, month). If a card checkpoint's own date falls in that same payment
// period (i.e. verifying/correcting the log partway through this statement
// cycle), the latest such checkpoint's balance is used as the starting
// point instead of the raw purchase log, and only purchases dated after it
// are added on top -- otherwise purchases missed by logging (e.g. a Google
// Pay tap that never got confirmed) would drift the repayment amount away
// from the real balance forever, since the checkpoint was never consulted.
func sumPurchasesForPeriod(cardID int64, year, month int) (total float64, hasData bool, oneOffTotal float64, err error) {
	card, err := getCreditCard(cardID)
	if err != nil {
		return 0, false, 0, err
	}

	checkpoint, hasCheckpoint, err := latestCardCheckpointForPeriod(card, year, month)
	if err != nil {
		return 0, false, 0, err
	}
	var afterDate time.Time
	if hasCheckpoint {
		total = checkpoint.Balance
		hasData = true
		afterDate = time.Date(checkpoint.PeriodYear, time.Month(checkpoint.PeriodMonth), checkpoint.PeriodDay, 0, 0, 0, 0, time.UTC)

		// A checkpoint is a snapshot of everything posted to the card up to that
		// date, including any earlier statement's bill that hasn't been paid off
		// yet -- that amount is already baked into checkpoint.Balance, but it's
		// also counted on its own as that earlier period's entry, so leaving it
		// in here would double it. Net it back out.
		unpaidPrior, err := sumUnpaidPriorCardBills(cardID, year, month)
		if err != nil {
			return 0, false, 0, err
		}
		total -= unpaidPrior
	}

	rows, err := database.Query(`
		SELECT amount, purchase_date FROM card_purchases WHERE credit_card_id = $1`, cardID)
	if err != nil {
		return 0, false, 0, err
	}
	defer rows.Close()

	for rows.Next() {
		var amount float64
		var purchaseDate time.Time
		if err := rows.Scan(&amount, &purchaseDate); err != nil {
			return 0, false, 0, err
		}
		py, pm := paymentPeriodFor(card, purchaseDate)
		if py != year || pm != month {
			continue
		}
		if hasCheckpoint && !purchaseDate.After(afterDate) {
			continue // already reflected in the checkpoint balance
		}
		total += amount
		hasData = true
	}

	// One-off entries deliberately tagged with this card (e.g. a decaying
	// "sundries" contingency for anticipated-but-not-yet-logged spending) --
	// added on top for the payment period the user gave them directly, no
	// paymentPeriodFor translation needed since they're not dated purchases.
	// periodNet/periodNetFrom exclude these from the general forecast so they
	// aren't also counted as an independent line -- they're folded in here.
	// Kept out of hasData/total deliberately: a lone one-off with no real
	// checkpoint/purchase data for the period must still ADD ON TOP of the
	// recurring item's flat default_amount fallback in recalculateCardEntry,
	// never silently replace it.
	oneOffRows, err := database.Query(`
		SELECT item_type, planned_amount, actual_amount, decay_per_week, decay_start_date
		FROM entries WHERE credit_card_id = $1 AND recurring_item_id IS NULL
		AND period_year = $2 AND period_month = $3`, cardID, year, month)
	if err != nil {
		return 0, false, 0, err
	}
	defer oneOffRows.Close()

	for oneOffRows.Next() {
		var itemType string
		var plannedAmount float64
		var actualAmount *float64
		var decayPerWeek *float64
		var decayStartDate *time.Time
		if err := oneOffRows.Scan(&itemType, &plannedAmount, &actualAmount, &decayPerWeek, &decayStartDate); err != nil {
			return 0, false, 0, err
		}
		amount := effectiveEntryAmount(plannedAmount, actualAmount, decayPerWeek, decayStartDate)
		if itemType == "income" {
			oneOffTotal -= amount
		} else {
			oneOffTotal += amount
		}
	}
	return total, hasData, oneOffTotal, nil
}

// sumUnpaidPriorCardBills totals this card's own repayment entries for every
// payment period strictly before (year, month) that aren't marked incurred
// yet -- amounts a checkpoint dated after those periods closed would already
// include in its running balance, but that are still due to be settled on
// their own as that earlier period's entry. Returns 0 if the card has no
// dedicated repayment recurring item (e.g. Barclaycard, paid down by a fixed
// amount rather than in full each cycle -- see recurringItemForCard).
func sumUnpaidPriorCardBills(cardID int64, year, month int) (float64, error) {
	item, found, err := recurringItemForCard(cardID)
	if err != nil || !found {
		return 0, err
	}
	// Only the single period immediately before this one -- a pay-in-full card
	// realistically has at most one statement awaiting payment at a time. An
	// older "planned" entry is almost always just never having been marked
	// paid rather than a genuine pile of unpaid statements, so scanning
	// further back would wrongly net out settled bills too.
	prevYear, prevMonth := prevPeriod(year, month)
	var plannedAmount float64
	var actualAmount *float64
	var decayPerWeek *float64
	var decayStartDate *time.Time
	var status string
	err = database.QueryRow(`
		SELECT planned_amount, actual_amount, decay_per_week, decay_start_date, status
		FROM entries WHERE recurring_item_id = $1 AND period_year = $2 AND period_month = $3`,
		item.id, prevYear, prevMonth,
	).Scan(&plannedAmount, &actualAmount, &decayPerWeek, &decayStartDate, &status)
	if err == sql.ErrNoRows {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	if status == "incurred" {
		return 0, nil
	}
	return effectiveEntryAmount(plannedAmount, actualAmount, decayPerWeek, decayStartDate), nil
}

// prevPeriod steps a (year, month) pair back by one calendar month.
func prevPeriod(year, month int) (int, int) {
	month--
	if month < 1 {
		month = 12
		year--
	}
	return year, month
}

// latestCardCheckpointForPeriod finds the most recent checkpoint for this
// card whose own date maps (via paymentPeriodFor, the same rule used for
// purchases) to this same payment period -- i.e. it was taken partway
// through the statement cycle that pays out in (year, month).
func latestCardCheckpointForPeriod(card CreditCard, year, month int) (cp CardCheckpoint, found bool, err error) {
	rows, err := database.Query(`
		SELECT id, credit_card_id, period_year, period_month, period_day, balance
		FROM card_checkpoints WHERE credit_card_id = $1`, card.ID)
	if err != nil {
		return CardCheckpoint{}, false, err
	}
	defer rows.Close()

	var bestDate time.Time
	for rows.Next() {
		var c CardCheckpoint
		if err := rows.Scan(&c.ID, &c.CreditCardID, &c.PeriodYear, &c.PeriodMonth, &c.PeriodDay, &c.Balance); err != nil {
			return CardCheckpoint{}, false, err
		}
		cpDate := time.Date(c.PeriodYear, time.Month(c.PeriodMonth), c.PeriodDay, 0, 0, 0, 0, time.UTC)
		py, pm := paymentPeriodFor(card, cpDate)
		if py != year || pm != month {
			continue
		}
		if !found || cpDate.After(bestDate) {
			cp, found, bestDate = c, true, cpDate
		}
	}
	return cp, found, nil
}

// CardPaymentBreakdown explains how a card's payment-period entry total was
// arrived at: the checkpoint it anchored to (if any), every purchase already
// folded into that checkpoint (informational -- doesn't add to Total), and
// every purchase added on top (does add to Total), in the same order
// sumPurchasesForPeriod would sum them.
type CardPaymentBreakdown struct {
	Checkpoint          *CardCheckpoint `json:"checkpoint"`
	CoveredByCheckpoint []CardPurchase  `json:"covered_by_checkpoint"`
	Purchases           []CardPurchase  `json:"purchases"`
	OneOffs             []Entry         `json:"one_offs"`              // card-tagged one-offs added on top (e.g. a sundries buffer)
	UnpaidPriorBill     *Entry          `json:"unpaid_prior_bill"`     // netted out -- see sumUnpaidPriorCardBills
	DefaultAmountUsed   *float64        `json:"default_amount_used"`   // set when no checkpoint/purchase exists yet and the recurring item's flat default was used instead -- see recalculateCardEntry
	Total               float64         `json:"total"`
}

// GetCardPaymentBreakdown mirrors sumPurchasesForPeriod exactly, but returns
// the checkpoint/purchases/one-offs/netted-prior-bill behind the total
// instead of just the sum.
func GetCardPaymentBreakdown(cardID int64, year, month int) (CardPaymentBreakdown, error) {
	card, err := getCreditCard(cardID)
	if err != nil {
		return CardPaymentBreakdown{}, err
	}

	checkpoint, hasCheckpoint, err := latestCardCheckpointForPeriod(card, year, month)
	if err != nil {
		return CardPaymentBreakdown{}, err
	}
	result := CardPaymentBreakdown{}
	var afterDate time.Time
	if hasCheckpoint {
		cp := checkpoint
		result.Checkpoint = &cp
		result.Total = checkpoint.Balance
		afterDate = time.Date(checkpoint.PeriodYear, time.Month(checkpoint.PeriodMonth), checkpoint.PeriodDay, 0, 0, 0, 0, time.UTC)

		prevYear, prevMonth := prevPeriod(year, month)
		if item, found, err := recurringItemForCard(cardID); err == nil && found {
			var e Entry
			err := database.QueryRow(`
				SELECT id, recurring_item_id, category_id, period_year, period_month, name, item_type,
				       planned_amount, actual_amount, status, credit_card_id, due_day, decay_per_week, decay_start_date
				FROM entries WHERE recurring_item_id = $1 AND period_year = $2 AND period_month = $3`,
				item.id, prevYear, prevMonth,
			).Scan(&e.ID, &e.RecurringItemID, &e.CategoryID, &e.PeriodYear, &e.PeriodMonth, &e.Name, &e.ItemType,
				&e.PlannedAmount, &e.ActualAmount, &e.Status, &e.CreditCardID, &e.DueDay, &e.DecayPerWeek, &e.DecayStartDate)
			if err == nil && e.Status != "incurred" {
				e.EffectiveAmount = effectiveEntryAmount(e.PlannedAmount, e.ActualAmount, e.DecayPerWeek, e.DecayStartDate)
				result.UnpaidPriorBill = &e
				result.Total -= e.EffectiveAmount
			}
		}
	}

	rows, err := database.Query(`
		SELECT id, credit_card_id, description, amount, purchase_date, recurring_purchase_id, category_id
		FROM card_purchases WHERE credit_card_id = $1 ORDER BY purchase_date, id`, cardID)
	if err != nil {
		return CardPaymentBreakdown{}, err
	}
	defer rows.Close()

	hasRealPurchase := false
	for rows.Next() {
		var p CardPurchase
		if err := rows.Scan(&p.ID, &p.CreditCardID, &p.Description, &p.Amount, &p.PurchaseDate, &p.RecurringPurchaseID, &p.CategoryID); err != nil {
			return CardPaymentBreakdown{}, err
		}
		py, pm := paymentPeriodFor(card, p.PurchaseDate)
		if py != year || pm != month {
			continue
		}
		if hasCheckpoint && !p.PurchaseDate.After(afterDate) {
			result.CoveredByCheckpoint = append(result.CoveredByCheckpoint, p)
			continue
		}
		result.Purchases = append(result.Purchases, p)
		result.Total += p.Amount
		hasRealPurchase = true
	}

	// Mirrors recalculateCardEntry: with no checkpoint and nothing real logged
	// yet for this period, the entry's own total falls back to the recurring
	// item's flat default_amount -- shown here too so the breakdown matches
	// what the entry actually displays, with one-offs still added on top below.
	if !hasCheckpoint && !hasRealPurchase {
		if item, found, ferr := recurringItemForCard(cardID); ferr == nil && found {
			amt := item.defaultAmount
			result.DefaultAmountUsed = &amt
			result.Total = amt
		}
	}

	oneOffs, err := GetCardTaggedOneOffs(cardID, year, month)
	if err != nil {
		return CardPaymentBreakdown{}, err
	}
	for _, e := range oneOffs {
		result.OneOffs = append(result.OneOffs, e)
		if e.ItemType == "income" {
			result.Total -= e.EffectiveAmount
		} else {
			result.Total += e.EffectiveAmount
		}
	}
	return result, nil
}

func GetCardPurchasesByMonth(year, month int) ([]CardPurchase, error) {
	rows, err := database.Query(`
		SELECT id, credit_card_id, description, amount, purchase_date, recurring_purchase_id, category_id
		FROM card_purchases
		WHERE EXTRACT(YEAR FROM purchase_date) = $1 AND EXTRACT(MONTH FROM purchase_date) = $2
		ORDER BY purchase_date, id`, year, month)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []CardPurchase
	for rows.Next() {
		var p CardPurchase
		if err := rows.Scan(&p.ID, &p.CreditCardID, &p.Description, &p.Amount, &p.PurchaseDate, &p.RecurringPurchaseID, &p.CategoryID); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, nil
}

func GetCardPurchases(cardID int64) ([]CardPurchase, error) {
	rows, err := database.Query(`
		SELECT id, credit_card_id, description, amount, purchase_date, recurring_purchase_id, category_id
		FROM card_purchases WHERE credit_card_id = $1 ORDER BY purchase_date DESC, id DESC`, cardID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []CardPurchase
	for rows.Next() {
		var p CardPurchase
		if err := rows.Scan(&p.ID, &p.CreditCardID, &p.Description, &p.Amount, &p.PurchaseDate, &p.RecurringPurchaseID, &p.CategoryID); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, nil
}

func AddCardPurchase(p CardPurchase) (int64, error) {
	var id int64
	err := database.QueryRow(`
		INSERT INTO card_purchases (credit_card_id, description, amount, purchase_date, recurring_purchase_id, category_id)
		VALUES ($1, $2, $3, $4, $5, $6) RETURNING id`,
		p.CreditCardID, p.Description, p.Amount, p.PurchaseDate, p.RecurringPurchaseID, p.CategoryID,
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

// UpdateCardPurchase edits a (manually-added) purchase's description/amount/
// date, recalculating both its old and new payment period -- editing the
// date can move it from one card payment entry to another.
func UpdateCardPurchase(id int64, p CardPurchase) error {
	var oldCardID int64
	var oldDate time.Time
	err := database.QueryRow(`SELECT credit_card_id, purchase_date FROM card_purchases WHERE id=$1`, id).
		Scan(&oldCardID, &oldDate)
	if err != nil {
		return err
	}

	if _, err := database.Exec(`
		UPDATE card_purchases SET description=$2, amount=$3, purchase_date=$4, category_id=$5 WHERE id=$1`,
		id, p.Description, p.Amount, p.PurchaseDate, p.CategoryID,
	); err != nil {
		return err
	}

	oldCard, err := getCreditCard(oldCardID)
	if err != nil {
		return err
	}
	oldYear, oldMonth := paymentPeriodFor(oldCard, oldDate)
	if err := recalculateCardEntry(oldCardID, oldYear, oldMonth); err != nil {
		return err
	}

	newYear, newMonth := paymentPeriodFor(oldCard, p.PurchaseDate)
	if newYear == oldYear && newMonth == oldMonth {
		return nil
	}
	return recalculateCardEntry(oldCardID, newYear, newMonth)
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
		       due_day, target_month, anchor_date::text, credit_card_id, active, notes
		FROM recurring_items ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []RecurringItem
	for rows.Next() {
		var r RecurringItem
		if err := rows.Scan(&r.ID, &r.CategoryID, &r.Name, &r.ItemType, &r.Frequency,
			&r.DefaultAmount, &r.DueDay, &r.TargetMonth, &r.AnchorDate, &r.CreditCardID, &r.Active, &r.Notes); err != nil {
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
			(category_id, name, item_type, frequency, default_amount, due_day, target_month, anchor_date, credit_card_id, active, notes)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11) RETURNING id`,
		r.CategoryID, r.Name, r.ItemType, r.Frequency, r.DefaultAmount,
		r.DueDay, r.TargetMonth, r.AnchorDate, r.CreditCardID, r.Active, r.Notes,
	).Scan(&id)
	return id, err
}

func UpdateRecurringItem(id int64, r RecurringItem) error {
	// Read old default_amount so we can propagate a change to entries that still
	// carry it as their planned_amount (i.e. the card entry has never had real
	// purchases logged and is just showing the template estimate).
	var oldAmount sql.NullFloat64
	_ = database.QueryRow(`SELECT default_amount FROM recurring_items WHERE id=$1`, id).Scan(&oldAmount)

	_, err := database.Exec(`
		UPDATE recurring_items SET
			category_id=$2, name=$3, item_type=$4, frequency=$5, default_amount=$6,
			due_day=$7, target_month=$8, anchor_date=$9, credit_card_id=$10, active=$11, notes=$12
		WHERE id=$1`,
		id, r.CategoryID, r.Name, r.ItemType, r.Frequency, r.DefaultAmount,
		r.DueDay, r.TargetMonth, r.AnchorDate, r.CreditCardID, r.Active, r.Notes,
	)
	if err != nil {
		return err
	}

	// Propagate due_day change to unpaid entries so their position matches the template.
	if r.DueDay != nil {
		database.Exec(`UPDATE entries SET due_day = $1 WHERE recurring_item_id = $2 AND actual_amount IS NULL`,
			*r.DueDay, id)
	}
	// Propagate item_type change to unpaid entries so their income/expense sign
	// matches the template -- otherwise an already-generated entry keeps whatever
	// type it had at generation time forever, silently throwing off carried-forward.
	database.Exec(`UPDATE entries SET item_type = $1 WHERE recurring_item_id = $2 AND actual_amount IS NULL`,
		r.ItemType, id)
	// When frequency is changed to last_working_day, recalculate each unpaid entry's
	// due_day to the correct last Mon-Fri for its own period.
	if r.Frequency == "last_working_day" {
		rows, err := database.Query(`
			SELECT DISTINCT period_year, period_month FROM entries
			WHERE recurring_item_id = $1 AND actual_amount IS NULL`, id)
		if err == nil {
			type period struct{ year, month int }
			var periods []period
			for rows.Next() {
				var p period
				if rows.Scan(&p.year, &p.month) == nil {
					periods = append(periods, p)
				}
			}
			rows.Close()
			for _, p := range periods {
				day := lastWorkingDayOfMonth(p.year, p.month)
				database.Exec(`UPDATE entries SET due_day = $1 WHERE recurring_item_id = $2 AND period_year = $3 AND period_month = $4 AND actual_amount IS NULL`,
					day, id, p.year, p.month)
			}
		}
	}

	if r.DefaultAmount != nil && oldAmount.Valid && *r.DefaultAmount != oldAmount.Float64 {
		if r.CreditCardID != nil {
			// Card-linked: recalculate each period so months with real purchases keep
			// their purchase total and months with no purchases get the new default.
			rows, err := database.Query(`
				SELECT DISTINCT period_year, period_month FROM entries
				WHERE recurring_item_id = $1 AND actual_amount IS NULL`, id)
			if err == nil {
				type period struct{ year, month int }
				var periods []period
				for rows.Next() {
					var p period
					if rows.Scan(&p.year, &p.month) == nil {
						periods = append(periods, p)
					}
				}
				rows.Close()
				for _, p := range periods {
					recalculateCardEntry(*r.CreditCardID, p.year, p.month)
				}
			}
		} else {
			// Non-card item: simply update all unpaid planned entries to the new amount.
			database.Exec(`
				UPDATE entries SET planned_amount = $1
				WHERE recurring_item_id = $2 AND actual_amount IS NULL`,
				*r.DefaultAmount, id)
		}
	}
	return nil
}

func DeleteRecurringItem(id int64) error {
	// Remove planned entries generated from this item; null out the FK on
	// incurred ones so the historical record is kept but the item can be deleted.
	if _, err := database.Exec(`DELETE FROM entries WHERE recurring_item_id=$1 AND status='planned'`, id); err != nil {
		return err
	}
	if _, err := database.Exec(`UPDATE entries SET recurring_item_id=NULL WHERE recurring_item_id=$1`, id); err != nil {
		return err
	}
	_, err := database.Exec(`DELETE FROM recurring_items WHERE id=$1`, id)
	return err
}

// ---- Entries ----

func GetEntries(year, month int) ([]Entry, error) {
	rows, err := database.Query(`
		SELECT id, recurring_item_id, category_id, period_year, period_month,
		       name, item_type, planned_amount, actual_amount, status, credit_card_id, due_day,
		       decay_per_week, decay_start_date
		FROM entries WHERE period_year=$1 AND period_month=$2 ORDER BY due_day NULLS LAST, id`, year, month)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Entry
	for rows.Next() {
		var e Entry
		if err := rows.Scan(&e.ID, &e.RecurringItemID, &e.CategoryID, &e.PeriodYear, &e.PeriodMonth,
			&e.Name, &e.ItemType, &e.PlannedAmount, &e.ActualAmount, &e.Status, &e.CreditCardID, &e.DueDay,
			&e.DecayPerWeek, &e.DecayStartDate); err != nil {
			return nil, err
		}
		e.EffectiveAmount = effectiveEntryAmount(e.PlannedAmount, e.ActualAmount, e.DecayPerWeek, e.DecayStartDate)
		out = append(out, e)
	}
	return out, nil
}

func AddEntry(e Entry) (int64, error) {
	if e.DecayPerWeek != nil && e.DecayStartDate == nil {
		today := time.Now().Truncate(24 * time.Hour)
		e.DecayStartDate = &today
	}
	var id int64
	err := database.QueryRow(`
		INSERT INTO entries
			(recurring_item_id, category_id, period_year, period_month, name, item_type,
			 planned_amount, actual_amount, status, credit_card_id, due_day, decay_per_week, decay_start_date)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13) RETURNING id`,
		e.RecurringItemID, e.CategoryID, e.PeriodYear, e.PeriodMonth, e.Name, e.ItemType,
		e.PlannedAmount, e.ActualAmount, e.Status, e.CreditCardID, e.DueDay, e.DecayPerWeek, e.DecayStartDate,
	).Scan(&id)
	if err != nil {
		return id, err
	}
	if e.RecurringItemID == nil && e.CreditCardID != nil {
		if err := recalculateCardEntry(*e.CreditCardID, e.PeriodYear, e.PeriodMonth); err != nil {
			return id, err
		}
	}
	return id, nil
}

func UpdateEntry(id int64, e Entry) error {
	// If decay is being turned on (or the client omitted the field while it's
	// already running) without a start date, don't silently disable decay --
	// fall back to whatever's already stored, or today if this is genuinely new.
	if e.DecayPerWeek != nil && e.DecayStartDate == nil {
		var existing *time.Time
		_ = database.QueryRow(`SELECT decay_start_date FROM entries WHERE id=$1`, id).Scan(&existing)
		if existing != nil {
			e.DecayStartDate = existing
		} else {
			today := time.Now().Truncate(24 * time.Hour)
			e.DecayStartDate = &today
		}
	}

	var oldRecurringItemID *int64
	var oldCreditCardID *int64
	var periodYear, periodMonth int
	_ = database.QueryRow(`SELECT recurring_item_id, credit_card_id, period_year, period_month FROM entries WHERE id=$1`, id).
		Scan(&oldRecurringItemID, &oldCreditCardID, &periodYear, &periodMonth)

	_, err := database.Exec(`
		UPDATE entries SET
			category_id=$2, name=$3, item_type=$4, planned_amount=$5,
			actual_amount=$6, status=$7, credit_card_id=$8, due_day=$9,
			decay_per_week=$10, decay_start_date=$11
		WHERE id=$1`,
		id, e.CategoryID, e.Name, e.ItemType, e.PlannedAmount, e.ActualAmount, e.Status, e.CreditCardID, e.DueDay,
		e.DecayPerWeek, e.DecayStartDate,
	)
	if err != nil {
		return err
	}

	// One-off entries tagged with a card feed into that card's own repayment
	// entry (see sumPurchasesForPeriod) -- recalculate whichever card(s) this
	// edit affects, old and new, in case the tag was added, removed, or changed.
	if oldRecurringItemID == nil && oldCreditCardID != nil {
		if err := recalculateCardEntry(*oldCreditCardID, periodYear, periodMonth); err != nil {
			return err
		}
	}
	if e.RecurringItemID == nil && e.CreditCardID != nil && (oldCreditCardID == nil || *oldCreditCardID != *e.CreditCardID) {
		if err := recalculateCardEntry(*e.CreditCardID, periodYear, periodMonth); err != nil {
			return err
		}
	}

	// If this entry IS a card's own repayment item, its paid/unpaid status
	// feeds sumUnpaidPriorCardBills for every later period of that same card
	// -- keep them all in sync whenever it changes (e.g. marking it paid).
	if oldRecurringItemID != nil {
		var cardID *int64
		_ = database.QueryRow(`SELECT credit_card_id FROM recurring_items WHERE id=$1`, *oldRecurringItemID).Scan(&cardID)
		if cardID != nil {
			if err := recalculateLaterCardEntries(*cardID, periodYear, periodMonth); err != nil {
				return err
			}
		}
	}
	return nil
}

// recalculateLaterCardEntries recalculates every already-generated entry for
// this card's own repayment item in payment periods strictly after
// (year, month) -- used whenever an earlier period's paid/unpaid status
// changes, since sumUnpaidPriorCardBills for everything after it depends on it.
func recalculateLaterCardEntries(cardID int64, year, month int) error {
	item, found, err := recurringItemForCard(cardID)
	if err != nil || !found {
		return err
	}
	rows, err := database.Query(`
		SELECT DISTINCT period_year, period_month FROM entries
		WHERE recurring_item_id = $1
		AND (period_year > $2 OR (period_year = $2 AND period_month > $3))`,
		item.id, year, month)
	if err != nil {
		return err
	}
	type period struct{ year, month int }
	var periods []period
	for rows.Next() {
		var p period
		if err := rows.Scan(&p.year, &p.month); err != nil {
			rows.Close()
			return err
		}
		periods = append(periods, p)
	}
	rows.Close()
	for _, p := range periods {
		if err := recalculateCardEntry(cardID, p.year, p.month); err != nil {
			return err
		}
	}
	return nil
}

func DeleteEntry(id int64) error {
	var recurringItemID *int64
	var creditCardID *int64
	var periodYear, periodMonth int
	_ = database.QueryRow(`SELECT recurring_item_id, credit_card_id, period_year, period_month FROM entries WHERE id=$1`, id).
		Scan(&recurringItemID, &creditCardID, &periodYear, &periodMonth)

	_, err := database.Exec(`DELETE FROM entries WHERE id=$1`, id)
	if err != nil {
		return err
	}
	if recurringItemID == nil && creditCardID != nil {
		return recalculateCardEntry(*creditCardID, periodYear, periodMonth)
	}
	if recurringItemID != nil {
		var cardID *int64
		_ = database.QueryRow(`SELECT credit_card_id FROM recurring_items WHERE id=$1`, *recurringItemID).Scan(&cardID)
		if cardID != nil {
			return recalculateLaterCardEntries(*cardID, periodYear, periodMonth)
		}
	}
	return nil
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
		SELECT id, category_id, name, item_type, frequency, default_amount, anchor_date, credit_card_id, due_day
		FROM recurring_items
		WHERE active = TRUE
		  AND (
		    (frequency = 'monthly' AND (
		      anchor_date IS NULL
		      OR (EXTRACT(YEAR FROM anchor_date) * 12 + EXTRACT(MONTH FROM anchor_date)) <= ($2 * 12 + $1)
		    ))
		    OR (frequency = 'last_working_day' AND (
		      anchor_date IS NULL
		      OR (EXTRACT(YEAR FROM anchor_date) * 12 + EXTRACT(MONTH FROM anchor_date)) <= ($2 * 12 + $1)
		    ))
		    OR (frequency = 'annual' AND target_month = $1)
		    OR (frequency = 'four_weekly' AND anchor_date IS NOT NULL)
		    OR (frequency = 'three_monthly' AND anchor_date IS NOT NULL)
		  )`, month, year)
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
		anchorDate    *time.Time
		creditCardID  *int64
		dueDay        *int
	}
	var templates []tmpl
	for rows.Next() {
		var t tmpl
		if err := rows.Scan(&t.id, &t.categoryID, &t.name, &t.itemType, &t.frequency, &t.defaultAmount, &t.anchorDate, &t.creditCardID, &t.dueDay); err != nil {
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

		dueDay := t.dueDay
		if t.frequency == "last_working_day" {
			day := lastWorkingDayOfMonth(year, month)
			dueDay = &day
		}
		if t.frequency == "four_weekly" {
			occurrences := fourWeeklyOccurrences(*t.anchorDate, year, month)
			if occurrences == 0 {
				continue // this cycle's 28-day drift means not every month gets one
			}
			amount *= float64(occurrences)
			day := fourWeeklyFirstDay(*t.anchorDate, year, month)
			if day > 0 {
				dueDay = &day
			}
		}
		if t.frequency == "three_monthly" {
			if t.anchorDate == nil || !threeMonthlyFires(*t.anchorDate, year, month) {
				continue
			}
		}

		res, err := database.Exec(`
			INSERT INTO entries (recurring_item_id, category_id, period_year, period_month, name, item_type, planned_amount, credit_card_id, due_day)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)
			ON CONFLICT (recurring_item_id, period_year, period_month) DO NOTHING`,
			t.id, t.categoryID, year, month, t.name, t.itemType, amount, t.creditCardID, dueDay,
		)
		if err != nil {
			return created, err
		}
		if n, _ := res.RowsAffected(); n > 0 {
			created++
		}
		// Backfill due_day on existing entries that were generated without one.
		if dueDay != nil {
			if _, err := database.Exec(`
				UPDATE entries SET due_day = $1
				WHERE recurring_item_id = $2 AND period_year = $3 AND period_month = $4 AND due_day IS NULL`,
				*dueDay, t.id, year, month,
			); err != nil {
				return created, err
			}
		}
	}

	// Also materialize this calendar month's subscription instances. Note
	// (year, month) here is whatever period is currently being touched --
	// when called as a payment period it's "off" from the statement month
	// that actually feeds it, but Forecast/Grid sweep many consecutive
	// months, so every calendar month gets its own call eventually, and
	// each generated purchase recalculates whichever future entry it
	// belongs to regardless of which month triggered its generation.
	if _, err := generateRecurringCardPurchases(year, month); err != nil {
		return created, err
	}

	return created, nil
}

// generateRecurringCardPurchases creates this calendar month's card_purchases
// instance for each active recurring_card_purchases template (idempotent --
// see card_purchases' (recurring_purchase_id, purchase_date) unique
// constraint), then recalculates whichever payment-period entry each new
// instance affects.
func generateRecurringCardPurchases(year, month int) (int, error) {
	rows, err := database.Query(`
		SELECT id, credit_card_id, description, amount, day_of_month
		FROM recurring_card_purchases
		WHERE active = TRUE
		  AND (frequency = 'monthly' OR (frequency = 'annual' AND target_month = $1))`, month)
	if err != nil {
		return 0, err
	}
	defer rows.Close()

	type tmpl struct {
		id           int64
		creditCardID int64
		description  string
		amount       float64
		dayOfMonth   int
	}
	var templates []tmpl
	for rows.Next() {
		var t tmpl
		if err := rows.Scan(&t.id, &t.creditCardID, &t.description, &t.amount, &t.dayOfMonth); err != nil {
			return 0, err
		}
		templates = append(templates, t)
	}

	created := 0
	for _, t := range templates {
		day := t.dayOfMonth
		if lastDay := daysInMonth(year, month); day > lastDay {
			day = lastDay
		}
		date := time.Date(year, time.Month(month), day, 0, 0, 0, 0, time.UTC)

		var newID int64
		err := database.QueryRow(`
			INSERT INTO card_purchases (credit_card_id, description, amount, purchase_date, recurring_purchase_id)
			VALUES ($1,$2,$3,$4,$5)
			ON CONFLICT (recurring_purchase_id, purchase_date) DO NOTHING
			RETURNING id`,
			t.creditCardID, t.description, t.amount, date, t.id,
		).Scan(&newID)
		if err == sql.ErrNoRows {
			continue // already generated for this month
		}
		if err != nil {
			return created, err
		}
		created++

		card, err := getCreditCard(t.creditCardID)
		if err != nil {
			return created, err
		}
		py, pm := paymentPeriodFor(card, date)
		if err := recalculateCardEntry(t.creditCardID, py, pm); err != nil {
			return created, err
		}
	}
	return created, nil
}

func daysInMonth(year, month int) int {
	return time.Date(year, time.Month(month)+1, 0, 0, 0, 0, 0, time.UTC).Day()
}

// threeMonthlyFires returns true when (year, month) is a multiple of 3 months
// from the anchor date's year/month and is on or after it.
func threeMonthlyFires(anchor time.Time, year, month int) bool {
	ay, am := anchor.Year(), int(anchor.Month())
	diff := (year*12 + month) - (ay*12 + am)
	return diff >= 0 && diff%3 == 0
}

// fourWeeklyOccurrences counts how many anchor+28*k days (k=0,1,2,...) land
// within calendar month (year, month). A 28-day cycle is ~13 occurrences a
// year, not 12, so it drifts against calendar months: most months get
// exactly one, occasionally one gets two (when the drift "catches up") or
// none. The loop is bounded -- at most 31 days in a month / 28-day step
// can ever produce more than 2 occurrences, so 4 iterations is generous,
// not a risk of runaway (this codebase has already had one of those).
func fourWeeklyOccurrences(anchor time.Time, year, month int) int {
	monthStart := time.Date(year, time.Month(month), 1, 0, 0, 0, 0, time.UTC)
	monthEnd := monthStart.AddDate(0, 1, 0)

	diffDays := int(monthStart.Sub(anchor).Hours() / 24)
	k := diffDays / 28
	if diffDays%28 != 0 && diffDays > 0 {
		k++ // round up to the first occurrence on/after monthStart
	}
	if k < 0 {
		k = 0
	}

	count := 0
	for i := 0; i < 4; i++ {
		occ := anchor.AddDate(0, 0, 28*(k+i))
		if !occ.Before(monthStart) && occ.Before(monthEnd) {
			count++
		}
		if !occ.Before(monthEnd) {
			break
		}
	}
	return count
}

// fourWeeklyFirstDay returns the day-of-month of the first four_weekly occurrence
// in (year, month), or 0 if there is none.
func fourWeeklyFirstDay(anchor time.Time, year, month int) int {
	monthStart := time.Date(year, time.Month(month), 1, 0, 0, 0, 0, time.UTC)
	monthEnd := monthStart.AddDate(0, 1, 0)

	diffDays := int(monthStart.Sub(anchor).Hours() / 24)
	k := diffDays / 28
	if diffDays%28 != 0 && diffDays > 0 {
		k++
	}
	if k < 0 {
		k = 0
	}

	for i := 0; i < 4; i++ {
		occ := anchor.AddDate(0, 0, 28*(k+i))
		if occ.Before(monthEnd) && !occ.Before(monthStart) {
			return occ.Day()
		}
		if !occ.Before(monthEnd) {
			break
		}
	}
	return 0
}

// lastWorkingDayOfMonth returns the last Monday-Friday of the given month.
func lastWorkingDayOfMonth(year, month int) int {
	last := time.Date(year, time.Month(month)+1, 0, 0, 0, 0, 0, time.UTC) // last day of month
	for last.Weekday() == time.Saturday || last.Weekday() == time.Sunday {
		last = last.AddDate(0, 0, -1)
	}
	return last.Day()
}

// ---- Recurring card purchases (subscription templates) ----

func GetRecurringCardPurchases(cardID int64) ([]RecurringCardPurchase, error) {
	rows, err := database.Query(`
		SELECT id, credit_card_id, description, amount, frequency, day_of_month, target_month, active
		FROM recurring_card_purchases WHERE credit_card_id = $1 ORDER BY description`, cardID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []RecurringCardPurchase
	for rows.Next() {
		var r RecurringCardPurchase
		if err := rows.Scan(&r.ID, &r.CreditCardID, &r.Description, &r.Amount, &r.Frequency, &r.DayOfMonth, &r.TargetMonth, &r.Active); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, nil
}

func AddRecurringCardPurchase(r RecurringCardPurchase) (int64, error) {
	var id int64
	err := database.QueryRow(`
		INSERT INTO recurring_card_purchases (credit_card_id, description, amount, frequency, day_of_month, target_month, active)
		VALUES ($1,$2,$3,$4,$5,$6,$7) RETURNING id`,
		r.CreditCardID, r.Description, r.Amount, r.Frequency, r.DayOfMonth, r.TargetMonth, r.Active,
	).Scan(&id)
	return id, err
}

func UpdateRecurringCardPurchase(id int64, r RecurringCardPurchase) error {
	_, err := database.Exec(`
		UPDATE recurring_card_purchases SET
			description=$2, amount=$3, frequency=$4, day_of_month=$5, target_month=$6, active=$7
		WHERE id=$1`,
		id, r.Description, r.Amount, r.Frequency, r.DayOfMonth, r.TargetMonth, r.Active,
	)
	return err
}

func DeleteRecurringCardPurchase(id int64) error {
	_, err := database.Exec(`DELETE FROM recurring_card_purchases WHERE id=$1`, id)
	return err
}

// ---- Card checkpoints ----

type CardCheckpoint struct {
	ID           int64   `json:"id"`
	CreditCardID int64   `json:"credit_card_id"`
	PeriodYear   int     `json:"period_year"`
	PeriodMonth  int     `json:"period_month"`
	PeriodDay    int     `json:"period_day"`
	Balance      float64 `json:"balance"`
}

func GetCardCheckpoints(creditCardID int64) ([]CardCheckpoint, error) {
	rows, err := database.Query(`
		SELECT id, credit_card_id, period_year, period_month, period_day, balance
		FROM card_checkpoints WHERE credit_card_id = $1
		ORDER BY period_year, period_month, period_day`, creditCardID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []CardCheckpoint
	for rows.Next() {
		var c CardCheckpoint
		if err := rows.Scan(&c.ID, &c.CreditCardID, &c.PeriodYear, &c.PeriodMonth, &c.PeriodDay, &c.Balance); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, nil
}

// AddCardCheckpoint records (or replaces) a checkpoint, then recalculates
// whichever payment-period entry it now anchors -- otherwise the checkpoint
// would sit unused until some unrelated purchase edit happened to trigger a
// recalculation of that period.
// AddCardCheckpoint records (or replaces) a checkpoint, recalculates the
// payment period it now anchors, and also returns any card-tagged one-off
// entries (e.g. a decaying sundries buffer) already sitting in that same
// period -- likely redundant now that a fresh, verified balance covers it,
// so the caller can offer to remove them instead of them being silently
// forgotten and double-counted (this has bitten the user twice already).
func AddCardCheckpoint(creditCardID int64, year, month, day int, balance float64) (int64, []Entry, error) {
	var id int64
	err := database.QueryRow(`
		INSERT INTO card_checkpoints (credit_card_id, period_year, period_month, period_day, balance)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (credit_card_id, period_year, period_month, period_day) DO UPDATE SET balance = $5
		RETURNING id`,
		creditCardID, year, month, day, balance,
	).Scan(&id)
	if err != nil {
		return id, nil, err
	}
	if err := recalculateCheckpointPeriod(creditCardID, year, month, day); err != nil {
		return id, nil, err
	}

	card, err := getCreditCard(creditCardID)
	if err != nil {
		return id, nil, err
	}
	checkpointDate := time.Date(year, time.Month(month), day, 0, 0, 0, 0, time.UTC)
	py, pm := paymentPeriodFor(card, checkpointDate)
	oneOffs, err := GetCardTaggedOneOffs(creditCardID, py, pm)
	if err != nil {
		return id, nil, err
	}
	return id, oneOffs, nil
}

// GetCardTaggedOneOffs returns one-off entries (recurring_item_id IS NULL)
// tagged with this card for the given payment period.
func GetCardTaggedOneOffs(cardID int64, year, month int) ([]Entry, error) {
	rows, err := database.Query(`
		SELECT id, recurring_item_id, category_id, period_year, period_month,
		       name, item_type, planned_amount, actual_amount, status, credit_card_id, due_day,
		       decay_per_week, decay_start_date
		FROM entries WHERE credit_card_id = $1 AND recurring_item_id IS NULL
		AND period_year = $2 AND period_month = $3
		ORDER BY due_day NULLS LAST, id`, cardID, year, month)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Entry
	for rows.Next() {
		var e Entry
		if err := rows.Scan(&e.ID, &e.RecurringItemID, &e.CategoryID, &e.PeriodYear, &e.PeriodMonth,
			&e.Name, &e.ItemType, &e.PlannedAmount, &e.ActualAmount, &e.Status, &e.CreditCardID, &e.DueDay,
			&e.DecayPerWeek, &e.DecayStartDate); err != nil {
			return nil, err
		}
		e.EffectiveAmount = effectiveEntryAmount(e.PlannedAmount, e.ActualAmount, e.DecayPerWeek, e.DecayStartDate)
		out = append(out, e)
	}
	return out, nil
}

// DeleteCardCheckpoint removes a checkpoint, then recalculates the payment
// period it used to anchor so that period falls back to summing its
// purchases (or an earlier/no checkpoint) instead of keeping the stale total.
func DeleteCardCheckpoint(id int64) error {
	var creditCardID int64
	var year, month, day int
	err := database.QueryRow(`
		SELECT credit_card_id, period_year, period_month, period_day FROM card_checkpoints WHERE id=$1`, id,
	).Scan(&creditCardID, &year, &month, &day)
	if err != nil {
		return err
	}
	if _, err := database.Exec(`DELETE FROM card_checkpoints WHERE id=$1`, id); err != nil {
		return err
	}
	return recalculateCheckpointPeriod(creditCardID, year, month, day)
}

// recalculateCheckpointPeriod recalculates the payment-period entry that a
// checkpoint dated (year, month, day) falls into, using the same
// paymentPeriodFor rule applied to purchases.
func recalculateCheckpointPeriod(creditCardID int64, year, month, day int) error {
	card, err := getCreditCard(creditCardID)
	if err != nil {
		return err
	}
	checkpointDate := time.Date(year, time.Month(month), day, 0, 0, 0, 0, time.UTC)
	py, pm := paymentPeriodFor(card, checkpointDate)
	return recalculateCardEntry(creditCardID, py, pm)
}

// ---- Balance checkpoints ----

func GetCheckpoints() ([]BalanceCheckpoint, error) {
	rows, err := database.Query(`
		SELECT id, period_year, period_month, period_day, balance FROM balance_checkpoints
		ORDER BY period_year, period_month, period_day`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []BalanceCheckpoint
	for rows.Next() {
		var c BalanceCheckpoint
		if err := rows.Scan(&c.ID, &c.PeriodYear, &c.PeriodMonth, &c.PeriodDay, &c.Balance); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, nil
}

// AddCheckpoint records (or replaces) the known balance for a specific day,
// e.g. after checking the real bank app mid-month.
func AddCheckpoint(year, month, day int, balance float64) (int64, error) {
	var id int64
	err := database.QueryRow(`
		INSERT INTO balance_checkpoints (period_year, period_month, period_day, balance)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (period_year, period_month, period_day) DO UPDATE SET balance = $4
		RETURNING id`,
		year, month, day, balance,
	).Scan(&id)
	return id, err
}

// latestCheckpointAtOrBefore finds the most recent checkpoint at or before
// (year, month) to use as the forecast's starting point. Any checkpoint
// within the requested month qualifies (day is ignored for month-level forecasts).
func latestCheckpointAtOrBefore(year, month int) (BalanceCheckpoint, bool, error) {
	var c BalanceCheckpoint
	err := database.QueryRow(`
		SELECT id, period_year, period_month, period_day, balance FROM balance_checkpoints
		WHERE period_year < $1 OR (period_year = $1 AND period_month <= $2)
		ORDER BY period_year DESC, period_month DESC, period_day DESC
		LIMIT 1`, year, month,
	).Scan(&c.ID, &c.PeriodYear, &c.PeriodMonth, &c.PeriodDay, &c.Balance)
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
	return periodNetFrom(year, month, 0)
}

// periodNetFrom is like periodNet but only counts entries with due_day >= fromDay
// (entries with no due_day are always counted). Used to avoid double-counting
// pre-checkpoint entries when the checkpoint falls within the month.
func periodNetFrom(year, month, fromDay int) (income, expense, savings float64, err error) {
	if _, err := GeneratePeriodEntries(year, month); err != nil {
		return 0, 0, 0, err
	}

	// One-off entries tagged with a credit card (recurring_item_id IS NULL,
	// credit_card_id set) are folded into that card's own repayment entry by
	// sumPurchasesForPeriod instead -- excluded here so they aren't also
	// counted as an independent line.
	var rows *sql.Rows
	if fromDay <= 1 {
		rows, err = database.Query(`
			SELECT item_type, planned_amount, actual_amount, decay_per_week, decay_start_date
			FROM entries WHERE period_year=$1 AND period_month=$2
			AND (credit_card_id IS NULL OR recurring_item_id IS NOT NULL)`, year, month)
	} else {
		// Exclude entries already incurred on the checkpoint day — they are baked
		// into the checkpoint balance and must not be counted again.
		rows, err = database.Query(`
			SELECT item_type, planned_amount, actual_amount, decay_per_week, decay_start_date
			FROM entries WHERE period_year=$1 AND period_month=$2
			AND (credit_card_id IS NULL OR recurring_item_id IS NOT NULL)
			AND (
				due_day IS NULL
				OR due_day > $3
				OR (due_day = $3 AND (status IS NULL OR status != 'incurred'))
			)`, year, month, fromDay)
	}
	if err != nil {
		return 0, 0, 0, err
	}
	defer rows.Close()
	for rows.Next() {
		var itemType string
		var plannedAmount float64
		var actualAmount *float64
		var decayPerWeek *float64
		var decayStartDate *time.Time
		if err := rows.Scan(&itemType, &plannedAmount, &actualAmount, &decayPerWeek, &decayStartDate); err != nil {
			return 0, 0, 0, err
		}
		amount := effectiveEntryAmount(plannedAmount, actualAmount, decayPerWeek, decayStartDate)
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
		var inc, exp, sav float64
		if y == checkpoint.PeriodYear && m == checkpoint.PeriodMonth {
			inc, exp, sav, err = periodNetFrom(y, m, checkpoint.PeriodDay)
		} else {
			inc, exp, sav, err = periodNet(y, m)
		}
		if err != nil {
			return ForecastSummary{}, err
		}
		balance += inc - exp - sav
		y, m = nextPeriod(y, m)
	}

	// Display totals use the full month; carried-forward only counts post-checkpoint entries.
	inc, exp, sav, err := periodNet(year, month)
	if err != nil {
		return ForecastSummary{}, err
	}
	cfInc, cfExp, cfSav := inc, exp, sav
	if year == checkpoint.PeriodYear && month == checkpoint.PeriodMonth {
		cfInc, cfExp, cfSav, err = periodNetFrom(year, month, checkpoint.PeriodDay)
		if err != nil {
			return ForecastSummary{}, err
		}
	}
	return ForecastSummary{
		PeriodYear:     year,
		PeriodMonth:    month,
		BroughtForward: balance,
		Income:         inc,
		Expense:        exp,
		Savings:        sav,
		CarriedForward: balance + cfInc - cfExp - cfSav,
	}, nil
}

type ForecastDanger struct {
	PeriodYear     int     `json:"period_year"`
	PeriodMonth    int     `json:"period_month"`
	BroughtForward float64 `json:"brought_forward"`
	MinBalance     float64 `json:"min_balance"`      // lowest intra-month running balance
	MinBalanceDay  int     `json:"min_balance_day"`  // day of month when minimum occurs
	CarriedForward float64 `json:"carried_forward"`
}

// periodMinBalance walks entries in (year, month) from startBalance and
// returns the minimum running balance reached plus the day it first occurs.
// Pass fromDay=0 to include all entries. Entries with no due_day are treated
// as day 0 (counted first). When fromDay is a checkpoint day, an entry due
// exactly on it that's already incurred is skipped -- already reflected in
// startBalance -- matching periodNetFrom.
//
// trackMinFromDay (>= fromDay) lets the minimum-tracking window start later
// than the summation itself -- e.g. "today", so a dip that already happened
// and was survived doesn't get reported as still-upcoming danger. Every
// entry from fromDay onward still gets summed either way, so carried (the
// period's true closing balance, chained into next period's brought-forward)
// is unaffected by where tracking starts.
func periodMinBalance(year, month, fromDay, trackMinFromDay int, startBalance float64) (minBalance float64, minDay int, carried float64, err error) {
	if _, err = GeneratePeriodEntries(year, month); err != nil {
		return
	}
	// One-off entries tagged with a credit card are folded into that card's
	// own repayment entry by sumPurchasesForPeriod instead -- excluded here,
	// same as periodNetFrom.
	var rows *sql.Rows
	if fromDay <= 1 {
		rows, err = database.Query(`
			SELECT item_type, planned_amount, actual_amount, decay_per_week, decay_start_date, COALESCE(due_day, 0)
			FROM entries WHERE period_year=$1 AND period_month=$2
			AND (credit_card_id IS NULL OR recurring_item_id IS NOT NULL)
			ORDER BY COALESCE(due_day, 0)`, year, month)
	} else {
		// Exclude entries already incurred on the checkpoint day — they are baked
		// into startBalance (the checkpoint balance) and must not be counted again.
		// Mirrors periodNetFrom's exclusion rule exactly.
		rows, err = database.Query(`
			SELECT item_type, planned_amount, actual_amount, decay_per_week, decay_start_date, COALESCE(due_day, 0)
			FROM entries WHERE period_year=$1 AND period_month=$2
			AND (credit_card_id IS NULL OR recurring_item_id IS NOT NULL)
			AND (
				due_day IS NULL
				OR due_day > $3
				OR (due_day = $3 AND (status IS NULL OR status != 'incurred'))
			)
			ORDER BY COALESCE(due_day, 0)`, year, month, fromDay)
	}
	if err != nil {
		return
	}
	defer rows.Close()
	balance := startBalance
	tracking := trackMinFromDay <= fromDay
	if tracking {
		minBalance = startBalance
	}
	minDay = 0
	for rows.Next() {
		var itemType string
		var plannedAmount float64
		var actualAmount *float64
		var decayPerWeek *float64
		var decayStartDate *time.Time
		var day int
		if rows.Scan(&itemType, &plannedAmount, &actualAmount, &decayPerWeek, &decayStartDate, &day) != nil {
			continue
		}
		amount := effectiveEntryAmount(plannedAmount, actualAmount, decayPerWeek, decayStartDate)
		if itemType == "income" {
			balance += amount
		} else {
			balance -= amount
		}
		if !tracking && day >= trackMinFromDay {
			tracking = true
			minBalance = balance
			minDay = day
		}
		if tracking && balance < minBalance {
			minBalance = balance
			minDay = day
		}
	}
	if !tracking {
		// Nothing left at/after trackMinFromDay -- balance won't dip further this period.
		minBalance = balance
		minDay = trackMinFromDay
	}
	carried = balance
	return
}

// ForecastDangerRange computes the intra-month minimum balance for each of
// the next `count` months starting at (fromYear, fromMonth).
func ForecastDangerRange(fromYear, fromMonth, count int) ([]ForecastDanger, error) {
	checkpoint, found, err := latestCheckpointAtOrBefore(fromYear, fromMonth)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, errors.New("no balance checkpoint at or before this period -- add one via POST /checkpoints")
	}

	// Walk forward from the checkpoint to the start of the requested range.
	balance := checkpoint.Balance
	y, m := checkpoint.PeriodYear, checkpoint.PeriodMonth
	for y < fromYear || (y == fromYear && m < fromMonth) {
		var inc, exp, sav float64
		if y == checkpoint.PeriodYear && m == checkpoint.PeriodMonth {
			inc, exp, sav, err = periodNetFrom(y, m, checkpoint.PeriodDay)
		} else {
			inc, exp, sav, err = periodNet(y, m)
		}
		if err != nil {
			return nil, err
		}
		balance += inc - exp - sav
		y, m = nextPeriod(y, m)
	}

	now := time.Now()
	todayYear, todayMonth, todayDay := now.Year(), int(now.Month()), now.Day()

	out := make([]ForecastDanger, 0, count)
	for i := 0; i < count; i++ {
		bf := balance
		// For the checkpoint's own month, start from the checkpoint balance and
		// only walk entries from the checkpoint day forward — past entries are
		// already reflected in the checkpoint balance.
		fromDay := 0
		startBal := balance
		if y == checkpoint.PeriodYear && m == checkpoint.PeriodMonth {
			fromDay = checkpoint.PeriodDay
			startBal = checkpoint.Balance
		}
		// For today's own month, don't report a dip that already happened and
		// was survived -- only track the minimum from today onward.
		trackMinFromDay := fromDay
		if y == todayYear && m == todayMonth && todayDay > trackMinFromDay {
			trackMinFromDay = todayDay
		}
		minBal, minDay, carried, err := periodMinBalance(y, m, fromDay, trackMinFromDay, startBal)
		if err != nil {
			return nil, err
		}
		out = append(out, ForecastDanger{
			PeriodYear: y, PeriodMonth: m,
			BroughtForward: bf,
			MinBalance:     minBal,
			MinBalanceDay:  minDay,
			CarriedForward: carried,
		})
		balance = carried
		y, m = nextPeriod(y, m)
	}
	return out, nil
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
		var inc, exp, sav float64
		if y == checkpoint.PeriodYear && m == checkpoint.PeriodMonth {
			inc, exp, sav, err = periodNetFrom(y, m, checkpoint.PeriodDay)
		} else {
			inc, exp, sav, err = periodNet(y, m)
		}
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
		cfInc, cfExp, cfSav := inc, exp, sav
		if y == checkpoint.PeriodYear && m == checkpoint.PeriodMonth {
			cfInc, cfExp, cfSav, err = periodNetFrom(y, m, checkpoint.PeriodDay)
			if err != nil {
				return nil, err
			}
		}
		carried := balance + cfInc - cfExp - cfSav
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
