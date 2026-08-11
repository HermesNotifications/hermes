// Copyright 2026 Hermes Notifications. Licensed under the Apache License, Version 2.0.
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

package id

import (
	"crypto/rand"
	"encoding/binary"
	"math/big"
	"strings"
	"time"
)

// Sorted base62 alphabet: 0-9A-Za-z preserves lexicographic == numeric order.
const alphabet = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"

var base = big.NewInt(int64(len(alphabet)))

// Config defines how IDs are generated.
type Config struct {
	Prefix   string // optional type prefix, e.g. "key", "usr"
	TimeBits int    // 0 = no time component, >0 = ms timestamp truncated to this many bits
	RandBits int    // required, number of random bits
}

// Generator produces IDs with a fixed configuration.
type Generator struct {
	cfg    Config
	idLen  int // fixed output length for the base62 portion
}

// NewGenerator creates an ID generator with the given configuration.
func NewGenerator(cfg Config) *Generator {
	if cfg.RandBits <= 0 {
		panic("id: RandBits must be > 0")
	}
	totalBits := cfg.TimeBits + cfg.RandBits
	// base62 digits needed: ceil(totalBits * ln2 / ln62)
	// Use a tight approximation: ceil(totalBits / 5.954) since log2(62) ≈ 5.954
	idLen := (totalBits*1000 + 5953) / 5954
	return &Generator{cfg: cfg, idLen: idLen}
}

// New generates a new ID.
func (g *Generator) New() string {
	var buf []byte

	if g.cfg.TimeBits > 0 {
		timeBytes := (g.cfg.TimeBits + 7) / 8
		ms := uint64(time.Now().UnixMilli())
		tb := make([]byte, 8)
		binary.BigEndian.PutUint64(tb, ms)
		buf = append(buf, tb[8-timeBytes:]...)
	}

	randBytes := (g.cfg.RandBits + 7) / 8
	rb := make([]byte, randBytes)
	if _, err := rand.Read(rb); err != nil {
		panic("crypto/rand failed: " + err.Error())
	}
	buf = append(buf, rb...)

	encoded := encodeBase62(buf, g.idLen)

	if g.cfg.Prefix != "" {
		return g.cfg.Prefix + "_" + encoded
	}
	return encoded
}

// Pre-configured generators for common entity types.
var (
	// Notification generates time-sortable IDs with no prefix.
	// 48-bit ms timestamp + 80-bit random = 128 bits = 22 base62 chars.
	Notification = NewGenerator(Config{TimeBits: 48, RandBits: 80})

	// User generates prefixed IDs with no time component.
	// "usr_" prefix + 80-bit random = 14 base62 chars.
	User = NewGenerator(Config{Prefix: "usr", RandBits: 80})
)

// encodeBase62 encodes raw bytes as a fixed-width base62 string,
// zero-padded on the left to ensure consistent length.
func encodeBase62(src []byte, width int) string {
	n := new(big.Int).SetBytes(src)
	out := make([]byte, width)
	var mod big.Int
	for i := width - 1; i >= 0; i-- {
		n.DivMod(n, base, &mod)
		out[i] = alphabet[mod.Int64()]
	}
	return string(out)
}

// Parse splits a prefixed ID into its prefix and the base62-encoded value.
// The prefix is the part before the first underscore.
func Parse(id string) (prefix string, value string) {
	idx := strings.Index(id, "_")
	if idx < 0 {
		return "", id
	}
	return id[:idx], id[idx+1:]
}

// DecodeBase62 decodes a base62 string back to raw bytes.
func DecodeBase62(s string) []byte {
	n := new(big.Int)
	for _, c := range s {
		n.Mul(n, base)
		n.Add(n, big.NewInt(int64(strings.IndexRune(alphabet, c))))
	}
	return n.Bytes()
}
