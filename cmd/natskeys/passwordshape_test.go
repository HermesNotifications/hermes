// Copyright 2026 Hermes Notifications. Licensed under the Apache License, Version 2.0.
// See LICENSE and NOTICE in the project root for full terms and restrictions.

package main

import (
	"bytes"
	"encoding/base64"
	"io"
	"strings"
	"testing"
)

// nats-accounts.conf carries this password as an unquoted variable reference, and
// nats-server resolves such a reference by RE-PARSING the environment value as a conf
// document. So the password is not an opaque string to the server: it has to lex as a bare
// conf value, and a 43-character base64url draw fails to about 2.3% of the time. When it
// fails the server does not start.
//
// The reference cannot be quoted to escape it — quoting a $VARIABLE in NATS conf stops the
// lookup happening at all and hands the server the literal text as the password. So the
// constraint has to live here, in what is generated.
//
// The rule is: the first character must be an ASCII letter. internal/messaging's
// TestCentrifugoPassword_ALeadingLetterIsAlwaysSafe enumerates the real parser against
// every such first character and a set of hostile tails; these tests are the other half,
// that the generator never emits anything else.

// asciiLetters is written out rather than computed so that no mutation of the production
// predicate can also move the assertion.
const asciiLetters = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"

// blockEncodingTo builds a 32-byte block whose base64url encoding starts with want.
// base64url's first character is the top six bits of the first byte, so '-' (index 62) needs
// 0b111110xx and 'A' (index 0) needs 0b000000xx.
//
// The assertion is the point: if the arithmetic were wrong these tests would still pass while
// feeding the generator nothing it was supposed to reject.
func blockEncodingTo(t *testing.T, firstByte byte, want rune) []byte {
	t.Helper()
	b := bytes.Repeat([]byte{0x5A}, 32)
	b[0] = firstByte
	if got := rune(base64.RawURLEncoding.EncodeToString(b)[0]); got != want {
		t.Fatalf("first byte %#x encodes to a string starting %q, want %q", firstByte, got, want)
	}
	return b
}

// The redraw, driven deterministically. A reader that hands out a '-'-leading block, then a
// digit-leading one, then a letter-leading one must produce the third — not the first.
// Sampling would only make this probable; feeding the bytes makes it certain.
func TestNewPasswordRedrawsUntilTheFirstCharacterIsALetter(t *testing.T) {
	dash := blockEncodingTo(t, 0xF8, '-')
	digit := blockEncodingTo(t, 0xD0, '0')
	underscore := blockEncodingTo(t, 0xFC, '_')
	letter := blockEncodingTo(t, 0x00, 'A')

	src := io.MultiReader(
		bytes.NewReader(dash),
		bytes.NewReader(digit),
		bytes.NewReader(underscore),
		bytes.NewReader(letter),
	)

	pw, err := newPasswordFrom(src)
	if err != nil {
		t.Fatalf("newPasswordFrom: %v", err)
	}
	if want := base64.RawURLEncoding.EncodeToString(letter); pw != want {
		t.Fatalf("newPasswordFrom returned %q, want the first letter-leading draw %q", pw, want)
	}
}

// The invariant itself, asserted on the value rather than on the mechanism, so it survives
// a rewrite of how the redraw is implemented.
func TestNewPasswordAlwaysStartsWithALetter(t *testing.T) {
	const draws = 512 // P(no rejection in 512 draws) is (52/64)^512, i.e. never
	for i := 0; i < draws; i++ {
		pw, err := newPassword()
		if err != nil {
			t.Fatalf("draw %d: %v", i, err)
		}
		if len(pw) != 43 {
			t.Fatalf("draw %d is %d characters, want 43", i, len(pw))
		}
		// Deliberately NOT the production isASCIILetter. Asserting with the same predicate
		// the generator filters on makes this test vacuous: a mutation that widened
		// isASCIILetter to "anything that is not a digit" passed here while letting a
		// '-'-leading password straight through. Spell the alphabet out.
		if !strings.ContainsRune(asciiLetters, rune(pw[0])) {
			t.Fatalf("draw %d is %q, which starts with %q — nats-server may refuse to parse it",
				i, pw, pw[0])
		}
		if strings.ContainsAny(pw, " \t\n\r\"'\\,;") {
			t.Fatalf("draw %d is %q, which contains a character base64url cannot produce", i, pw)
		}
	}
}

// The password travels to Centrifugo inside a URL, so the shape constraint must not have
// broken the property the URL depends on: base64url is entirely RFC 3986 unreserved.
func TestNewPasswordStaysURLSafe(t *testing.T) {
	const unreserved = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_"
	for i := 0; i < 128; i++ {
		pw, err := newPassword()
		if err != nil {
			t.Fatalf("draw %d: %v", i, err)
		}
		for _, r := range pw {
			if !strings.ContainsRune(unreserved, r) {
				t.Fatalf("draw %d is %q, which contains %q — that needs percent-encoding in the "+
					"Centrifugo URL and would then reach nats-server differently", i, pw, r)
			}
		}
	}
}

// A reader that never yields a usable block must produce an error rather than an infinite
// loop or a password of the wrong shape.
func TestNewPasswordGivesUpRatherThanReturningABadShape(t *testing.T) {
	// Every block encodes to a '-'-leading string.
	endless := endlessReader{block: blockEncodingTo(t, 0xF8, '-')}
	if _, err := newPasswordFrom(endless); err == nil {
		t.Fatal("newPasswordFrom returned a password from a source that can only produce bad shapes")
	}
}

type endlessReader struct{ block []byte }

func (e endlessReader) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = e.block[i%len(e.block)]
	}
	return len(p), nil
}

// A short read is an error, not a short password. crypto/rand does not do this, but an
// injected reader can, and a silently truncated credential is the worst possible outcome.
func TestNewPasswordRejectsAShortRead(t *testing.T) {
	if _, err := newPasswordFrom(bytes.NewReader(make([]byte, 8))); err == nil {
		t.Fatal("newPasswordFrom accepted 8 bytes of entropy")
	}
}
