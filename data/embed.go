// Package data embeds the files under data/ into the binary so a released
// eraser is self-contained (no data/ directory needs to sit beside it).
package data

import _ "embed"

// BrokersYAML is data/brokers.yaml, the broker database shipped with this
// build. broker.Load() falls back to it when there is no --brokers override
// and no ~/.eraser/brokers.yaml. Refresh the on-disk copy with
// `eraser update-brokers`.
//
//go:embed brokers.yaml
var BrokersYAML []byte

// AuthoritiesYAML is data/authorities.yaml, the worldwide privacy /
// data-protection authority reference used by `eraser export` and the docs site.
//
//go:embed authorities.yaml
var AuthoritiesYAML []byte
