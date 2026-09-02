---
title: "When a broker ignores you"
description: "Deadlines, the evidence to keep, and how to complain to a supervisory authority — with what to expect."
---

Most brokers comply once a request lands — the request itself, and the liability
it creates, is what does the work. Some don't. Here's the escalation path.

## 1. Know the deadline

- **GDPR (EU/EEA):** one month from the day the controller received the request.
  Extendable by up to two more months only if they told you so, with reasons,
  inside the first month.
- **UK GDPR:** the same — one month, extendable by two.
- **CCPA (California):** 45 days, extendable once to a total of 90 with notice.
- **Other US state laws:** typically 45 days (some 30), often with one extension.

An automated "we got your email" is not a response. The deadline is missed when
the period passes with no substantive answer — no confirmation of erasure and no
reasoned refusal.

## 2. Keep the evidence

Run `eraser export`. It produces a single document — per broker: what was sent
and when, a copy of the request, every reply and its date, the current pipeline
stage, and an explicit list of the controllers that are **past the deadline with
no substantive response**. `--format html` prints cleanly to PDF for attaching
to a complaint.

A complaint is stronger if you can show:

- the original request (date, recipient, full text);
- proof it was delivered (a non-bounce, or a reply quoting it);
- any acknowledgement or partial reply, with dates;
- the date the deadline passed;
- for a refusal: their stated reason, so you can say why it doesn't hold.

`eraser export` assembles all of this except the raw delivery receipt from your
mail provider — keep that too.

## 3. Pick the right supervisory authority

Under **GDPR Article 77** you can lodge a complaint with the data protection
authority of the EU/EEA country where you **live**, where you **work**, or where
the **infringement happened** — your choice. The [authorities page](/authorities/)
lists all of them; `eraser export` also names the one for your country.

- **Cross-border brokers:** you still file with your *local* authority. For a
  company operating in several EU countries, that authority coordinates with the
  broker's "lead" authority under the one-stop-shop mechanism — you do not have
  to work out which that is or file abroad.
- **Germany:** for a complaint against a private company, go to the supervisory
  authority of your own federal state (Land), not the federal BfDI. The BfDI
  site links the list of the 16 state authorities.
- **UK:** complain to the ICO at
  [ico.org.uk/make-a-complaint](https://ico.org.uk/make-a-complaint/).

## 4. File the complaint

Every authority takes complaints for free, most through an online form. Include:

- your identification and contact details;
- the broker's name and (if known) address;
- what you asked for and when, and that you relied on Article 17;
- what they did or didn't do, with dates;
- the `eraser export` PDF and your mail records as attachments;
- what you want: an order that they erase your data and confirm it.

You do not need a lawyer, and you do not need to have suffered a specific harm.

## 5. What to expect

- The authority must inform you of progress within **three months**, and you can
  challenge it in court (Article 78) if it goes silent or dismisses the case
  without good reason.
- Outcomes range from a letter to the broker (often enough on its own), to a
  binding order to erase, to a fine.
- Most authorities are slow — cases can take one to several years. But an open
  complaint is itself pressure on the company, and many brokers settle the
  individual request as soon as the authority makes contact.

## 6. Compensation

**GDPR Article 82** gives a right to compensation for material *or* non-material
damage (including distress) caused by an infringement. Claims go through the
ordinary civil courts of your country, often via a small-claims track. Amounts
awarded for a single ignored erasure request are modest, but the option exists.

## 7. noyb and collective routes

[noyb.eu](https://noyb.eu) publishes ready-to-use complaint templates, and
sometimes takes strategic cases directly. Some countries also allow consumer or
privacy organisations to bring representative actions under Article 80.

## 8. United States

- **California:** report an unresponsive business to the California Privacy
  Protection Agency (CPPA) or the state Attorney General.
- **Other states:** complaints go to the state Attorney General's office; there
  is generally no private right of action for a deletion failure.
- **California DELETE Act:** the CPPA's single-request deletion platform
  ("DROP") was scheduled to open to consumers in 2026, with registered data
  brokers required to honour requests from August 2026 — check the CPPA site for
  the current status before working through brokers one by one.

---

*None of this is legal advice. You are responsible for what you submit to any
authority.*
