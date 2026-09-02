# Eraser

Take back your privacy. Eraser sends data removal requests to 700+ data brokers on your behalf—for free.

📖 **[eraser.drumandbytes.dev](https://eraser.drumandbytes.dev)** — broker directory, opt-out guides, and the list of EU/EEA data protection authorities.

You know those sites like Spokeo, BeenVerified, and Whitepages that have your home address, phone number, and family members' names? They're called data brokers, and there are hundreds of them. Services like Incogni and DeleteMe charge $100+/year to send opt-out requests to these companies. Eraser does the same thing, but it's open source and completely free.

### What to Expect

**The good:** Eraser automatically sends removal request emails to 700+ data brokers. Many brokers process these requests automatically—you send the email, they remove your data, done.

**The reality:** Some brokers require additional steps. They might send you a confirmation link to click, ask you to fill out a form on their website, or request identity verification. Eraser tracks these responses and shows you exactly what needs manual attention.

**The bottom line:** You're not paying $100+/year, and you're taking real action to protect your privacy. Even with some manual steps, Eraser handles the heavy lifting and gives you a fighting chance against the data broker industry.

---

## About This Fork

This is a maintained fork of [digisamroc/eraser](https://github.com/digisamroc/eraser), which has been inactive since early 2026. It includes bug fixes, performance cleanups, and GDPR/EU-specific customizations on top of the original CCPA-focused tool.

If you're an EU resident exercising GDPR rights rather than a US CCPA use case, read [EU-NOTES.md](EU-NOTES.md) first — it covers which template to pick, safe send-volume behavior, and what to do if a broker ignores your request.

---

## The Easy Way (Web Interface)

If you're not comfortable with command-line tools, Eraser has a visual interface that runs in your web browser.

### What You'll Need

1. **Go** installed on your computer ([download here](https://go.dev/dl/))
2. A **Gmail account** to send emails from (with an App Password—setup instructions below)

### Getting Started

**Step 1: Get Eraser**

**Homebrew (macOS, and Linux where cask support is available):**

```bash
brew tap drumandbytes/tap
brew install --cask drumandbytes/tap/eraser
```

Homebrew 6+ asks you to trust a third-party tap once — if it does, run
`brew trust drumandbytes/tap` and re-run the install. On macOS this also clears
the Gatekeeper quarantine (no `xattr` step). If `brew` on your Linux setup
doesn't do casks, use the tarball below.

**Prebuilt binary:** grab your OS's file from the
[Releases page](https://github.com/drumandbytes/eraser/releases) — the broker
list is baked in, nothing to install:

- **Windows** — `..._windows_amd64.exe` (run it directly) or the `.zip` (also has
  the README and `config.example.yaml`).
- **macOS / Linux** — `..._<os>_<arch>.tar.gz`; `tar xzf` it and run `./eraser`.

- **macOS:** the binary is unsigned, so Gatekeeper will block it on first run.
  Either right-click it → *Open* → *Open*, or run
  `xattr -dr com.apple.quarantine ./eraser` once.
- **Windows:** the archive contains `eraser.exe`.
- `eraser fill` and `eraser confirm` (browser automation for opt-out forms) need
  Chrome or Chromium installed; nothing else does.

**Build from source** (needs [Go](https://go.dev/dl/)):

```bash
git clone https://github.com/drumandbytes/eraser.git
cd eraser
go build -o eraser ./cmd/eraser
```

Or install straight from the module path (the broker list is baked in):

```bash
go install github.com/drumandbytes/eraser/cmd/eraser@latest
```

On Windows, build it as `eraser.exe` (`go build -o eraser.exe ./cmd/eraser`) —
Windows won't run a downloaded file with no extension.

**Step 2: Start the Web Interface**

```bash
./eraser serve
```

On Windows, run `.\eraser.exe serve` instead.

Open your browser and go to `http://localhost:8080`

**Step 3: Complete the Setup Wizard**

The wizard walks you through entering your personal information (the data brokers need this to find your records) and setting up email. Just follow the prompts.

**Step 4: Send Removal Requests**

From the dashboard, you can:
- Browse the list of 700+ data brokers
- Send requests one at a time or in bulk
- Track which requests have been sent and their status
- Exclude a broker from sends with one click (and bring it back later) - useful for a broker you'd rather skip, e.g. one that demands ID verification

That's it. The whole process takes about 10 minutes to set up, and then Eraser handles the rest.

### Setting Up Gmail

Eraser uses your Gmail account to send removal requests. You'll need to create an "App Password" (Google doesn't allow third-party apps to use your regular password).

**One-time setup (takes 2 minutes):**

1. Go to your [Google Account](https://myaccount.google.com)
2. Enable **2-Factor Authentication** if you haven't already (Security → 2-Step Verification)
3. Go to [App Passwords](https://myaccount.google.com/apppasswords)
4. Select "Mail" and your device, then click "Generate"
5. Copy the 16-character password (looks like `xxxx xxxx xxxx xxxx`)

That's the password you'll use in Eraser's setup wizard. Your regular Gmail password won't work.

**Daily sending limits:** Gmail allows ~500 emails per day. Eraser caps itself at 450/day by default (`options.daily_send_limit`) and automatically resumes where it left off on the next run, so it's safe to just re-run `eraser send` until it reports nothing left to send.

### Prefer not to give any tool your email password?

Choose **"Skip — I'll send the emails myself"** in the setup wizard (or `options.send_mode: manual` in the config, or pick option 2 in `eraser init`). Then Eraser never sends anything and needs no credentials:

- The **Brokers** page shows an **Email** link per broker (the ready-to-send removal request, with an "Open in mail app" button) and a **Mark sent** button.
- On the CLI: `eraser draft <broker-id>` prints one email, `eraser draft -o ./out` writes one `.eml` per broker (open them in your mail client), and `eraser send --manual` walks the whole list one at a time.
- After you send one, `eraser mark-sent <broker-id>` (or the web button) records it so `status`, `pipeline` and `export` still account for it.

---

## For Developers: CLI Usage

If you prefer the command line, Eraser has a full CLI.

### Installation

```bash
git clone https://github.com/drumandbytes/eraser.git
cd eraser
go build -o eraser ./cmd/eraser
```

On Windows, build it as `eraser.exe` instead (`go build -o eraser.exe ./cmd/eraser`) and run commands as `.\eraser.exe <command>` - see the note above.

### Quick Start

```bash
# Interactive setup
./eraser init

# Preview what would be sent (no emails go out)
./eraser send --dry-run

# Send removal requests to all brokers
./eraser send

# Check your history
./eraser status
```

### Commands

| Command | What it does |
|---------|--------------|
| `eraser init` | Interactive config setup |
| `eraser send` | Send removal requests (auto-capped at `daily_send_limit`/day, resumable) |
| `eraser send --dry-run` | Preview without sending |
| `eraser send --ignore-daily-limit` | Send everything in one run, ignoring the daily cap |
| `eraser send --resend` | Force re-send even to brokers within the cooldown window |
| `eraser list-brokers` | Show all 700+ brokers |
| `eraser status` | View history and stats |
| `eraser status --limit 50` | Show more history |
| `eraser add-broker` | Add a custom broker interactively |
| `eraser mark-bounced <broker-id>...` | Correct the record for brokers whose email actually bounced |
| `eraser cleanup-bounces` | Find and clear bounced broker email addresses (keeps the broker entry) |
| `eraser audit-brokers` | Check broker websites/email domains for signs of life |
| `eraser update-brokers` | Fetch the latest broker list (the list ships inside the binary; this refreshes it) |
| `eraser monitor` | Monitor your inbox (IMAP) for broker responses |
| `eraser pipeline` | Show pipeline status — which brokers need manual follow-up |
| `eraser export` | Write an evidence report (HTML/JSON) of every request and reply — for a DPA/noyb complaint |
| `eraser confirm` | Click confirmation links found in broker emails |
| `eraser fill` | Fill opt-out forms via browser automation |
| `eraser serve` | Start web interface |
| `eraser serve -p 3000` | Web interface on custom port |

### Configuration File

Your config lives at `~/.eraser/config.yaml`. Here's the full schema:

```yaml
profile:
  first_name: Jane
  last_name: Doe
  email: jane@example.com
  # Optional but helps brokers find your records
  address: "123 Main Street"
  city: "San Francisco"
  state: "CA"
  zip_code: "94102"
  country: "USA"
  phone: "+1-555-123-4567"
  date_of_birth: "1990-01-15"
  # Optional: other identities/addresses brokers may have indexed you under
  # additional_emails: [old-address@example.com]
  # name_variants: [Jane D.]
  # previous_addresses: ["456 Old Street, San Francisco, CA"]
  # additional_phones: ["+1-555-987-6543"]

email:
  provider: smtp
  from: jane@gmail.com

  smtp:
    host: smtp.gmail.com
    port: 465
    username: jane@gmail.com
    password: your-16-char-app-password  # From Google App Passwords
    use_tls: true

options:
  template: generic  # or "gdpr" or "ccpa"
  rate_limit_ms: 2000  # delay between emails
  daily_send_limit: 450  # cap per rolling 24h window (Gmail's limit is ~500/day)

  # Optional: only target specific regions
  # regions:
  #   - us
  #   - global

  # Optional: skip specific brokers
  # excluded_brokers:
  #   - spokeo
  #   - whitepages

  # Optional: skip every broker in these categories - e.g. "requires-id" to
  # skip brokers that demand a government-issued ID document before they'll
  # act on a request (this tool won't supply one on your behalf)
  # excluded_categories:
  #   - requires-id

# Optional: monitor your inbox for broker replies (used by `eraser monitor`)
# inbox:
#   enabled: true
#   provider: gmail
#   email: jane@gmail.com
#   password: your-16-char-app-password
```

### Email Templates

Eraser includes three templates:

- **GDPR** — Invokes Article 17 "Right to Erasure" under EU law
- **CCPA** — Invokes California Consumer Privacy Act rights
- **Generic** — References multiple privacy laws, works anywhere

The generic template is a good default if you're not sure. EU residents should pick `gdpr` explicitly — see [EU-NOTES.md](EU-NOTES.md).

### Adding Brokers

The broker database is at `data/brokers.yaml` and is compiled into the binary, so a downloaded `eraser` is self-contained. `eraser update-brokers` pulls a fresh copy from this repo into `~/.eraser/brokers.yaml` (a small conditional download — the app itself isn't touched). To add one by hand:

```yaml
- id: example-broker
  name: Example Broker
  email: privacy@example.com
  website: https://example.com
  opt_out_url: https://example.com/optout
  region: us  # us, eu, or global
  category: people-search  # people-search, marketing, background-check, financial-b2b, data-intermediary, device-id-only, or requires-id
```

Or use the interactive command:

```bash
./eraser add-broker
```

---

## Security Notes

- **Your config file contains personal data.** Don't commit it to git. The file is created with restricted permissions (readable only by you).
- **Use app passwords, not your real password.** For Gmail, this is required. For other providers, it's still a good idea.
- **Consider using a dedicated email.** This keeps your removal request activity separate from your main inbox.

---

## Does This Actually Work?

Yes, with caveats:

- **Most brokers comply.** They're legally required to under GDPR (EU) and CCPA (California). Even brokers not covered by these laws often honor requests to avoid liability.
- **Some require manual steps.** A meaningful share of brokers will respond asking you to:
  - Click a confirmation link (Eraser detects these and shows them to you)
  - Fill out an opt-out form on their website (Eraser tracks these as "pending tasks")
  - Verify your identity via email reply
- **It's not instant.** Brokers have up to 30-45 days to process requests (varies by law). Some are faster.
- **You'll need to repeat this.** Data brokers buy and sell data continuously. Running Eraser every 60-90 days keeps you off their lists.

**The Pipeline view** (`eraser pipeline` or the web UI) shows you exactly which brokers need manual attention. It's not fully automated, but it's free—and it does the tedious work of sending 700+ emails and tracking responses for you.

---

## How It Compares

| Service | Price | Brokers | Open Source |
|---------|-------|---------|--------------|
| **Eraser** | Free | 700+ | Yes |
| Incogni | $77/year | 180+ | No |
| DeleteMe | $129/year | 750+ | No |
| Privacy Duck | $500+/year | 500+ | No |

---

## Contributing

Contributions are welcome. The most helpful things:

- **Adding brokers** — The database at `data/brokers.yaml` can always use more entries; broker contact details also go stale
- **Privacy authorities** — `data/authorities.yaml` isn't exhaustive and links move; corrections and additions welcome
- **Template improvements** — Better wording for removal requests
- **Bug fixes** — Found something broken? PRs welcome
- **Documentation** — Typos, clarifications, better examples

---

## Project Structure

```
eraser/
├── cmd/eraser/                # CLI entry point - main.go has root wiring, one cmd_*.go
│                               # file per command (cmd_send.go, cmd_fill.go, etc.)
├── internal/
│   ├── broker/                # Broker loading and filtering
│   ├── browser/                # Browser automation (form fill, confirm links, CAPTCHA handling)
│   ├── config/                 # Configuration handling
│   ├── email/                  # SMTP sending
│   ├── history/                # SQLite request tracking
│   ├── inbox/                   # IMAP monitoring and reply classification
│   ├── template/                # Email template rendering
│   │   └── templates/            # gdpr.tmpl, ccpa.tmpl, generic.tmpl
│   └── web/                     # Web UI - server.go has core setup, handlers_*.go files
│                                 # hold the actual page/API handlers by resource
├── data/brokers.yaml           # 700+ broker database
├── config.example.yaml         # Example configuration
└── EU-NOTES.md                 # GDPR/EU-specific setup and customization notes
```

See [docs/architecture.md](docs/architecture.md) for the full breakdown, including CI/lint setup.

---

## License

MIT — do whatever you want with it.

---

## Disclaimer

This tool sends legitimate data removal requests based on privacy laws. It's not legal advice. Not all brokers are required to comply with all requests, and response times vary. But it works, and it's free.

**Your responsibility.** Eraser acts on your instructions and sends requests from your own email account. You are responsible for the accuracy of the personal data you enter, for complying with your email provider's terms and sending limits, and for anything you choose to submit to a data protection authority or other body. The templates and the `export` report are starting points, not a substitute for advice on your specific situation.
