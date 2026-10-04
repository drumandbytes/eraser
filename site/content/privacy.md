---
title: "Privacy"
description: "What this website collects (cookieless, aggregate visit counts), and what the Eraser tool on your machine doesn't — nothing about your use reaches us."
---

Two different things live under this name, and they have very different answers.

## The tool

Eraser runs on your own machine. Your profile, the brokers you've contacted, and
every response you've received stay in a local database on that machine. Removal
requests go straight from your mail account to the broker.

Nothing is sent to us. There is no account, no sync, no telemetry, and no
server of ours in the path. We could not tell you how many people use Eraser, or
which brokers they've written to, because we never receive any of it.

That isn't a promise about our intentions — it's a property of how the thing is
built, and you can check it in the source.

## This website

The site is static pages served by Cloudflare Pages. Cloudflare sits in front
of it and keeps two kinds of aggregate counts:

- **Page views** (Cloudflare Web Analytics) — which pages get visited, roughly
  how many times, and referrers.
- **Referrals** — which site or link sent a visit: the referring site's name (or
  a short tag we add to our own links) and the page path, without the query
  string. Kept for three months.

Neither sets **cookies**, uses a cross-site identifier, or stores your IP
address. There's no way for us to single you out in them, which is also why
there's no cookie banner to click through.

One processor is involved in serving you this page:

| Who | What they see | Why |
| --- | --- | --- |
| Cloudflare | Your IP address and request headers, as any web server does | Serving the site, aggregate visitor counts |

## Lawful basis

Aggregate, cookieless analytics on a public site: legitimate interest,
GDPR Article 6(1)(f). Knowing roughly how many people find the project is
proportionate, and it can't identify anyone.

## Your rights

We hold no personal data about you from this website — nothing is stored beyond
the aggregate counts and whatever the processors above keep in their own logs.
So there's nothing of yours for us to export or delete.

If you use the tool, the data is on your computer, under your control. Delete
the database and it's gone.

## Who is responsible

Eraser is run by Drumandbytes OÜ (registry code 17599202), Uus-Sadama tn 21-207,
10120 Tallinn, Estonia. Write to <privacy@drumandbytes.com> about anything on
this page, including any of the rights above.

You can also raise anything publicly as a
[GitHub issue](https://github.com/drumandbytes/eraser/issues/new).

If you're in the EU or EEA and think we've got this wrong, you can complain to
your national data protection authority — see [Privacy authorities](/authorities/).
