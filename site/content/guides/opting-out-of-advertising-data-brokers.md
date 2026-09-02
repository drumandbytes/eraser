---
title: "Opting out of advertising data brokers"
description: "The long tail of adtech and data-append brokers, and why one email covers most of them."
---

Most of the companies in the [broker directory](/brokers/) aren't people-search
sites — they're **advertising and data-append brokers**: they buy, enrich and
resell marketing profiles (interests, purchase intent, approximate location,
household attributes) to advertisers and other data companies.

They rarely have a slick "remove me" page. But the same laws apply, and the same
email works for almost all of them.

## The approach

Send each one a GDPR erasure request (EU/EEA) or a CCPA deletion request
(California). Eraser does this in bulk — that's the point of `eraser send` — or
you can send them by hand with `eraser draft` / `eraser send --manual`.

For a broker where Eraser has no working email on file, check the company's
website for a privacy or "your privacy choices" page; adtech firms in scope for
US state laws increasingly have one.

## What to expect

- Many won't reply at all, but are still required to act.
- Some will say they "don't recognise" you because they key off cookies or
  device IDs, not your name. If you don't carry their tracking identifiers
  (e.g. you block third-party cookies), there may genuinely be nothing linked to
  your name — but the request still forces them to check.
- A few will ask you to use a specific form. Follow it.

## Device / advertising IDs

For brokers tagged **device-id-only**, the useful action isn't an email — it's
resetting the advertising identifier on your phone (iOS: Settings → Privacy →
Tracking / Apple Advertising; Android: Settings → Privacy → Ads) and opting out
of ad personalisation. Those pages have the detail.
