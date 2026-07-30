#!/usr/bin/env python3
"""Cashflow lookahead summary: average income/expense/savings over a range of
months, plus a per-month list of expenses that aren't the month's usual bills
(one-off entries, and recurring items that only fall due every 3 months or
once a year, e.g. Dog pills, TV licence, Liberty). Excludes the recurring
Jenny's card / Visacard sundries buffer, which is expected every month.

Unusual items paid for on a card (e.g. a quarterly vet-meds order put on
Jenny's card) are folded into that card's bill rather than paid in cash, so
they're listed against the month the card repayment actually covers them,
not the month they were incurred in -- with a note showing that original
date.

Usage:
  ./lookahead.py                          # from this month, 7 months, print + save
  ./lookahead.py --year 2026 --month 7 --count 7
  ./lookahead.py --out ~/cashflow-forecast.md
"""
import argparse
import datetime
import urllib.request
import json

BASE_URL = "https://fimblefowl.co.uk/cashflow"


def fetch(path):
    with urllib.request.urlopen(f"{BASE_URL}{path}") as resp:
        return json.load(resp)


def month_range(year, month, count):
    out = []
    for _ in range(count):
        out.append((year, month))
        month += 1
        if month > 12:
            month = 1
            year += 1
    return out


def unusual_expenses(entries, recurring_freq):
    items = []
    for e in entries:
        if e["item_type"] != "expense":
            continue
        if e.get("credit_card_id") is not None:
            continue  # folded into a card's bill -- see card_bill_extras
        rid = e.get("recurring_item_id")
        if rid is None:
            items.append(f"{e['name']} £{e['planned_amount']:.0f} (one-off)")
        elif recurring_freq.get(rid) in ("three_monthly", "annual"):
            freq = recurring_freq[rid].replace("_", "-")
            items.append(f"{e['name']} £{e['planned_amount']:.0f} ({freq})")
    return items


def incurred_on(entry):
    y, m, d = entry["period_year"], entry["period_month"], entry.get("due_day")
    if d:
        return datetime.date(y, m, d).strftime("%-d %b")
    return datetime.date(y, m, 1).strftime("%b %Y")


def card_bill_extras(card, year, month, recurring_freq):
    """Unusual (non-routine) items folded into card's bill for (year, month),
    labelled with the date they were originally incurred on."""
    breakdown = fetch(
        f"/card-payment-breakdown?credit_card_id={card['id']}&year={year}&month={month}"
    )
    items = []
    for e in breakdown.get("one_offs") or []:
        if e["item_type"] != "expense" or e.get("auto_sundries"):
            continue
        rid = e.get("recurring_item_id")
        if rid is None:
            freq = "one-off"
        elif recurring_freq.get(rid) in ("three_monthly", "annual"):
            freq = recurring_freq[rid].replace("_", "-")
        else:
            continue  # a routine monthly item merely billed via this card
        items.append(
            f"{e['name']} £{e['planned_amount']:.0f} ({freq}, incurred on "
            f"{incurred_on(e)}, via {card['name']})"
        )
    return items


def main():
    today = datetime.date.today()
    p = argparse.ArgumentParser(description=__doc__)
    p.add_argument("--year", type=int, default=today.year)
    p.add_argument("--month", type=int, default=today.month)
    p.add_argument("--count", type=int, default=7)
    p.add_argument("--out", default=str(__import__("pathlib").Path.home() / "cashflow-forecast.md"))
    args = p.parse_args()

    recurring_freq = {r["id"]: r["frequency"] for r in fetch("/recurring-items")}
    cards = fetch("/credit-cards")
    summaries = fetch(f"/forecast/range?year={args.year}&month={args.month}&count={args.count}")

    rows = []
    total_income = total_expense = total_savings = 0.0
    for s in summaries:
        y, m = s["period_year"], s["period_month"]
        entries = fetch(f"/entries?year={y}&month={m}")
        unusual = unusual_expenses(entries, recurring_freq)
        for card in cards:
            unusual += card_bill_extras(card, y, m, recurring_freq)
        rows.append((y, m, s["income"], s["expense"], s["savings"], unusual))
        total_income += s["income"]
        total_expense += s["expense"]
        total_savings += s["savings"]

    n = len(rows)
    avg_income = total_income / n
    avg_expense = total_expense / n
    avg_savings = total_savings / n

    lines = []
    y0, m0 = rows[0][0], rows[0][1]
    y1, m1 = rows[-1][0], rows[-1][1]
    lines.append(f"# Cashflow forecast — {y0}-{m0:02d} to {y1}-{m1:02d}")
    lines.append("")
    lines.append("| Month | Income | Expense | Savings | Unusual expenses (not the month's usual bills) |")
    lines.append("|---|---|---|---|---|")
    for y, m, inc, exp, sav, unusual in rows:
        unusual_str = ", ".join(unusual) if unusual else "—"
        lines.append(f"| {y}-{m:02d} | £{inc:,.2f} | £{exp:,.2f} | £{sav:,.0f} | {unusual_str} |")
    lines.append("")
    lines.append(f"**Average income:** £{avg_income:,.2f}/month")
    lines.append(f"**Average expenses:** £{avg_expense:,.2f}/month")
    lines.append(f"**Average savings:** £{avg_savings:,.2f}/month")
    lines.append("")
    lines.append(
        "\"Unusual\" covers manual one-off entries (no recurring template) and "
        "entries from recurring items with a 3-monthly or annual frequency. "
        "Routine monthly bills and the card sundries buffer are excluded. "
        "Unusual items paid for on a card are listed against the month the "
        "card repayment covers them, noted with when they were incurred."
    )
    lines.append("")
    lines.append(f"Generated {today.isoformat()} by scripts/lookahead.py.")

    output = "\n".join(lines) + "\n"
    print(output)
    with open(args.out, "w") as f:
        f.write(output)
    print(f"\nSaved to {args.out}")


if __name__ == "__main__":
    main()
