package db

import (
	"database/sql"
	"errors"
	"fmt"
	"math"
	"sort"
	"time"
)

// ---- Present occasions ----
//
// A present occasion is a yearly gift allowance -- Christmas, or a cluster
// of birthdays -- on a card (see migration 023). Card purchases in the
// "Presents" category count toward the next occasion on or after their
// date, whichever card they're on. What's left of the allowance is held
// back on the occasion's card bills, spread evenly by day from today (or
// the day after the previous occasion, if later) up to the occasion
// itself, so the bills the shopping will actually land on carry it. The
// spending itself is already on those bills as purchases, which is why
// only the unspent part is held back.
//
// Each held-back entry keeps its full amount until the day after its last
// day and then drops to zero, so an allowance left unspent is released
// once the occasion has gone by. The entries are rebuilt from scratch
// (refreshPresents) whenever an occasion or a Presents purchase changes,
// and whenever the occasions are listed, so the spread follows today.

const presentsCategoryName = "Presents"

type PresentOccasion struct {
	ID           int64   `json:"id"`
	CreditCardID int64   `json:"credit_card_id"`
	Name         string  `json:"name"`
	Month        int     `json:"month"`
	Day          int     `json:"day"`
	Allowance    float64 `json:"allowance"`
	// Optional: when shopping for it starts each year. What's left is held
	// back from then (or today) rather than from the previous occasion.
	ShopStartMonth *int `json:"shop_start_month"`
	ShopStartDay   *int `json:"shop_start_day"`
	// Computed at read time for the next occurrence (on or after today).
	NextDate  string  `json:"next_date"` // "YYYY-MM-DD"
	Spent     float64 `json:"spent"`
	Remaining float64 `json:"remaining"` // Allowance - Spent, floored at zero
	Overspent bool    `json:"overspent"` // Spent > Allowance
}

// occasionDate is the occasion's date in a given year, clamped to the
// month's length (a 29 Feb birthday falls on the 28th in other years).
func occasionDate(month, day, year int) time.Time {
	if last := daysInMonth(year, month); day > last {
		day = last
	}
	return time.Date(year, time.Month(month), day, 0, 0, 0, 0, time.UTC)
}

type occurrence struct {
	occasion PresentOccasion
	date     time.Time
	prev     time.Time // the previous occurrence of any occasion; purchases after it and up to date count here
}

// presentOccurrences lists every occasion's dates from the year before
// `from` to two years after it, oldest first, each with the date of the
// occurrence (of any occasion) just before it. Two occasions on the same
// day share the purchases between them and the one before -- the earlier
// id gets them (see spentByOccurrence).
func presentOccurrences(occasions []PresentOccasion, from time.Time) []occurrence {
	var out []occurrence
	for y := from.Year() - 1; y <= from.Year()+2; y++ {
		for _, o := range occasions {
			out = append(out, occurrence{occasion: o, date: occasionDate(o.Month, o.Day, y)})
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if !out[i].date.Equal(out[j].date) {
			return out[i].date.Before(out[j].date)
		}
		return out[i].occasion.ID < out[j].occasion.ID
	})
	for i := range out {
		if i == 0 {
			out[i].prev = out[i].date.AddDate(-1, 0, 0)
		} else {
			out[i].prev = out[i-1].date
		}
	}
	return out
}

// shopStart is when shopping for the occurrence on date starts: the last
// shop_start day/month on or before it (so a 1 Dec start for a 20 Jan
// occasion is the December before). ok is false if none is set.
func shopStart(o PresentOccasion, date time.Time) (time.Time, bool) {
	if o.ShopStartMonth == nil || o.ShopStartDay == nil {
		return time.Time{}, false
	}
	start := occasionDate(*o.ShopStartMonth, *o.ShopStartDay, date.Year())
	if start.After(date) {
		start = occasionDate(*o.ShopStartMonth, *o.ShopStartDay, date.Year()-1)
	}
	return start, true
}

type presentPurchase struct {
	amount float64
	date   time.Time
}

// spentFor totals the purchases in (occ.prev, occ.date].
func spentFor(occ occurrence, purchases []presentPurchase) float64 {
	var total float64
	for _, p := range purchases {
		if p.date.After(occ.prev) && !p.date.After(occ.date) {
			total += p.amount
		}
	}
	return math.Round(total*100) / 100
}

func presentsCategoryID() (id int64, found bool, err error) {
	err = database.QueryRow(`SELECT id FROM categories WHERE name=$1 ORDER BY id LIMIT 1`, presentsCategoryName).Scan(&id)
	if err == sql.ErrNoRows {
		return 0, false, nil
	}
	return id, err == nil, err
}

