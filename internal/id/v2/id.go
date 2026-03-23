package id

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/binary"
	"strings"
	"time"
)

// Config defines how IDs are generated.
type Config struct {
	Prefix   string // optional type prefix, e.g. "key", "ntf"
	TimeBits int    // 0 = no time component, >0 = ms timestamp truncated to this many bits
	RandBits int    // required, number of random bits
}

// Generator produces IDs with a fixed configuration.
type Generator struct {
	cfg Config
}

// NewGenerator creates an ID generator with the given configuration.
func NewGenerator(cfg Config) *Generator {
	if cfg.RandBits <= 0 {
		panic("id: RandBits must be > 0")
	}
	return &Generator{cfg: cfg}
}

// New generates a new ID.
func (g *Generator) New() string {
	var buf []byte

	if g.cfg.TimeBits > 0 {
		timeBytes := g.cfg.TimeBits / 8
		if g.cfg.TimeBits%8 != 0 {
			timeBytes++
		}
		ms := uint64(time.Now().UnixMilli())
		tb := make([]byte, 8)
		binary.BigEndian.PutUint64(tb, ms)
		buf = append(buf, tb[8-timeBytes:]...)
	}

	randBytes := g.cfg.RandBits / 8
	if g.cfg.RandBits%8 != 0 {
		randBytes++
	}
	rb := make([]byte, randBytes)
	if _, err := rand.Read(rb); err != nil {
		panic("crypto/rand failed: " + err.Error())
	}
	buf = append(buf, rb...)

	encoded := base64.RawURLEncoding.EncodeToString(buf)

	if g.cfg.Prefix != "" {
		return g.cfg.Prefix + "_" + encoded
	}
	return encoded
}

// MustNew generates a new ID. Panics on error (convenience wrapper).
func (g *Generator) MustNew() string {
	return g.New()
}

// Parse splits an ID into its prefix and raw decoded bytes.
// Callers that know the config can split time/random portions themselves.
func Parse(id string) (prefix string, raw []byte) {
	data := id
	if idx := strings.Index(id, "_"); idx >= 0 {
		prefix = id[:idx]
		data = id[idx+1:]
	}

	decoded, err := base64.RawURLEncoding.DecodeString(data)
	if err != nil {
		return prefix, nil
	}
	return prefix, decoded
}
