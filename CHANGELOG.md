# Changelog

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
