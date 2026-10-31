# Changelog

## [1.8.0](https://github.com/drumandbytes/eraser/compare/v1.7.1...v1.8.0) (2026-10-31)


### Features

* **brokers:** optional per-broker countries scope ([#229](https://github.com/drumandbytes/eraser/issues/229)) ([abaa46e](https://github.com/drumandbytes/eraser/commit/abaa46ef853c1bc1f685a7c4840aac904151b807))
* **brokers:** show country scope in list-brokers and web, scope single-country brokers ([#234](https://github.com/drumandbytes/eraser/issues/234)) ([f994465](https://github.com/drumandbytes/eraser/commit/f994465b6831d8007ef9becdb34be55403b671c8))

## [1.7.1](https://github.com/drumandbytes/eraser/compare/v1.7.0...v1.7.1) (2026-10-30)


### Bug Fixes

* **brokers:** remove AZ Direct at their request (German-only address data) ([#227](https://github.com/drumandbytes/eraser/issues/227)) ([c143e50](https://github.com/drumandbytes/eraser/commit/c143e50887933282147cb62eaea91b063218d6a7))

## [1.7.0](https://github.com/drumandbytes/eraser/compare/v1.6.2...v1.7.0) (2026-10-28)


### Features

* **site:** edit request options and custom email template in Settings ([#225](https://github.com/drumandbytes/eraser/issues/225)) ([3d2a7ca](https://github.com/drumandbytes/eraser/commit/3d2a7cae347c445547cf7549f1f6252eabefe6bf))

## [1.6.2](https://github.com/drumandbytes/eraser/compare/v1.6.1...v1.6.2) (2026-10-27)


### Bug Fixes

* **brokers:** clear dead email domains (weekly audit) ([#220](https://github.com/drumandbytes/eraser/issues/220)) ([7b688a2](https://github.com/drumandbytes/eraser/commit/7b688a295048d335a547350aedd74ccc6a258e1b))

## [1.6.1](https://github.com/drumandbytes/eraser/compare/v1.6.0...v1.6.1) (2026-10-26)


### Performance Improvements

* **site:** filter the server-rendered broker rows instead of re-rendering them ([#221](https://github.com/drumandbytes/eraser/issues/221)) ([0683483](https://github.com/drumandbytes/eraser/commit/0683483fb99295781c5f324c5e6f5ffb43727dfe))

## [1.6.0](https://github.com/drumandbytes/eraser/compare/v1.5.8...v1.6.0) (2026-10-25)


### Features

* **site,brokers:** fuller guides, server-rendered directory, llms.txt, researched broker facts ([#219](https://github.com/drumandbytes/eraser/issues/219)) ([cca1f3b](https://github.com/drumandbytes/eraser/commit/cca1f3b1f747457954106be21c1e7f19c086026e))
* **site:** fix SEO audit findings ([#213](https://github.com/drumandbytes/eraser/issues/213)) ([2a3dd18](https://github.com/drumandbytes/eraser/commit/2a3dd182765e122a668864551c4fbf4b80e26dae))


### Bug Fixes

* **ci:** widen timing in the flaky session expiry test ([#217](https://github.com/drumandbytes/eraser/issues/217)) ([2d25d7c](https://github.com/drumandbytes/eraser/commit/2d25d7c453cb4439a1b0506483a59ffbd2f475af))
* **site:** keep full page titles within 60 characters ([#216](https://github.com/drumandbytes/eraser/issues/216)) ([95f10b3](https://github.com/drumandbytes/eraser/commit/95f10b3c948aad5f1cc63a3943a1c9984cc62728))
* **site:** point the docs breadcrumb at the latest version and cap titles at 58 ([#218](https://github.com/drumandbytes/eraser/issues/218)) ([c5a17a6](https://github.com/drumandbytes/eraser/commit/c5a17a6b6d774111de656afab6ed2e36845b7372))
* **site:** rewrite ../ doc links that point into subfolders ([#215](https://github.com/drumandbytes/eraser/issues/215)) ([4ced5e6](https://github.com/drumandbytes/eraser/commit/4ced5e6b2da0f57edd28fa36546294952df2c20d))

## [1.5.8](https://github.com/drumandbytes/eraser/compare/v1.5.7...v1.5.8) (2026-10-24)


### Bug Fixes

* **ci:** build releases and the site with the latest Go 1.26 patch ([#210](https://github.com/drumandbytes/eraser/issues/210)) ([918c947](https://github.com/drumandbytes/eraser/commit/918c947742b34b388044f735e1918596d3621c78))

## [1.5.7](https://github.com/drumandbytes/eraser/compare/v1.5.6...v1.5.7) (2026-10-24)


### Bug Fixes

* **ci:** make govulncheck jobs fetch the latest Go 1.26 patch ([#208](https://github.com/drumandbytes/eraser/issues/208)) ([3a948d3](https://github.com/drumandbytes/eraser/commit/3a948d3189429792cf1d478266d7e09e0e630f38))

## [1.5.6](https://github.com/drumandbytes/eraser/compare/v1.5.5...v1.5.6) (2026-10-20)


### Bug Fixes

* **cask:** remove quarantine with postflight_steps instead of deprecated postflight ([#202](https://github.com/drumandbytes/eraser/issues/202)) ([9c8e274](https://github.com/drumandbytes/eraser/commit/9c8e2748d59b525711e98c32bcaa593192aed4a6))

## [1.5.5](https://github.com/drumandbytes/eraser/compare/v1.5.4...v1.5.5) (2026-10-17)


### Bug Fixes

* **site:** cap page titles at 60 chars before the site suffix ([#200](https://github.com/drumandbytes/eraser/issues/200)) ([cc386e7](https://github.com/drumandbytes/eraser/commit/cc386e7b0cb4f044ae15cf6adae6c91a5cb8a9de))

## [1.5.4](https://github.com/drumandbytes/eraser/compare/v1.5.3...v1.5.4) (2026-10-17)


### Bug Fixes

* **site:** keep noindex docs versions out of the sitemap ([#197](https://github.com/drumandbytes/eraser/issues/197)) ([8a8cb2f](https://github.com/drumandbytes/eraser/commit/8a8cb2f11663874fc5db75d9a704897b1dce7023))

## [1.5.3](https://github.com/drumandbytes/eraser/compare/v1.5.2...v1.5.3) (2026-10-17)


### Bug Fixes

* **a11y:** table header scope in docs and 24px targets in the web UI ([#195](https://github.com/drumandbytes/eraser/issues/195)) ([2d68048](https://github.com/drumandbytes/eraser/commit/2d68048d42cbd4b529b38c51097ef3790553a9b8))

## [1.5.2](https://github.com/drumandbytes/eraser/compare/v1.5.1...v1.5.2) (2026-10-17)


### Bug Fixes

* **a11y:** skip link, labelled nav, table scope and 320px reflow on the website ([#192](https://github.com/drumandbytes/eraser/issues/192)) ([8bfebd5](https://github.com/drumandbytes/eraser/commit/8bfebd5b1dea477fc1f55b5b07f1a396996e435c))

## [1.5.1](https://github.com/drumandbytes/eraser/compare/v1.5.0...v1.5.1) (2026-10-17)


### Bug Fixes

* **a11y:** keyboard and screen-reader fixes for the web UI ([#191](https://github.com/drumandbytes/eraser/issues/191)) ([5e91c43](https://github.com/drumandbytes/eraser/commit/5e91c4381f5e232a1ecc5aa8f4a637a546d5c1fd))

## [1.5.0](https://github.com/drumandbytes/eraser/compare/v1.4.2...v1.5.0) (2026-10-17)


### Features

* **site:** add a favicon to the website and the web UI ([#187](https://github.com/drumandbytes/eraser/issues/187)) ([09f6e66](https://github.com/drumandbytes/eraser/commit/09f6e6633dd8d8a71edde75c3f09f8bcc52c79e3))


### Bug Fixes

* **a11y:** fix contrast, labels and heading order, and scan both UIs in CI ([#189](https://github.com/drumandbytes/eraser/issues/189)) ([125b7a4](https://github.com/drumandbytes/eraser/commit/125b7a4a17d0ea43169f22957feb86f27db2c7dc))
* **site:** redirect /docs/ to the latest release server-side ([#186](https://github.com/drumandbytes/eraser/issues/186)) ([f8ac914](https://github.com/drumandbytes/eraser/commit/f8ac914219efdaeb24cb433281bbedf3c2117818))

## [1.4.2](https://github.com/drumandbytes/eraser/compare/v1.4.1...v1.4.2) (2026-10-17)


### Bug Fixes

* **browser:** make the automation CSP apply, and test the Chrome paths ([#183](https://github.com/drumandbytes/eraser/issues/183)) ([c6daf91](https://github.com/drumandbytes/eraser/commit/c6daf91ce8af277f49e501c0e26d994bfaab0de2))

## [1.4.1](https://github.com/drumandbytes/eraser/compare/v1.4.0...v1.4.1) (2026-10-17)


### Bug Fixes

* **email:** recognise Windows' refused connection and run the full suite on Windows ([#179](https://github.com/drumandbytes/eraser/issues/179)) ([06fabf8](https://github.com/drumandbytes/eraser/commit/06fabf8e80be3e6b479548fb6ef1e644e6190c70))

## [1.4.0](https://github.com/drumandbytes/eraser/compare/v1.3.1...v1.4.0) (2026-10-17)


### Features

* **mcp:** add a feedback form, known limits and the real server version ([#175](https://github.com/drumandbytes/eraser/issues/175)) ([1baa25c](https://github.com/drumandbytes/eraser/commit/1baa25c792c15fa592636c48091d0f8b2d69bc3d))

## [1.3.1](https://github.com/drumandbytes/eraser/compare/v1.3.0...v1.3.1) (2026-10-17)


### Bug Fixes

* **site:** send /docs/ to the latest release ([#171](https://github.com/drumandbytes/eraser/issues/171)) ([f411a7d](https://github.com/drumandbytes/eraser/commit/f411a7de2822ecb3e5179d8de89c433259cebd40))

## [1.3.0](https://github.com/drumandbytes/eraser/compare/v1.2.9...v1.3.0) (2026-10-17)


### Features

* **mcp:** automate removal follow-ups through local agent tools ([#167](https://github.com/drumandbytes/eraser/issues/167)) ([e2d2c9d](https://github.com/drumandbytes/eraser/commit/e2d2c9d2d6460528f87cf008074f0fe1e76c9cbd))
* **mcp:** record which agent changed tasks and pipeline stages ([#168](https://github.com/drumandbytes/eraser/issues/168)) ([b9a6190](https://github.com/drumandbytes/eraser/commit/b9a619057355014e36afbe9972d0b4372d430dcd))
* **pipeline:** mark no-record replies as no_data_held ([#165](https://github.com/drumandbytes/eraser/issues/165)) ([af8a664](https://github.com/drumandbytes/eraser/commit/af8a6643d48660eb0af727baae5d84199e8432d2))
* **site:** versioned documentation with a version menu ([#169](https://github.com/drumandbytes/eraser/issues/169)) ([c3700f6](https://github.com/drumandbytes/eraser/commit/c3700f6fd118edeae0056a60468686ade70ceb0e))

## [1.2.9](https://github.com/drumandbytes/eraser/compare/v1.2.8...v1.2.9) (2026-10-13)


### Bug Fixes

* **audit:** keep broker lists canonical and report dead-end entries ([#162](https://github.com/drumandbytes/eraser/issues/162)) ([0979eb6](https://github.com/drumandbytes/eraser/commit/0979eb6535c6a23abc5324ca2889b6177fd2f930))

## [1.2.8](https://github.com/drumandbytes/eraser/compare/v1.2.7...v1.2.8) (2026-10-13)


### Bug Fixes

* **brokers:** clear dead email domains (weekly audit) ([#157](https://github.com/drumandbytes/eraser/issues/157)) ([ea8226d](https://github.com/drumandbytes/eraser/commit/ea8226d562623d320ef1c95c20c1b9b996f9201c))

## [1.2.7](https://github.com/drumandbytes/eraser/compare/v1.2.6...v1.2.7) (2026-10-04)


### Bug Fixes

* **brokers:** refresh moved opt-out URLs and clear dead ones ([#154](https://github.com/drumandbytes/eraser/issues/154)) ([29bd7bb](https://github.com/drumandbytes/eraser/commit/29bd7bb60a96cfb87eb84e65fd4f0a434860511e))

## [1.2.6](https://github.com/drumandbytes/eraser/compare/v1.2.5...v1.2.6) (2026-10-04)


### Bug Fixes

* **site:** add 404 page, share image and fuller meta descriptions ([#152](https://github.com/drumandbytes/eraser/issues/152)) ([cbdd110](https://github.com/drumandbytes/eraser/commit/cbdd1102d82f966b47498ce681a5c6215729b32f))

## [1.2.5](https://github.com/drumandbytes/eraser/compare/v1.2.4...v1.2.5) (2026-10-04)


### Bug Fixes

* **site:** add trailing slashes to internal links ([#150](https://github.com/drumandbytes/eraser/issues/150)) ([db43878](https://github.com/drumandbytes/eraser/commit/db43878658499769dfaeae39cd148e42b03f6963))

## [1.2.4](https://github.com/drumandbytes/eraser/compare/v1.2.3...v1.2.4) (2026-10-04)


### Bug Fixes

* **web:** open the browser only once serve is listening; no panic on an early Ctrl+C ([a47f0e4](https://github.com/drumandbytes/eraser/commit/a47f0e48bf4a03a77030784ea836afef711bb504))

## [1.2.3](https://github.com/drumandbytes/eraser/compare/v1.2.2...v1.2.3) (2026-10-04)


### Bug Fixes

* **send:** the daily cap no longer counts brokers that have no email ([90cf4a5](https://github.com/drumandbytes/eraser/commit/90cf4a5489aef4cf80e361de2b9a31557d7f6193))

## [1.2.2](https://github.com/drumandbytes/eraser/compare/v1.2.1...v1.2.2) (2026-10-03)


### Bug Fixes

* **browser:** eraser fill no longer fails every form after the first ([150eced](https://github.com/drumandbytes/eraser/commit/150ecedfe4a3db188cab3252f9f782f9dc1a0f36))

## [1.2.1](https://github.com/drumandbytes/eraser/compare/v1.2.0...v1.2.1) (2026-10-03)


### Bug Fixes

* **inbox:** monitor --watch no longer hangs on the first new email ([924828f](https://github.com/drumandbytes/eraser/commit/924828f0722df09b712fba034e908c2d40b95b2a))
* **web:** show "No pending brokers" when a default Send all has nothing to send ([fbecae4](https://github.com/drumandbytes/eraser/commit/fbecae4cf5267693e06602e27dfda751fbabb30a))

## [1.2.0](https://github.com/drumandbytes/eraser/compare/v1.1.0...v1.2.0) (2026-10-03)


### Features

* **web:** export evidence, mark bounced and update the broker list ([#124](https://github.com/drumandbytes/eraser/issues/124)) ([b1bfbe3](https://github.com/drumandbytes/eraser/commit/b1bfbe34ca54aef672bff4f4c75047567aca14e3))


### Bug Fixes

* **brokers:** keep your own broker entries across list updates; add them from the web ([#126](https://github.com/drumandbytes/eraser/issues/126)) ([1d7147d](https://github.com/drumandbytes/eraser/commit/1d7147d0beda302d1e9eead3d54bc2a0fbaf3e4c))

## [1.1.0](https://github.com/drumandbytes/eraser/compare/v1.0.0...v1.1.0) (2026-10-03)


### Features

* **cli:** multi-value --region/--category on draft, mark-sent, list-brokers, audit-brokers ([#121](https://github.com/drumandbytes/eraser/issues/121)) ([3c98f4f](https://github.com/drumandbytes/eraser/commit/3c98f4fe7b7398ce1dae35481e547e2d58d1dfad))
* **web:** edit name variants, other emails/phones, previous addresses and date of birth ([#122](https://github.com/drumandbytes/eraser/issues/122)) ([d102181](https://github.com/drumandbytes/eraser/commit/d1021810bd6dd031f1a86c8fdf25c9f930df87e8))

## [1.0.0](https://github.com/drumandbytes/eraser/compare/v0.11.2...v1.0.0) (2026-10-03)


### Miscellaneous Chores

* release 1.0.0 ([#117](https://github.com/drumandbytes/eraser/issues/117)) ([0d2c3ef](https://github.com/drumandbytes/eraser/commit/0d2c3ef2a83ec388c87a1f1c157b7db73c9cd2cb))

## [0.11.2](https://github.com/drumandbytes/eraser/compare/v0.11.1...v0.11.2) (2026-10-03)


### Bug Fixes

* **web:** profile saves no longer erase fields the form doesn't show ([#115](https://github.com/drumandbytes/eraser/issues/115)) ([d8edbd7](https://github.com/drumandbytes/eraser/commit/d8edbd7cc3ba2e6ff8f22a98d593ef6b72670d04))

## [0.11.1](https://github.com/drumandbytes/eraser/compare/v0.11.0...v0.11.1) (2026-10-03)


### Bug Fixes

* **release:** clear Gatekeeper quarantine in the Homebrew cask ([#112](https://github.com/drumandbytes/eraser/issues/112)) ([1f7c6b6](https://github.com/drumandbytes/eraser/commit/1f7c6b62397f26386573b7b9c607244b78036df9))
* **web:** empty provider dropdown on the setup profile step ([#113](https://github.com/drumandbytes/eraser/issues/113)) ([759b1ef](https://github.com/drumandbytes/eraser/commit/759b1eff33c5a791dfc34ccd6810e29c874974e4))

## [0.11.0](https://github.com/drumandbytes/eraser/compare/v0.10.0...v0.11.0) (2026-10-03)


### Features

* **config:** settle the config format before 1.0 ([#108](https://github.com/drumandbytes/eraser/issues/108)) ([fdd9915](https://github.com/drumandbytes/eraser/commit/fdd991571cc4194b28616ef9dae61381bfd63118))


### Bug Fixes

* **cli:** 1.0 readiness fixes and stability policy ([#110](https://github.com/drumandbytes/eraser/issues/110)) ([cfc279c](https://github.com/drumandbytes/eraser/commit/cfc279c3c1776e859b00fd18265d62fa9e64c0ac))

## [0.10.0](https://github.com/drumandbytes/eraser/compare/v0.9.2...v0.10.0) (2026-10-03)


### Features

* **email:** provider presets + STARTTLS for non-Gmail accounts ([#106](https://github.com/drumandbytes/eraser/issues/106)) ([d04a680](https://github.com/drumandbytes/eraser/commit/d04a6809672dac7f86c5b5e1e73c81fa15ebe914))

## [0.9.2](https://github.com/drumandbytes/eraser/compare/v0.9.1...v0.9.2) (2026-10-03)


### Bug Fixes

* **brokers:** place-exchange needs MAID, not email ([#104](https://github.com/drumandbytes/eraser/issues/104)) ([6d3f91c](https://github.com/drumandbytes/eraser/commit/6d3f91c7a384b420b90c6fa56818e0a6267505b4))

## [0.9.1](https://github.com/drumandbytes/eraser/compare/v0.9.0...v0.9.1) (2026-09-29)


### Bug Fixes

* **brokers:** route Narvar to DSAR form ([#100](https://github.com/drumandbytes/eraser/issues/100)) ([e51eaef](https://github.com/drumandbytes/eraser/commit/e51eaef5845f3853a217a47f3dd76eb4b760e63b))

## [0.9.0](https://github.com/drumandbytes/eraser/compare/v0.8.5...v0.9.0) (2026-09-27)


### Features

* **auto:** eraser auto + OS scheduler (launchd/systemd) ([#95](https://github.com/drumandbytes/eraser/issues/95)) ([1b0a304](https://github.com/drumandbytes/eraser/commit/1b0a3049da771a8e70e5b97f544bec456296342b))
* **web:** automation settings + in-app scheduler ([#97](https://github.com/drumandbytes/eraser/issues/97)) ([c48885c](https://github.com/drumandbytes/eraser/commit/c48885cb285a628138182e3b87e5434558bb94a0))


### Bug Fixes

* **email:** send and record a real Message-ID header ([7251c39](https://github.com/drumandbytes/eraser/commit/7251c39fa39a571533f93b51064645ecbe6bc87d))
* **inbox:** new replies from web scans and monitor --watch advance pipeline status ([7251c39](https://github.com/drumandbytes/eraser/commit/7251c39fa39a571533f93b51064645ecbe6bc87d))
* **inbox:** web scans no longer archive unrelated INBOX messages ([7251c39](https://github.com/drumandbytes/eraser/commit/7251c39fa39a571533f93b51064645ecbe6bc87d))
* **web:** scan, setup and send-status messages render with the app's styles ([7251c39](https://github.com/drumandbytes/eraser/commit/7251c39fa39a571533f93b51064645ecbe6bc87d))

## [0.8.5](https://github.com/drumandbytes/eraser/compare/v0.8.4...v0.8.5) (2026-09-27)


### Bug Fixes

* **send:** prioritize never/oldest-sent brokers under daily cap ([#93](https://github.com/drumandbytes/eraser/issues/93)) ([5d9760f](https://github.com/drumandbytes/eraser/commit/5d9760f23b07830cd53deffa315cc315a3351be0))

## [0.8.4](https://github.com/drumandbytes/eraser/compare/v0.8.3...v0.8.4) (2026-09-27)


### Performance Improvements

* **inbox,history:** envelope-first IMAP fetch, SQLite WAL + busy_timeout ([#91](https://github.com/drumandbytes/eraser/issues/91)) ([06bd6ab](https://github.com/drumandbytes/eraser/commit/06bd6abed8e2e090692ebc35e12f455398894767))

## [0.8.3](https://github.com/drumandbytes/eraser/compare/v0.8.2...v0.8.3) (2026-09-26)


### Bug Fixes

* **brokers:** First Orion ceased, Sovrn device-level note ([#88](https://github.com/drumandbytes/eraser/issues/88)) ([ef89b06](https://github.com/drumandbytes/eraser/commit/ef89b06e39b39f6fca1999d439fa77db8e22cecc))

## [0.8.2](https://github.com/drumandbytes/eraser/compare/v0.8.1...v0.8.2) (2026-09-24)


### Bug Fixes

* **release:** add trailing slash to cask homepage ([#82](https://github.com/drumandbytes/eraser/issues/82)) ([c41d938](https://github.com/drumandbytes/eraser/commit/c41d9388e0d5fe529fc84c06dee083d36e1f7fcd))

## [0.8.1](https://github.com/drumandbytes/eraser/compare/v0.8.0...v0.8.1) (2026-09-23)


### Bug Fixes

* **brokers:** add Perion (device-id-only, no email path) ([#79](https://github.com/drumandbytes/eraser/issues/79)) ([4b50e5f](https://github.com/drumandbytes/eraser/commit/4b50e5fd7767baff3e2131ecc11fcac068444a38))

## [0.8.0](https://github.com/drumandbytes/eraser/compare/v0.7.4...v0.8.0) (2026-09-23)


### Features

* **profiles:** dedicated email account per profile ([#76](https://github.com/drumandbytes/eraser/issues/76)) ([ddb9418](https://github.com/drumandbytes/eraser/commit/ddb94183c5c2a06aab817a0c538390c8ebea201b))

## [0.7.4](https://github.com/drumandbytes/eraser/compare/v0.7.3...v0.7.4) (2026-09-20)


### Bug Fixes

* stop path-filtering the workflow that hosts the required-check gate ([#72](https://github.com/drumandbytes/eraser/issues/72)) ([1b03bbd](https://github.com/drumandbytes/eraser/commit/1b03bbd11a87bef747d4e6947da8f72e85cd9eb5))

## [0.7.3](https://github.com/drumandbytes/eraser/compare/v0.7.2...v0.7.3) (2026-09-19)


### Bug Fixes

* **site:** unique broker metadata, jurisdiction-correct legal text, noindex dead ends ([#70](https://github.com/drumandbytes/eraser/issues/70)) ([99fabd2](https://github.com/drumandbytes/eraser/commit/99fabd2fed9000d27de66f0c385a90694b3e2447))

## [0.7.2](https://github.com/drumandbytes/eraser/compare/v0.7.1...v0.7.2) (2026-09-18)


### Bug Fixes

* **brokers:** mark NexSales as requires-id ([#67](https://github.com/drumandbytes/eraser/issues/67)) ([df1b15f](https://github.com/drumandbytes/eraser/commit/df1b15f7cdd90660b5670c7577f5a7efa1e19283))

## [0.7.1](https://github.com/drumandbytes/eraser/compare/v0.7.0...v0.7.1) (2026-09-14)


### Bug Fixes

* **brokers:** mark Irys and CRISIL as device-id/ID-required, name operator ([#64](https://github.com/drumandbytes/eraser/issues/64)) ([578f199](https://github.com/drumandbytes/eraser/commit/578f19922a056ebf2c2a41bfd5afad355a5ae049))

## [0.7.0](https://github.com/drumandbytes/eraser/compare/v0.6.1...v0.7.0) (2026-09-12)


### Features

* **brokers:** verified registry-sourced list, custom list config, audit auto-prune ([#55](https://github.com/drumandbytes/eraser/issues/55)) ([460a8b8](https://github.com/drumandbytes/eraser/commit/460a8b8ab9f2b22ec90c1c2cbc3befa32063bfd5))

## [0.6.1](https://github.com/drumandbytes/eraser/compare/v0.6.0...v0.6.1) (2026-09-12)


### Bug Fixes

* **web:** restore manual response review flow ([#57](https://github.com/drumandbytes/eraser/issues/57)) ([469145c](https://github.com/drumandbytes/eraser/commit/469145c24722fbc3745cc415ea5a056297231c11))

## [0.6.0](https://github.com/drumandbytes/eraser/compare/v0.5.4...v0.6.0) (2026-09-07)


### Features

* **brokers:** restore a verified contact for Cross Pixel Media ([#47](https://github.com/drumandbytes/eraser/issues/47)) ([7d717a4](https://github.com/drumandbytes/eraser/commit/7d717a4a4284b50cd34a8562549b17864fa0437a))

## [0.5.4](https://github.com/drumandbytes/eraser/compare/v0.5.3...v0.5.4) (2026-09-07)


### Bug Fixes

* **brokers:** drop Cross Pixel Media's undeliverable contact ([#45](https://github.com/drumandbytes/eraser/issues/45)) ([acf5205](https://github.com/drumandbytes/eraser/commit/acf52050bf2d875e58374e8909f5841878dab860))

## [0.5.3](https://github.com/drumandbytes/eraser/compare/v0.5.2...v0.5.3) (2026-09-07)


### Bug Fixes

* **audit:** stop reporting blocked sites as dead brokers ([#43](https://github.com/drumandbytes/eraser/issues/43)) ([895419d](https://github.com/drumandbytes/eraser/commit/895419da3e7b89126c7a0a23569e8e55f31369ae))

## [0.5.2](https://github.com/drumandbytes/eraser/compare/v0.5.1...v0.5.2) (2026-09-07)


### Bug Fixes

* **site:** drop the manual beacon, let the edge inject it ([#40](https://github.com/drumandbytes/eraser/issues/40)) ([2fa04a6](https://github.com/drumandbytes/eraser/commit/2fa04a6bb06b7c9152ab408192200cc4a56dc417))

## [0.5.1](https://github.com/drumandbytes/eraser/compare/v0.5.0...v0.5.1) (2026-09-06)


### Bug Fixes

* **site:** load the beacon as a module, not a deferred script ([#38](https://github.com/drumandbytes/eraser/issues/38)) ([f05dd38](https://github.com/drumandbytes/eraser/commit/f05dd3827dce9d0374883b2ce4704094ffd2e60b))

## [0.5.0](https://github.com/drumandbytes/eraser/compare/v0.4.1...v0.5.0) (2026-09-06)


### Features

* **site:** add Cloudflare Web Analytics to the public site ([#36](https://github.com/drumandbytes/eraser/issues/36)) ([69b56ce](https://github.com/drumandbytes/eraser/commit/69b56ce396f86d8f8e38b5d1b8c6a1290c787f2b))

## [0.4.1](https://github.com/drumandbytes/eraser/compare/v0.4.0...v0.4.1) (2026-09-06)


### Bug Fixes

* **ci:** opt into app-token merge, so releases actually finish ([#34](https://github.com/drumandbytes/eraser/issues/34)) ([873d0b1](https://github.com/drumandbytes/eraser/commit/873d0b1def2feef9a310acdb46390b773f3da5bb))

## [0.4.0](https://github.com/drumandbytes/eraser/compare/v0.3.2...v0.4.0) (2026-09-04)


### Features

* **ci:** adopt release-please for automated releases ([#29](https://github.com/drumandbytes/eraser/issues/29)) ([5097ed8](https://github.com/drumandbytes/eraser/commit/5097ed860f45ff7a3a886b679a32712567f294ce))
* **seo:** sitemap reference in robots.txt + OG/JSON-LD metadata ([72d9f93](https://github.com/drumandbytes/eraser/commit/72d9f932f24b01eed18bfd2c2df860aa866d70ee))
* **seo:** sitemap reference in robots.txt + OG/JSON-LD metadata ([412a2a7](https://github.com/drumandbytes/eraser/commit/412a2a75407edbb9953526909db19854e3802e87))


### Bug Fixes

* **brokers:** Project Applecart no longer accepts email erasure requests ([#32](https://github.com/drumandbytes/eraser/issues/32)) ([d4986a0](https://github.com/drumandbytes/eraser/commit/d4986a0e2130a98a36a403055509c6e54f02f804))
