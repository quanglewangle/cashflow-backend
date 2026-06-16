// Cashflow forecast REST API.
//
// Deployed on fimblefowl.co.uk the same way as qrzlook: a static Go binary
// run as a systemd --user service, with nginx reverse-proxying /cashflow/
// to this process (see nginx.conf.snippet). Talks to its own "cashflow"
// Postgres database (separate from qrzlook's "sites" database).
package main

import (
	"encoding/json"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/quanglewangle/cashflow/db"
)

var writeToken string
var buildHash = "dev"

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// okToWrite mirrors qrzlook: if no write token is configured, everything is
// open (fine for a single-user home server); otherwise mutating requests
// need X-Write-Token to match.
func okToWrite(w http.ResponseWriter, r *http.Request) bool {
	if writeToken == "" || r.Header.Get("X-Write-Token") == writeToken {
		return true
	}
	writeError(w, http.StatusUnauthorized, "unauthorised")
	return false
}

// idFromPath extracts the trailing /{id} segment, e.g. "/entries/42" -> 42.
func idFromPath(path string) (int64, error) {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	return strconv.ParseInt(parts[len(parts)-1], 10, 64)
}

func intQueryParam(r *http.Request, name string) (int, bool) {
	v := r.URL.Query().Get(name)
	if v == "" {
		return 0, false
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return 0, false
	}
	return n, true
}

