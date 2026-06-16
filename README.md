# cashflow-backend

REST API for the cashflow forecast Android app, deployed on fimblefowl.co.uk
the same way as `qrzlook` and the shopping backend: a static Go binary run
as a `systemctl --user` service, behind nginx.

## One-time server setup

```
createdb cashflow
psql cashflow -f schema.sql
sudo cp cashflow.service ~/.config/systemd/user/   # as the peter user, no sudo needed if already in ~/.config/systemd/user
systemctl --user enable --now cashflow
```

Add the block in `nginx.conf.snippet` to the server config and reload nginx.

## Deploying updates

```
./deploy.sh peter@fimblefowl.co.uk
```

## API

All endpoints are JSON. Reads are open; writes (POST/PUT/DELETE) require an
`X-Write-Token` header matching `CASHFLOW_WRITE_TOKEN` if that env var is set.

| Method | Path                       | Notes                                            |
|--------|----------------------------|---------------------------------------------------|
| GET    | /health                    | liveness + build hash                              |
| GET    | /categories                |                                                     |
| POST   | /categories                |                                                     |
| GET    | /credit-cards              |                                                     |
| POST   | /credit-cards              |                                                     |
| PUT    | /credit-cards/{id}         |                                                     |
| GET    | /recurring-items           | templates that generate entries                    |
| POST   | /recurring-items           |                                                     |
| PUT    | /recurring-items/{id}      |                                                     |
| DELETE | /recurring-items/{id}      |                                                     |
| GET    | /entries?year=&month=      | one period's line items                            |
| POST   | /entries                   | add a one-off entry not backed by a template        |
| PUT    | /entries/{id}              | edit planned/actual amount, mark incurred           |
| DELETE | /entries/{id}              |                                                     |
| POST   | /periods/generate?year=&month= | idempotently materialize entries from templates |
| GET    | /forecast?year=&month=     | brought forward / income / expense / savings / carried forward |
| GET    | /forecast/range?year=&month=&count= | N consecutive period summaries             |
| GET    | /settings                  | opening balance + period                            |
| PUT    | /settings                  |                                                     |

## Why annual items don't get lost

`recurring_items` is the template (e.g. "glasses", frequency=annual,
target_month=3). Paying the bill only edits that period's `entries` row
(`status` → `incurred`, `actual_amount` set) — the template is never
touched. Calling `/periods/generate` for March next year creates a brand
new `entries` row from the same template, so it reappears automatically
instead of needing to be retyped.
