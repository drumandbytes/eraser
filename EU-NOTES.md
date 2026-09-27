# GDPR (EU / EEA) notes

This fork of [eraser](https://github.com/digisamroc/eraser) is set up for an EU/EEA resident exercising GDPR rights (right to erasure, Article 17), not the original US/CCPA use case. It defaults to the `gdpr` template and this file covers the EU-specific details.

## What changed

`data/brokers.yaml` originally shipped 764 brokers, 751 of them US-region. That's kept as-is (US-owned platforms, ad-tech, and breach-sourced people-search sites do end up holding EU residents' data too), plus EU/UK entries with direct opt-out emails have been added as they were found. The exact count moves around over time - EU/UK additions push it up, campaign-response reviews that find duplicate/dead entries push it back down (see auditing.md) - which is why the docs quote a round floor rather than a specific number. The floor moved to "750+" on 2026-09-08 (list at 763), down to "700+" on 2026-09-12 after a full-list legitimacy sweep merged 1 duplicate and removed 20 entries that couldn't be confirmed as real, operating data brokers (list at 741, then 718 after resolving the sweep's remaining flags), then a dedicated EU-coverage pass the same day added 10 researched entries (list at 728). A subsequent global pass consolidated stale aliases, removed one unreachable EU entry, and added authoritative credit/data-intelligence channels in Latin America, New Zealand, and Kenya (list at 723) - the sweep and its findings are described in the PR that landed it, not repeated here. Run `grep -c '^    - id:' data/brokers.yaml` for the current true count:

- `192-com` (192.com, UK)
- `creditreform-de` (Creditreform, Germany)
- `regis24` (Regis24, Germany)
- `adikteev` (Adikteev, ad-tech)
- `smartclip` (Smartclip, ad-tech)
- `genius-sports` (Genius Sports Group)
- `etarget-sk` (eTarget s.r.o., Slovakia)
- `creditsafe` (Creditsafe, financial/B2B credit reporting)
- `schufa` (SCHUFA, Germany - major consumer credit bureau)
- `crif` (CRIF, Italy - pan-EU credit/business-information bureau)
- `dun-bradstreet-eu` (Dun & Bradstreet EU, incl. former Bisnode entities)
- `deutsche-post-direkt` (Deutsche Post Direkt, Germany - address/geo-marketing data)
- `az-direct` (AZ Direct, Germany/Austria - Bertelsmann address broker; see its note re: a noyb complaint)
- `adform` (Adform, Denmark - ad-tech DSP/DMP)
- `criteo` (Criteo, France - retargeting ad-tech)
- `rtb-house` (RTB House, Poland - programmatic ad-tech)
- `equativ` (Equativ/Smart AdServer, France - ad-serving)
- `teads` (Teads, France - outstream video ad-tech)

`datajoy-eu` and `scope3` were removed on 2026-09-12: Datajoy's own reply said "we're not even a
data broker," and Scope3's privacy policy explicitly states it doesn't sell or share personal
data - neither belongs in a data-broker list regardless of region.

16 of the entries above are also in `data/brokers-verified.yaml` - see
[architecture.md](docs/architecture.md) for the `full`/`verified` list selector. `adikteev` is
excluded because it replied that it's a processor, not a controller, and can't action the
request directly. `genius-sports` is `region: global`, so it is already reachable regardless of
the EU region filter. `seawave-media` was removed after its address hard-bounced and no website
or request channel could be verified.

## What's NOT in brokers.yaml

Other EU/UK adtech, credit-reporting, B2B-intelligence, and Data Governance Act brokers exist that do **not** take a plain removal email — they require submitting a GDPR Article 15/17 request through their own web form or DSR portal (often a Termly/OneTrust/saymine.io-hosted page), or are cookie-identifier-based deletions that don't apply if you're not carrying their tracking cookies (e.g. browsing with Brave's strict cookie blocking). Bulk-emailing those wouldn't do anything, and auto-filling arbitrary DSR portals reliably isn't safe to automate. Track those manually outside this repo — a spreadsheet works fine — rather than committing a personal review-status file here.

Separately, a category of EU Data Governance Act-registered "data intermediation services" (e.g. personal data spaces, data wallets, data-sharing infrastructure providers) surfaced during manual review. These are generally **not applicable**: they're B2B/consumer data-sharing infrastructure, not data brokers that hold your profile by default. A GDPR request only makes sense there if you've actually signed up for one of those services yourself.

## Setup

1. `go build -o eraser ./cmd/eraser`
2. `./eraser init` — when it asks for a template, pick **gdpr** (not ccpa/generic), since GDPR Article 17 is the right legal basis to cite as an EU resident.
3. `./eraser send --dry-run` to preview, then `./eraser send` for real.

## If a company ignores or refuses a GDPR request

Controllers have 1 month to respond (extendable to 3 for complex requests). If they blow past that or refuse without a valid legal basis, you can lodge a complaint (GDPR Art. 77) with the supervisory authority of the EU/EEA country where you live, work, or where the infringement happened. The full list of authorities and their websites is in [data/authorities.yaml](data/authorities.yaml); `./eraser export` also names the one for your profile's `country`.

Set expectations: most EU DPAs are slow — noyb.eu shows some of its own complaints pending multiple years. In practice the GDPR request itself, and the liability it puts on the company, is usually what gets compliance, not a fast DPA turnaround. [noyb.eu](https://noyb.eu) has complaint templates and occasionally takes cases directly.

Run `./eraser export` to generate the evidence to attach to a complaint: per broker, what was sent and when, the reconstructed request, every reply and its date, and an explicit list of controllers past the 1-month deadline with no substantive response. `--format html` opens in a browser and prints to PDF.

### Germany

For a complaint against a private company (a data broker), a German resident normally goes to the supervisory authority of their own federal state (Land), not the federal BfDI. The [BfDI site](https://www.bfdi.bund.de) links the list of the 16 state authorities.

### UK

Post-Brexit the UK is outside GDPR/the EDPB but enforces the UK GDPR, which mirrors the right to erasure. UK residents complain to the ICO: https://ico.org.uk/make-a-complaint/

## Sending the requests by hand

A GDPR Article 17 request is exactly as valid sent from your own mail client as it is sent by Eraser. If you'd rather not configure an app password, set `options.send_mode: manual` (or choose "Skip" in the web setup): `eraser draft` / `eraser send --manual` render the emails, you send them, and `eraser mark-sent` records them so `export` and `pipeline` still work.

## Sending 770+ emails safely

The README originally claimed eraser auto-chunks large sends across multiple days - that wasn't actually implemented in the code, just documented. It is now: `send` caps itself at `options.daily_send_limit` (450 by default, safely under Gmail's ~500/day) per rolling 24h window, and automatically skips any broker it already successfully emailed in the last 25 days. That means it's safe to just run:

```
./eraser send
```

...repeatedly (same day or the next) until it reports nothing left to send - it resumes where it left off without double-emailing anyone. Flags: `--ignore-daily-limit` sends everything in one run regardless of the cap (only if your provider can actually handle that volume), `--resend` forces re-sending even to brokers within the 25-day cooldown (useful for a deliberate full re-run).

## Recurring maintenance

Data brokers re-scrape and re-list you continuously. Re-run `./eraser send` and revisit unresolved manual items (portal/DSR-based brokers, form-required cases) every 60-90 days.
