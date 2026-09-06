---
title: "Privacy"
description: "What this website collects, and what the tool on your machine doesn't."
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

The site is static pages hosted on GitHub Pages, with one third-party script:
Cloudflare Web Analytics.

It records aggregate page views — which pages get visited, roughly how many
times, and referrers. It sets **no cookies**, uses no cross-site identifier, and
builds no profile of you. There's no way for us to single you out in it, which
is also why there's no cookie banner to click through.

Two processors are involved in serving you this page:

| Who | What they see | Why |
| --- | --- | --- |
| GitHub (Pages) | Your IP address, as any web server does | Serving the site |
| Cloudflare | Your IP address, transiently, for the analytics beacon | Aggregate visitor counts |

Cloudflare is not in front of this site — the DNS record is unproxied, so GitHub
serves you directly. Cloudflare only receives the analytics beacon your browser
sends.

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

Eraser is run by Maris Popens, in Estonia. Write to <maris@popens.lv> about
anything on this page, including any of the rights above.

You can also raise anything publicly as a
[GitHub issue](https://github.com/drumandbytes/eraser/issues/new).

If you're in the EU or EEA and think we've got this wrong, you can complain to
your national data protection authority — see [Privacy authorities](/authorities/).
