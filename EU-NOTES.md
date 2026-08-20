# EU customization notes

This copy of [eraser](https://github.com/digisamroc/eraser) has been adjusted for an EU (Latvia) citizen exercising GDPR rights, not a US CCPA use case.

## What changed

`data/brokers.yaml` originally shipped 764 brokers, 751 of them US-region. That's kept as-is (US-owned platforms, ad-tech, and breach-sourced people-search sites do end up holding EU residents' data too), plus **6 new EU/UK entries** with direct opt-out emails were added at the bottom of the file:

- `192-com` (192.com, UK)
- `creditreform-de` (Creditreform, Germany)
- `regis24` (Regis24, Germany)
- `seawave-media` (Seawave Media, UK)
- `datajoy-eu` (Datajoy, Belgium)
- `athumi` (Athumi, Belgium — Data Governance Act registered intermediary)

## What's NOT in brokers.yaml

80 more EU/UK adtech, credit-reporting, B2B-intelligence, and Data Governance Act brokers exist that do **not** take a plain removal email — they require submitting a GDPR Article 15/17 request through their own web form or DSR portal (often a Termly/OneTrust/saymine.io-hosted page). Bulk-emailing those wouldn't do anything, and auto-filling arbitrary DSR portals reliably isn't safe to automate. Those live in a separate tracker: `EU_DSR_Portal_Tracker.xlsx`, delivered alongside this repo. Work through a handful at a time and log status there.

## Setup

1. `go build -o eraser ./cmd/eraser`
2. `./eraser init` — when it asks for a template, pick **gdpr** (not ccpa/generic), since GDPR Article 17 is the right legal basis to cite as an EU resident.
3. `./eraser send --dry-run` to preview, then `./eraser send` for real.

## If a company ignores or refuses a GDPR request

Controllers have 1 month to respond (extendable to 3 for complex requests). If they blow past that or refuse without a valid legal basis, Latvia's DPA is Datu valsts inspekcija (DVI): https://www.dvi.gov.lv/en. Worth knowing going in: DVI, like most EU DPAs, can be slow — noyb.eu shows some of its own complaints against Latvian companies pending multiple years. In practice the GDPR request itself, and the liability it puts on the company, is usually what gets compliance — not a fast DPA turnaround. noyb.eu also has complaint templates and occasionally takes cases directly: https://noyb.eu

## Recurring maintenance

Data brokers re-scrape and re-list you continuously. Re-run `./eraser send` and revisit unresolved rows in the tracker every 60-90 days.
