---
title: "Sending a GDPR Article 17 request"
description: "What to write, who to send it to, how brokers must respond, and the exemptions they can lawfully invoke."
---

If you live in the EU or EEA, **Article 17 of the GDPR** gives you the right to
have a company erase the personal data it holds about you. Data brokers are
"controllers" under the GDPR and this right applies to them squarely — it does
not matter that you never signed up with them or that the company is based
outside the EU, as long as it processes data about people in the EU.

## What counts as "your personal data"

Anything that identifies you or can be linked back to you: your name, addresses
(current and former), phone numbers, email addresses, date of birth, relatives
and associates, employment and property records, and any profile or score a
broker has built from them. You are entitled to have **all** of it erased, not
just the fields shown on a preview page.

## What to send

A short email is enough. It needs to:

1. **Identify you** — full name, email, and (so they can find the record)
   postal address, plus any former names, emails or addresses a broker might
   have indexed you under.
2. **State the request** — that you are exercising your right to erasure under
   **Article 17 of the GDPR** and want all personal data concerning you deleted.
3. **Ask for written confirmation** once it is done.
4. Optionally, cite **Article 19** — the broker must also tell every party it
   sold or shared your data with to erase it, and must name those recipients if
   you ask.

You do not have to give a reason. "The data is no longer necessary for the
purpose it was collected" and "I object to the processing" (Article 17(1)(a) and
(c)) both apply to a broker by default.

Eraser's `gdpr` template covers all of this. To send it yourself, run
`eraser draft <broker-id>` (or open the **Email** page in the web UI) and paste
the result into your mail client.

## Who to send it to

The broker's privacy or data-protection address. The [broker directory](/brokers/)
lists the address Eraser has on file for each one; where a broker only takes
requests through a web form or a dedicated portal, that link is shown instead.
Using their form is fine — the legal deadline is the same.

## Identity verification

A broker may ask you to confirm your identity before acting (Article 12(6)), but
the request has to be **proportionate**. Confirming the email address they
already hold, or matching a few data points from your file, is reasonable. A
full copy of your passport usually is not — if you send ID at all, redact the
photo, document number and any fields not needed to match your record. The
one-month clock is paused only for the time it genuinely takes you to answer a
proportionate verification request.

## What happens next

- The controller has **one month** from receipt to respond. It can extend this
  by **up to two further months** for genuinely complex or numerous requests,
  but only if it tells you about the extension *and the reason* within the first
  month. A plain deletion is rarely complex enough to justify this.
- A response means a substantive answer — "done", or a reasoned refusal — not an
  automated acknowledgement.
- If they refuse, wholly or partly, they must tell you **why**, which exemption
  they rely on, and that you can complain to a supervisory authority and go to
  court.
- They may not charge a fee unless the request is "manifestly unfounded or
  excessive" — a single, ordinary erasure request is neither.

## Exemptions a broker can lawfully invoke

Article 17(3) lets a controller keep *specific* data despite your request, where
it is still necessary for:

- exercising the right to freedom of expression and information;
- complying with a legal obligation, or a task carried out in the public
  interest;
- archiving in the public interest, scientific or historical research, or
  statistics;
- the establishment, exercise or defence of legal claims.

For a data broker these almost never cover the marketing or people-search
profile itself. If one is claimed, ask them to identify exactly which data it
applies to and to erase the rest.

## "We don't hold any data on you"

Some brokers — especially adtech firms — key their records off cookies or device
identifiers rather than your name, and may reply that they cannot find you. Ask
them to confirm in writing that no data linked to your name, email or postal
address is held, and to erase anything that later matches. That written "no
data" answer is itself useful evidence.

## Related rights

- **Article 15 (access)** — ask what data they hold and who they shared it with,
  before or alongside an erasure request.
- **Article 21 (objection)** — for direct-marketing processing you can object
  outright, and the controller must stop; there is no exemption.

## If they ignore you

See [When a broker ignores you](/guides/when-a-broker-ignores-you/).
