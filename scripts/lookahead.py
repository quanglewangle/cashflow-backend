#!/usr/bin/env python3
"""Cashflow lookahead summary: average income/expense/savings over a range of
months, plus a per-month list of expenses that aren't the month's usual bills
(one-off entries, and recurring items that only fall due every 3 months or
once a year, e.g. Dog pills, TV licence, Liberty). Excludes the recurring
Jenny's card / Visacard sundries buffer, which is expected every month.

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
        if "sundries" in e["name"].lower():
            continue
        rid = e.get("recurring_item_id")
        if rid is None:
            items.append(f"{e['name']} £{e['planned_amount']:.0f} (one-off)")
        elif recurring_freq.get(rid) in ("three_monthly", "annual"):
            freq = recurring_freq[rid].replace("_", "-")
            items.append(f"{e['name']} £{e['planned_amount']:.0f} ({freq})")
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
    summaries = fetch(f"/forecast/range?year={args.year}&month={args.month}&count={args.count}")

    rows = []
    total_income = total_expense = total_savings = 0.0
    for s in summaries:
        y, m = s["period_year"], s["period_month"]
        entries = fetch(f"/entries?year={y}&month={m}")
        unusual = unusual_expenses(entries, recurring_freq)
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
        "Routine monthly bills and the card sundries buffer are excluded."
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
