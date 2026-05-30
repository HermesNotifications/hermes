// Copyright 2026 Hermes Notifications. Licensed under the Apache License, Version 2.0.
// See LICENSE and NOTICE in the project root for full terms and restrictions.

package id

import (
	"crypto/rand"
	"encoding/binary"
	"time"
)

const crockford = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"

// New generates a time-sortable Crockford Base32 ID.
// Layout: 48 bits ms timestamp + 80 bits random = 128 bits = 26 Crockford chars.
func New() string {
	var b [16]byte

	// 48-bit millisecond timestamp in the high bytes
	ms := uint64(time.Now().UnixMilli())
	binary.BigEndian.PutUint64(b[:8], ms<<16)

	// 80 bits of randomness: remaining 2 bytes of b[6:8] + b[8:16]
	var rnd [10]byte
	if _, err := rand.Read(rnd[:]); err != nil {
		panic("crypto/rand failed: " + err.Error())
	}
	b[6] = rnd[0]
	b[7] = rnd[1]
	copy(b[8:], rnd[2:])

	return encode(b[:])
}

func encode(src []byte) string {
	dst := make([]byte, 26)
	hi := binary.BigEndian.Uint64(src[:8])
	lo := binary.BigEndian.Uint64(src[8:])

	for i := 25; i >= 0; i-- {
		dst[i] = crockford[lo&0x1F]
		lo = (lo >> 5) | (hi << 59)
		hi >>= 5
	}
	return string(dst)
}
