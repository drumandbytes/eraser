# EU customization notes

This copy of [eraser](https://github.com/digisamroc/eraser) has been adjusted for an EU (Latvia) citizen exercising GDPR rights, not a US CCPA use case.

## What changed

`data/brokers.yaml` originally shipped 764 brokers, 751 of them US-region. That's kept as-is (US-owned platforms, ad-tech, and breach-sourced people-search sites do end up holding EU residents' data too), plus EU/UK entries with direct opt-out emails have been added as they were found. The exact count moves around over time - EU/UK additions push it up, campaign-response reviews that find duplicate/dead entries push it back down (see auditing.md) - which is why the docs quote "700+" rather than a specific number. Run `grep -c '^    - id:' data/brokers.yaml` for the current true count:

- `192-com` (192.com, UK)
- `creditreform-de` (Creditreform, Germany)
- `regis24` (Regis24, Germany)
- `seawave-media` (Seawave Media, UK)
- `datajoy-eu` (Datajoy, Belgium)
- `adikteev` (Adikteev, ad-tech)
- `scope3` (Scope3, ad-tech)
- `smartclip` (Smartclip, ad-tech)
- `genius-sports` (Genius Sports Group)
- `etarget-sk` (eTarget s.r.o., Slovakia)
- `creditsafe` (Creditsafe, financial/B2B credit reporting)

### EU/GDPR broker expansion

The EU-region set was later widened from 9 entries to 45, sourced from
[datenanfragen.de's company database](https://github.com/datenanfragen/data)
(CC0), which carries verified GDPR/DSR contact addresses. Added: the German
and Austrian credit agencies (SCHUFA, CRIF/CRIFBÜRGEL, Bisnode, Regis24,
infoscore/Experian, Creditreform, KSV1870, AKV Europa, IHD, Intrum), the
address dealers and marketing-list brokers (AZ Direct, Deutsche Post Direkt,
Deutsche Post Adress, Schober, Nexiga, Panadress, bedirect, SAZ, Uniserv,
Quadress, Das Telefonbuch), and the UK credit-reference entities
(Experian UK, Equifax UK, Callcredit, Creditreform UK) as `global` rather
than `eu`, since post-Brexit they fall under UK GDPR rather than EU law.

Four Latvia-specific brokers are listed with **no email on file** -
`lursoft`, `firmas-lv`, `creditinfo-lv` and `1188-lv`. They are the most
locally relevant targets for this fork's user, but their sites could not be
reached to verify a DSR address, and an unverified privacy address is worse
than none. Look the contact up on each site and fill the `email:` field in
before sending; until then `eraser list-brokers --missing-email` will keep
surfacing them. All four are tagged `priority: high`.

## What's NOT in brokers.yaml

Other EU/UK adtech, credit-reporting, B2B-intelligence, and Data Governance Act brokers exist that do **not** take a plain removal email — they require submitting a GDPR Article 15/17 request through their own web form or DSR portal (often a Termly/OneTrust/saymine.io-hosted page), or are cookie-identifier-based deletions that don't apply if you're not carrying their tracking cookies (e.g. browsing with Brave's strict cookie blocking). Bulk-emailing those wouldn't do anything, and auto-filling arbitrary DSR portals reliably isn't safe to automate. Track those manually outside this repo — a spreadsheet works fine — rather than committing a personal review-status file here.

Separately, a category of EU Data Governance Act-registered "data intermediation services" (e.g. personal data spaces, data wallets, data-sharing infrastructure providers) surfaced during manual review. These are generally **not applicable**: they're B2B/consumer data-sharing infrastructure, not data brokers that hold your profile by default. A GDPR request only makes sense there if you've actually signed up for one of those services yourself.

## Setup

1. `go build -o eraser ./cmd/eraser`
2. `./eraser init` — when it asks for a template, pick **gdpr** (not ccpa/generic), since GDPR Article 17 is the right legal basis to cite as an EU resident.
3. `./eraser send --dry-run` to preview, then `./eraser send` for real.

## If a company ignores or refuses a GDPR request

Controllers have 1 month to respond (extendable to 3 for complex requests). If they blow past that or refuse without a valid legal basis, Latvia's DPA is Datu valsts inspekcija (DVI): https://www.dvi.gov.lv/en. Worth knowing going in: DVI, like most EU DPAs, can be slow — noyb.eu shows some of its own complaints against Latvian companies pending multiple years. In practice the GDPR request itself, and the liability it puts on the company, is usually what gets compliance — not a fast DPA turnaround. noyb.eu also has complaint templates and occasionally takes cases directly: https://noyb.eu

## Sending 770+ emails safely

The README originally claimed eraser auto-chunks large sends across multiple days - that wasn't actually implemented in the code, just documented. It is now: `send` caps itself at `options.daily_send_limit` (450 by default, safely under Gmail's ~500/day) per rolling 24h window, and automatically skips any broker it already successfully emailed in the last 25 days. That means it's safe to just run:

```
./eraser send
```

...repeatedly (same day or the next) until it reports nothing left to send - it resumes where it left off without double-emailing anyone. Flags: `--ignore-daily-limit` sends everything in one run regardless of the cap (only if your provider can actually handle that volume), `--resend` forces re-sending even to brokers within the 25-day cooldown (useful for a deliberate full re-run).

## Recurring maintenance

Data brokers re-scrape and re-list you continuously. Re-run `./eraser send` and revisit unresolved manual items (portal/DSR-based brokers, form-required cases) every 60-90 days.
