---
title: "Delistd vs Incogni and DeleteMe"
description: "How Delistd compares with the paid removal services Incogni and DeleteMe: price, where your details are held, who sends the requests, coverage, and when a paid service is the better choice."
---

*Written by the maker of Delistd. Prices and features were checked on 10 November 2026 against the vendors' own pricing pages (sources at the bottom). They change, so confirm them before you decide.*

Incogni and DeleteMe are services that remove your data for you. Delistd is a tool you run yourself that sends the same kind of requests from your own mailbox. They suit different people, and which is better depends on what you value: your time or your control.

## At a glance

| | Incogni | DeleteMe | Delistd |
|---|---|---|---|
| Price | $95.88/year (Standard) or $179.88/year (Unlimited) when billed annually; $15.98 or $29.98 a month when billed monthly | $129/year for one person on a one-year plan ($209 for two years) | Free, MIT licence |
| Who sends the requests | Incogni, on your behalf | DeleteMe, with human privacy advisors for custom removals | You, from your own email account on your own machine |
| Where your details are kept | With the service | With the service | On your machine only |
| Coverage | 420+ data broker sites; custom removals on the Unlimited plans only | Automated opt-outs plus manual custom removals; coverage figures vary by source | 700+ brokers in a published list you can read and add to, with a focus on the US and the EU/EEA |
| Progress reporting | Recurring removals on every plan; the Unlimited plans add an exposure scanner and a privacy score | Quarterly privacy reports | A local dashboard of each broker's reply, and an evidence report you can export |
| Replies, ID checks and confirmation links | Handled by the service | Handled by the service and its advisors | Flagged to you as tasks that need action |
| Support | Live phone support on the Unlimited plans | From the vendor | Community, through GitHub issues |
| Money-back guarantee | 30 days | Yes (terms on their site) | Not applicable |

## What a paid service gives you

- **Your time back.** You sign up, they send the requests, chase the replies and handle the verification steps. For most people that is the real product.
- **Custom removals.** Both services will deal with sites outside their standard automated list (Incogni on its Unlimited plans, DeleteMe through its advisors).
- **Someone to ask.** If something goes wrong, there is a support team.

## What it costs you

- **Your details live with the service.** To find and remove your records, a removal service needs your name, address, email address and similar identifiers, so it holds them. That is inherent to how those services work, not a criticism of them.
- **A subscription.** Roughly $96 to $180 a year per person for Incogni when billed annually and $129 for DeleteMe, and the removals only continue while you keep paying.

## What Delistd gives you

- **Custody.** Your profile, the brokers you have contacted and every reply stay in a local database on your machine. There is no account, no server of ours in the path and no telemetry (see the [privacy page](/privacy/)).
- **Requests from your own address.** Each request goes from your mail account straight to the broker, worded for where you live (GDPR Article 17 for the EU, UK and EEA; CCPA/CPRA for California). Replies are read over IMAP and sorted automatically.
- **Evidence.** `delistd export` produces a report of what was sent and when, which you can attach to a complaint to your data protection authority if a broker ignores the deadline.
- **A list you can read.** The broker list is public and editable, and you decide which brokers to include or exclude.
- **No cost.** The code is MIT-licensed and free to run.

## What Delistd asks of you

Being honest about the trade-off:

- **You do some of the work.** You set up a mail account for sending (an app password), and you handle the "needs action" items: confirmation links, ID checks and replies that ask a question.
- **It doesn't scan for you.** Delistd sends requests to the brokers in its list. It doesn't search the web for where your data appears, and it can't verify that a broker actually deleted it, only record what the broker replied.
- **No guarantee and no support team.** Brokers decide how they respond. Delistd re-sends on a schedule, because brokers re-list people, but there is nobody to call.
- **Coverage is its own list.** 700+ brokers is a large list, but it isn't the same list as the services above. You can add brokers yourself.

## Which should you choose?

- **Choose a paid service** if you want it handled for you, you'd rather not manage email replies and verification steps, or you want a support team behind it.
- **Choose Delistd** if you'd rather your personal details stay on your own machine, you're comfortable with a small amount of setup, or you're in the EU/EEA and want requests sent under your own name with an evidence trail.
- **You can also combine them.** Nothing stops you running Delistd for brokers the paid services don't cover.

[Get started](https://github.com/drumandbytes/delistd#quick-start) · [Broker directory](/brokers/)

## Sources and date

Checked on 10 November 2026. Vendors change prices and coverage, so use these to verify the figures above before you buy.

- Incogni plans, annual and monthly prices, 420+ site coverage, custom removals, exposure scanner, live phone support (Unlimited plans) and the 30-day guarantee: [incogni.com/pricing](https://incogni.com/pricing)
- DeleteMe plans and prices ($129 a year for one person, $209 for two years), and quarterly reports: [joindeleteme.com/pricing](https://joindeleteme.com/pricing/).
- Delistd: [source code and documentation](https://github.com/drumandbytes/delistd).