func main() {
	port := os.Getenv("CASHFLOW_PORT")
	if port == "" {
		port = "8092"
	}
	writeToken = os.Getenv("CASHFLOW_WRITE_TOKEN")

	db.OpenDatabase()

	http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "build": buildHash})
	})

	http.HandleFunc("/categories", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			cats, err := db.GetCategories()
			if err != nil {
				writeError(w, http.StatusInternalServerError, err.Error())
				return
			}
			writeJSON(w, http.StatusOK, cats)
		case http.MethodPost:
			if !okToWrite(w, r) {
				return
			}
			var c db.Category
			if err := json.NewDecoder(r.Body).Decode(&c); err != nil {
				writeError(w, http.StatusBadRequest, "invalid JSON")
				return
			}
			id, err := db.AddCategory(c)
			if err != nil {
				writeError(w, http.StatusInternalServerError, err.Error())
				return
			}
			writeJSON(w, http.StatusCreated, map[string]int64{"id": id})
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	})

	http.HandleFunc("/credit-cards", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			cards, err := db.GetCreditCards()
			if err != nil {
				writeError(w, http.StatusInternalServerError, err.Error())
				return
			}
			writeJSON(w, http.StatusOK, cards)
		case http.MethodPost:
			if !okToWrite(w, r) {
				return
			}
			var c db.CreditCard
			if err := json.NewDecoder(r.Body).Decode(&c); err != nil {
				writeError(w, http.StatusBadRequest, "invalid JSON")
				return
			}
			id, err := db.AddCreditCard(c)
			if err != nil {
				writeError(w, http.StatusInternalServerError, err.Error())
				return
			}
			writeJSON(w, http.StatusCreated, map[string]int64{"id": id})
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	})

	http.HandleFunc("/credit-cards/", func(w http.ResponseWriter, r *http.Request) {
		id, err := idFromPath(r.URL.Path)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid id")
			return
		}
		switch r.Method {
		case http.MethodPut:
			if !okToWrite(w, r) {
				return
			}
			var c db.CreditCard
			if err := json.NewDecoder(r.Body).Decode(&c); err != nil {
				writeError(w, http.StatusBadRequest, "invalid JSON")
				return
			}
			if err := db.UpdateCreditCard(id, c); err != nil {
				writeError(w, http.StatusInternalServerError, err.Error())
				return
			}
			writeJSON(w, http.StatusOK, map[string]string{"status": "updated"})
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	})

	// GET /card-purchases?credit_card_id=N lists a card's purchases.
	// POST /card-purchases adds one (date as "YYYY-MM-DD") and recalculates
	// the affected month's card payment entry from the running total.
	http.HandleFunc("/card-purchases", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			cardID, ok := intQueryParam(r, "credit_card_id")
			if !ok {
				writeError(w, http.StatusBadRequest, "credit_card_id required")
				return
			}
			purchases, err := db.GetCardPurchases(int64(cardID))
			if err != nil {
				writeError(w, http.StatusInternalServerError, err.Error())
				return
			}
			writeJSON(w, http.StatusOK, purchases)
		case http.MethodPost:
			if !okToWrite(w, r) {
				return
			}
			var body struct {
				CreditCardID int64   `json:"credit_card_id"`
				Description  string  `json:"description"`
				Amount       float64 `json:"amount"`
				PurchaseDate string  `json:"purchase_date"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				writeError(w, http.StatusBadRequest, "invalid JSON")
				return
			}
			date, err := time.Parse("2006-01-02", body.PurchaseDate)
			if err != nil {
				writeError(w, http.StatusBadRequest, "purchase_date must be YYYY-MM-DD")
				return
			}
			id, err := db.AddCardPurchase(db.CardPurchase{
				CreditCardID: body.CreditCardID,
				Description:  body.Description,
				Amount:       body.Amount,
				PurchaseDate: date,
			})
			if err != nil {
				writeError(w, http.StatusInternalServerError, err.Error())
				return
			}
			writeJSON(w, http.StatusCreated, map[string]int64{"id": id})
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	})

	http.HandleFunc("/card-purchases/", func(w http.ResponseWriter, r *http.Request) {
		id, err := idFromPath(r.URL.Path)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid id")
			return
		}
		switch r.Method {
		case http.MethodPut:
			if !okToWrite(w, r) {
				return
			}
			var body struct {
				Description  string  `json:"description"`
				Amount       float64 `json:"amount"`
				PurchaseDate string  `json:"purchase_date"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				writeError(w, http.StatusBadRequest, "invalid JSON")
				return
			}
			date, err := time.Parse("2006-01-02", body.PurchaseDate)
			if err != nil {
				writeError(w, http.StatusBadRequest, "purchase_date must be YYYY-MM-DD")
				return
			}
			if err := db.UpdateCardPurchase(id, db.CardPurchase{
				Description:  body.Description,
				Amount:       body.Amount,
				PurchaseDate: date,
			}); err != nil {
				writeError(w, http.StatusInternalServerError, err.Error())
				return
			}
			writeJSON(w, http.StatusOK, map[string]string{"status": "updated"})
		case http.MethodDelete:
			if !okToWrite(w, r) {
				return
			}
			if err := db.DeleteCardPurchase(id); err != nil {
				writeError(w, http.StatusInternalServerError, err.Error())
				return
			}
			writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	})

	// GET /recurring-card-purchases?credit_card_id=N lists a card's subscription
	// templates. POST adds one; PUT/DELETE /recurring-card-purchases/{id} edit
	// or remove one. Generation into card_purchases happens automatically
	// inside GeneratePeriodEntries (see /periods/generate and forecasting).
	http.HandleFunc("/recurring-card-purchases", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			cardID, ok := intQueryParam(r, "credit_card_id")
			if !ok {
				writeError(w, http.StatusBadRequest, "credit_card_id required")
				return
			}
			items, err := db.GetRecurringCardPurchases(int64(cardID))
			if err != nil {
				writeError(w, http.StatusInternalServerError, err.Error())
				return
			}
			writeJSON(w, http.StatusOK, items)
		case http.MethodPost:
			if !okToWrite(w, r) {
				return
			}
			var item db.RecurringCardPurchase
			if err := json.NewDecoder(r.Body).Decode(&item); err != nil {
				writeError(w, http.StatusBadRequest, "invalid JSON")
				return
			}
			id, err := db.AddRecurringCardPurchase(item)
			if err != nil {
				writeError(w, http.StatusInternalServerError, err.Error())
				return
			}
			writeJSON(w, http.StatusCreated, map[string]int64{"id": id})
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	})

	http.HandleFunc("/recurring-card-purchases/", func(w http.ResponseWriter, r *http.Request) {
		id, err := idFromPath(r.URL.Path)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid id")
			return
		}
		switch r.Method {
		case http.MethodPut:
			if !okToWrite(w, r) {
				return
			}
			var item db.RecurringCardPurchase
			if err := json.NewDecoder(r.Body).Decode(&item); err != nil {
				writeError(w, http.StatusBadRequest, "invalid JSON")
				return
			}
			if err := db.UpdateRecurringCardPurchase(id, item); err != nil {
				writeError(w, http.StatusInternalServerError, err.Error())
				return
			}
			writeJSON(w, http.StatusOK, map[string]string{"status": "updated"})
		case http.MethodDelete:
			if !okToWrite(w, r) {
				return
			}
			if err := db.DeleteRecurringCardPurchase(id); err != nil {
				writeError(w, http.StatusInternalServerError, err.Error())
				return
			}
			writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	})

	http.HandleFunc("/recurring-items", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			items, err := db.GetRecurringItems()
			if err != nil {
				writeError(w, http.StatusInternalServerError, err.Error())
				return
			}
			writeJSON(w, http.StatusOK, items)
		case http.MethodPost:
			if !okToWrite(w, r) {
				return
			}
			var item db.RecurringItem
			if err := json.NewDecoder(r.Body).Decode(&item); err != nil {
				writeError(w, http.StatusBadRequest, "invalid JSON")
				return
			}
			id, err := db.AddRecurringItem(item)
			if err != nil {
				writeError(w, http.StatusInternalServerError, err.Error())
				return
			}
			writeJSON(w, http.StatusCreated, map[string]int64{"id": id})
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	})

	http.HandleFunc("/recurring-items/", func(w http.ResponseWriter, r *http.Request) {
		id, err := idFromPath(r.URL.Path)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid id")
			return
		}
		switch r.Method {
		case http.MethodPut:
			if !okToWrite(w, r) {
				return
			}
			var item db.RecurringItem
			if err := json.NewDecoder(r.Body).Decode(&item); err != nil {
				writeError(w, http.StatusBadRequest, "invalid JSON")
				return
			}
			if err := db.UpdateRecurringItem(id, item); err != nil {
				writeError(w, http.StatusInternalServerError, err.Error())
				return
			}
			writeJSON(w, http.StatusOK, map[string]string{"status": "updated"})
		case http.MethodDelete:
			if !okToWrite(w, r) {
				return
			}
			if err := db.DeleteRecurringItem(id); err != nil {
				writeError(w, http.StatusInternalServerError, err.Error())
				return
			}
			writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	})

	http.HandleFunc("/entries", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			year, ok1 := intQueryParam(r, "year")
			month, ok2 := intQueryParam(r, "month")
			if !ok1 || !ok2 {
				writeError(w, http.StatusBadRequest, "year and month required")
				return
			}
			entries, err := db.GetEntries(year, month)
			if err != nil {
				writeError(w, http.StatusInternalServerError, err.Error())
				return
			}
			writeJSON(w, http.StatusOK, entries)
		case http.MethodPost:
			if !okToWrite(w, r) {
				return
			}
			var e db.Entry
			if err := json.NewDecoder(r.Body).Decode(&e); err != nil {
				writeError(w, http.StatusBadRequest, "invalid JSON")
				return
			}
			id, err := db.AddEntry(e)
			if err != nil {
				writeError(w, http.StatusInternalServerError, err.Error())
				return
			}
			writeJSON(w, http.StatusCreated, map[string]int64{"id": id})
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	})

	http.HandleFunc("/entries/", func(w http.ResponseWriter, r *http.Request) {
		id, err := idFromPath(r.URL.Path)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid id")
			return
		}
		switch r.Method {
		case http.MethodPut:
			if !okToWrite(w, r) {
				return
			}
			var e db.Entry
			if err := json.NewDecoder(r.Body).Decode(&e); err != nil {
				writeError(w, http.StatusBadRequest, "invalid JSON")
				return
			}
			if err := db.UpdateEntry(id, e); err != nil {
				writeError(w, http.StatusInternalServerError, err.Error())
				return
			}
			writeJSON(w, http.StatusOK, map[string]string{"status": "updated"})
		case http.MethodDelete:
			if !okToWrite(w, r) {
				return
			}
			if err := db.DeleteEntry(id); err != nil {
				writeError(w, http.StatusInternalServerError, err.Error())
				return
			}
			writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	})

	// POST /periods/generate?year=YYYY&month=M
	// Idempotently materializes entries for a period from active
	// recurring_items templates (this is what makes annual items reappear
	// every year instead of being lost).
	http.HandleFunc("/periods/generate", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		if !okToWrite(w, r) {
			return
		}
		year, ok1 := intQueryParam(r, "year")
		month, ok2 := intQueryParam(r, "month")
		if !ok1 || !ok2 {
			writeError(w, http.StatusBadRequest, "year and month required")
			return
		}
		created, err := db.GeneratePeriodEntries(year, month)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]int{"created": created})
	})

	http.HandleFunc("/forecast", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		year, ok1 := intQueryParam(r, "year")
		month, ok2 := intQueryParam(r, "month")
		if !ok1 || !ok2 {
			writeError(w, http.StatusBadRequest, "year and month required")
			return
		}
		summary, err := db.Forecast(year, month)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, summary)
	})

	http.HandleFunc("/forecast/range", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		year, ok1 := intQueryParam(r, "year")
		month, ok2 := intQueryParam(r, "month")
		count, ok3 := intQueryParam(r, "count")
		if !ok1 || !ok2 || !ok3 {
			writeError(w, http.StatusBadRequest, "year, month and count required")
			return
		}
		summaries, err := db.ForecastRange(year, month, count)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, summaries)
	})

	// GET /checkpoints lists every known-good balance recorded so far.
	// POST /checkpoints adds (or replaces, if the period already has one)
	// a checkpoint -- e.g. after checking the real bank app -- which
	// Forecast then re-anchors to instead of drifting forever.
	http.HandleFunc("/checkpoints", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			checkpoints, err := db.GetCheckpoints()
			if err != nil {
				writeError(w, http.StatusInternalServerError, err.Error())
				return
			}
			writeJSON(w, http.StatusOK, checkpoints)
		case http.MethodPost:
			if !okToWrite(w, r) {
				return
			}
			var c db.BalanceCheckpoint
			if err := json.NewDecoder(r.Body).Decode(&c); err != nil {
				writeError(w, http.StatusBadRequest, "invalid JSON")
				return
			}
			if c.PeriodDay == 0 {
				c.PeriodDay = 1
			}
			id, err := db.AddCheckpoint(c.PeriodYear, c.PeriodMonth, c.PeriodDay, c.Balance)
			if err != nil {
				writeError(w, http.StatusInternalServerError, err.Error())
				return
			}
			writeJSON(w, http.StatusCreated, map[string]int64{"id": id})
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	})

	log.Printf("cashflow API listening on :%s", port)
	log.Fatal(http.ListenAndServe(":"+port, nil))
}