func presentPurchases() ([]presentPurchase, error) {
	catID, found, err := presentsCategoryID()
	if err != nil || !found {
		return nil, err
	}
	rows, err := database.Query(`SELECT amount, purchase_date FROM card_purchases WHERE category_id=$1`, catID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []presentPurchase
	for rows.Next() {
		var p presentPurchase
		if err := rows.Scan(&p.amount, &p.date); err != nil {
			return nil, err
		}
		p.date = time.Date(p.date.Year(), p.date.Month(), p.date.Day(), 0, 0, 0, 0, time.UTC)
		out = append(out, p)
	}
	return out, rows.Err()
}

func loadPresentOccasions() ([]PresentOccasion, error) {
	rows, err := database.Query(`
		SELECT id, credit_card_id, name, month, day, allowance, shop_start_month, shop_start_day FROM present_occasions ORDER BY month, day, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PresentOccasion
	for rows.Next() {
		var o PresentOccasion
		if err := rows.Scan(&o.ID, &o.CreditCardID, &o.Name, &o.Month, &o.Day, &o.Allowance, &o.ShopStartMonth, &o.ShopStartDay); err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

func presentsToday() time.Time {
	now := time.Now()
	return time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
}

// GetPresentOccasions lists the occasions with what's been spent toward
// each one's next occurrence, refreshing their held-back entries first.
func GetPresentOccasions() ([]PresentOccasion, error) {
	if err := refreshPresents(); err != nil {
		return nil, err
	}
	occasions, err := loadPresentOccasions()
	if err != nil {
		return nil, err
	}
	purchases, err := presentPurchases()
	if err != nil {
		return nil, err
	}
	now := presentsToday()
	occs := presentOccurrences(occasions, now)
	for i := range occasions {
		for _, occ := range occs {
			if occ.occasion.ID != occasions[i].ID || occ.date.Before(now) {
				continue
			}
			spent := spentFor(occ, purchases)
			occasions[i].NextDate = occ.date.Format(isoDate)
			occasions[i].Spent = spent
			occasions[i].Remaining = math.Max(0, occasions[i].Allowance-spent)
			occasions[i].Overspent = spent > occasions[i].Allowance+0.005
			break
		}
	}
	sort.SliceStable(occasions, func(i, j int) bool { return occasions[i].NextDate < occasions[j].NextDate })
	return occasions, nil
}

func validatePresentOccasion(o PresentOccasion) error {
	if o.Name == "" {
		return errors.New("name required")
	}
	if o.Month < 1 || o.Month > 12 {
		return errors.New("month must be 1-12")
	}
	if o.Day < 1 || o.Day > daysInMonth(2028, o.Month) { // 2028: a leap year, so 29 Feb is allowed
		return errors.New("day is out of range for that month")
	}
	if o.Allowance <= 0 {
		return errors.New("allowance must be more than zero")
	}
	if (o.ShopStartMonth == nil) != (o.ShopStartDay == nil) {
		return errors.New("shopping start needs both a day and a month, or neither")
	}
	if o.ShopStartMonth != nil {
		if *o.ShopStartMonth < 1 || *o.ShopStartMonth > 12 {
			return errors.New("shop_start_month must be 1-12")
		}
		if *o.ShopStartDay < 1 || *o.ShopStartDay > daysInMonth(2028, *o.ShopStartMonth) {
			return errors.New("shopping start day is out of range for that month")
		}
	}
	card, err := getCreditCard(o.CreditCardID)
	if err != nil {
		return fmt.Errorf("unknown card: %w", err)
	}
	if _, found, err := recurringItemForCard(card.ID); err != nil {
		return err
	} else if !found {
		return fmt.Errorf("%s has no monthly repayment item to hold the allowance on", card.Name)
	}
	return nil
}

func AddPresentOccasion(o PresentOccasion) (int64, error) {
	if err := validatePresentOccasion(o); err != nil {
		return 0, err
	}
	var id int64
	err := database.QueryRow(`
		INSERT INTO present_occasions (credit_card_id, name, month, day, allowance, shop_start_month, shop_start_day)
		VALUES ($1,$2,$3,$4,$5,$6,$7) RETURNING id`,
		o.CreditCardID, o.Name, o.Month, o.Day, o.Allowance, o.ShopStartMonth, o.ShopStartDay,
	).Scan(&id)
	if err != nil {
		return 0, err
	}
	return id, refreshPresents()
}

func UpdatePresentOccasion(id int64, o PresentOccasion) error {
	if err := validatePresentOccasion(o); err != nil {
		return err
	}
	res, err := database.Exec(`
		UPDATE present_occasions SET credit_card_id=$2, name=$3, month=$4, day=$5, allowance=$6,
		       shop_start_month=$7, shop_start_day=$8 WHERE id=$1`,
		id, o.CreditCardID, o.Name, o.Month, o.Day, o.Allowance, o.ShopStartMonth, o.ShopStartDay)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return sql.ErrNoRows
	}
	return refreshPresents()
}

func DeletePresentOccasion(id int64) error {
	// Its entries go with it (ON DELETE CASCADE), but refreshPresents needs
	// to see them first to know which bills to recalculate.
	old, err := presentEntryPeriods()
	if err != nil {
		return err
	}
	if _, err := database.Exec(`DELETE FROM present_occasions WHERE id=$1`, id); err != nil {
		return err
	}
	if err := refreshPresents(); err != nil {
		return err
	}
	return recalculateHolidayPeriods(old)
}

func presentEntryPeriods() ([]cardPeriod, error) {
	rows, err := database.Query(`
		SELECT DISTINCT credit_card_id, period_year, period_month FROM entries WHERE present_occasion_id IS NOT NULL`)
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

type presentHold struct {
	cardID      int64
	occasionID  int64
	name        string
	year, month int // payment period
	first, last time.Time
	amount      float64
}

// presentHolds works out the held-back entries: for each occasion's next
// two occurrences, what's left of its allowance spread by day over
// max(today, the day after the previous occasion) .. the occasion date,
// split by payment period. Pure, for testing.
func presentHolds(occasions []PresentOccasion, cards map[int64]CreditCard, purchases []presentPurchase, now time.Time) []presentHold {
	var out []presentHold
	taken := map[int64]int{}
	for _, occ := range presentOccurrences(occasions, now) {
		if occ.date.Before(now) || taken[occ.occasion.ID] >= 2 {
			continue
		}
		taken[occ.occasion.ID]++
		remaining := occ.occasion.Allowance - spentFor(occ, purchases)
		if remaining <= 0 {
			continue
		}
		start := occ.prev.AddDate(0, 0, 1)
		if shop, ok := shopStart(occ.occasion, occ.date); ok && shop.After(start) {
			start = shop
		}
		if start.Before(now) {
			start = now
		}
		card := cards[occ.occasion.CreditCardID]
		segs := holidaySegments(card, start, occ.date)
		totalDays := int(occ.date.Sub(start).Hours()/24) + 1
		cents := int64(math.Round(remaining * 100))
		var allotted int64
		for i, seg := range segs {
			days := int(seg.last.Sub(seg.first).Hours()/24) + 1
			share := cents * int64(days) / int64(totalDays)
			if i == len(segs)-1 {
				share = cents - allotted
			}
			allotted += share
			if share <= 0 {
				continue
			}
			out = append(out, presentHold{
				cardID: card.ID, occasionID: occ.occasion.ID,
				name: fmt.Sprintf("%s presents %s", occ.occasion.Name, dayRange(seg.first, seg.last)),
				year: seg.year, month: seg.month, first: seg.first, last: seg.last,
				amount: float64(share) / 100,
			})
		}
	}
	return out
}

// refreshPresents rebuilds every occasion's held-back entries and
// recalculates the bills on both sides of the change.
func refreshPresents() error {
	occasions, err := loadPresentOccasions()
	if err != nil {
		return err
	}
	purchases, err := presentPurchases()
	if err != nil {
		return err
	}
	cards := map[int64]CreditCard{}
	categoryOf := map[int64]int64{}
	for _, o := range occasions {
		if _, ok := cards[o.CreditCardID]; ok {
			continue
		}
		card, err := getCreditCard(o.CreditCardID)
		if err != nil {
			return err
		}
		item, found, err := recurringItemForCard(card.ID)
		if err != nil {
			return err
		}
		if !found {
			return fmt.Errorf("%s has no monthly repayment item to hold the allowance on", card.Name)
		}
		cards[card.ID] = card
		categoryOf[card.ID] = item.categoryID
	}

	affected, err := presentEntryPeriods()
	if err != nil {
		return err
	}
	if _, err := database.Exec(`DELETE FROM entries WHERE present_occasion_id IS NOT NULL`); err != nil {
		return err
	}
	for _, h := range presentHolds(occasions, cards, purchases, presentsToday()) {
		if _, err := database.Exec(`
			INSERT INTO entries (category_id, period_year, period_month, name, item_type, planned_amount,
			                     credit_card_id, decay_per_day, decay_start_date, decay_end_date, present_occasion_id)
			VALUES ($1,$2,$3,$4,'expense',$5,$6,$5,$7,$8,$9)`,
			categoryOf[h.cardID], h.year, h.month, h.name, h.amount,
			h.cardID, h.first, h.last.AddDate(0, 0, 1), h.occasionID,
		); err != nil {
			return err
		}
		affected = append(affected, cardPeriod{h.cardID, h.year, h.month})
	}
	return recalculateHolidayPeriods(affected)
}

// refreshPresentsIfCategory rebuilds the held-back entries when a purchase
// in the Presents category was added, changed or removed.
func refreshPresentsIfCategory(categoryIDs ...*int64) error {
	catID, found, err := presentsCategoryID()
	if err != nil || !found {
		return err
	}
	for _, c := range categoryIDs {
		if c != nil && *c == catID {
			return refreshPresents()
		}
	}
	return nil
}
