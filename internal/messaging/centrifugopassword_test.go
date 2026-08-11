// Copyright 2026 Hermes Notifications. Licensed under the Apache License, Version 2.0.
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

package messaging_test

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nkeys"
)

// ADR 0005 phase 4. nats-accounts.conf carries Centrifugo's credential as an unquoted
// variable reference:
//
//	password: $HERMES_CENTRIFUGO_NATS_PASSWORD
//
// That reference is not a simple substitution. nats-server's conf parser resolves it by
// re-parsing the environment value as a fresh document (`pk=<value>`, conf/parse.go
// lookupVariable), so the VALUE has to lex as a bare conf value. Some strings over the
// base64url alphabet do not, and the server then refuses to start — in the cluster exactly
// as in this package.
//
// Everything below drives the real parser against the real committed file. Nothing here
// models the lexer; the tables are enumerated inputs and observed outcomes, because the
// rule turned out to be considerably stranger than either "leading dash" or "leading
// digit" — see TestCentrifugoPassword_ShapesTheParserRejects.

// parseWithCentrifugoPassword sets the variable to pw, runs the committed accounts file
// through the same server.ProcessConfigFile the StatefulSet's nats-server runs, and
// restores the variable. A nil error means a cluster given this password would start.
func parseWithCentrifugoPassword(t *testing.T, pw string) error {
	t.Helper()
	perms(t) // populates every $HERMES_NKEY_* the file also needs
	saved, hadSaved := os.LookupEnv(centrifugoPasswordVar)
	t.Cleanup(func() {
		if hadSaved {
			_ = os.Setenv(centrifugoPasswordVar, saved)
		} else {
			_ = os.Unsetenv(centrifugoPasswordVar)
		}
	})
	if err := os.Setenv(centrifugoPasswordVar, pw); err != nil {
		t.Fatalf("setenv: %v", err)
	}
	_, err := serverOptsFromAccountsConf(t.TempDir())
	return err
}

// safeTail is 42 characters that are unremarkable to the lexer, so a case built from it
// isolates whatever the case is varying.
const safeTail = "AbCdEfGhIjKlMnOpQrStUvWxYzAbCdEfGhIjKlMnOp"

// The failing shapes, named. Each entry was found by driving the parser, not by reading
// it, and each error string is the one nats-server actually produced.
//
// The rule, as far as it could be characterised:
//
//   - a leading '-' puts the lexer in lexNegNumberStart, which demands a digit next;
//   - a leading digit puts it in lexNumberOrDateOrStringOrIP, where the first non-digit
//     decides everything: '-' means "this is an ISO8601 date" and almost nothing is;
//   - a number suffix (kKmMgGtTpPeE) followed by a DIGIT ends the integer early, and the
//     rest of the password is then unexpected trailing junk;
//   - a value that lexes cleanly as an integer or a bool is worse than a parse error: it
//     reaches the server as the wrong Go type.
//
// A leading LETTER short-circuits all of it — lexValue falls straight through to
// lexString, which consumes the whole value. That is the invariant cmd/natskeys now
// guarantees, and TestCentrifugoPassword_ALeadingLetterIsAlwaysSafe is its other half.
func TestCentrifugoPassword_ShapesTheParserRejects(t *testing.T) {
	cases := []struct {
		name, password, wantErrSubstring string
	}{
		// The lexer wants a digit after the sign and gets the password's second character.
		// This is the ~1.6% of base64url draws that a leading '-' accounts for, and the
		// shape behind the field report that named 'R' as the offending character.
		{"leading dash, then a letter", "-" + safeTail, "Expected a digit but got 'A'"},
		// A leading '-' followed by a digit is not saved by it: the integer ends at the
		// first letter and the remaining 40 characters are trailing junk.
		{"leading dash, then a digit", "-7" + safeTail, "Expected a top-level value to end"},

		// A leading digit is only a problem when the first non-digit is '-'. At offset 4
		// the lexer commits to ISO8601 and complains about the date; anywhere else it
		// complains about the date's shape. Both are unreachable from a password.
		{"digits then a dash at offset 4", "1234-" + safeTail, "ISO8601"},
		{"digits then a dash elsewhere", "12-" + safeTail, "All ISO8601 dates must be in full Zulu form"},
		// The variant that names a DIGIT as the offending character, which is the other
		// half of the field report.
		{"digits, dash, then digits", "1234-5678" + safeTail, "but found '7' instead"},

		// The case neither of the two standing hypotheses predicted, and the one that
		// makes modelling the lexer a bad idea: 'p' is a size suffix, so "2p" is a
		// complete integer, and a digit after it terminates the number rather than
		// continuing the string.
		{"digit, size suffix, digit", "2p2" + safeTail, "Expected a top-level value to end"},
		{"digit, size suffix, digit (e)", "9e0" + safeTail, "Expected a top-level value to end"},

		// Values that lex cleanly but as the wrong type. These do not reach the parser's
		// error path at all; they fail later, when the server reads the users block.
		{"all digits, small enough for int64", "1234567890", "not string"},
		{"all digits, too long for int64", strings.Repeat("9", 43), "out of the range"},
		{"the word true", "true", "not string"},
		{"the word false", "false", "not string"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := parseWithCentrifugoPassword(t, tc.password)
			if err == nil {
				t.Fatalf("the configuration parsed with password %q; this shape is supposed to break it",
					tc.password)
			}
			if !strings.Contains(err.Error(), tc.wantErrSubstring) {
				t.Errorf("password %q failed for the wrong reason:\n got: %v\nwant substring: %q",
					tc.password, err, tc.wantErrSubstring)
			}
		})
	}
}

// The sufficiency half, enumerated rather than sampled: every character the base64url
// alphabet can put first, against tails chosen to be hostile. If a leading letter ever
// stops being safe this goes red for the exact character that broke it.
func TestCentrifugoPassword_ALeadingLetterIsAlwaysSafe(t *testing.T) {
	// Tails that exercise each branch the lexer can take AFTER the first character:
	// digits, a dash, a size suffix followed by a digit, an underscore, and a value that
	// would be an integer on its own.
	tails := []string{
		safeTail,
		strings.Repeat("9", 42),
		"-" + safeTail[1:],
		"1234-5678" + safeTail[9:],
		"2p2" + safeTail[3:],
		"_-_-_-" + safeTail[6:],
		strings.Repeat("-", 42),
		strings.Repeat("_", 42),
	}

	// The claim is about EVERY letter, so count them. Without this a filter that quietly
	// stopped matching would leave the test green having asserted nothing — the loop would
	// simply run zero times.
	letters := 0
	for _, first := range base64URLAlphabet() {
		if !isASCIILetter(first) {
			continue
		}
		letters++
		for i, tail := range tails {
			t.Run(fmt.Sprintf("%c/tail%d", first, i), func(t *testing.T) {
				pw := string(first) + tail
				if err := parseWithCentrifugoPassword(t, pw); err != nil {
					t.Fatalf("a password starting with a letter must always parse; %q gave: %v", pw, err)
				}
			})
		}
	}
	if letters != 52 {
		t.Fatalf("swept %d leading letters, want all 52; the alphabet or the filter has drifted "+
			"and the sufficiency claim is no longer established", letters)
	}
}

// And the necessity half at the same granularity: with an inert tail, '-' is the only
// leading character that breaks the file. Enumerated over the whole alphabet so a future
// nats-server bump that widens the failing set is caught here rather than by a 1-in-43
// draw in CI.
func TestCentrifugoPassword_LeadingCharacterSweep(t *testing.T) {
	for _, first := range base64URLAlphabet() {
		t.Run(string(first), func(t *testing.T) {
			err := parseWithCentrifugoPassword(t, string(first)+safeTail)
			if first == '-' {
				if err == nil {
					t.Fatalf("a leading '-' must break the parse")
				}
				return
			}
			if err != nil {
				t.Fatalf("leading %q broke the parse, which the shape table does not account for: %v",
					first, err)
			}
		})
	}
}

// The reference must stay UNQUOTED, which is the opposite of the reflex. nats-server only
// treats an unquoted token as a variable (conf/lex.go isVariable, reached from lexString
// alone), so quoting it does not escape the value — it stops the lookup happening at all
// and hands the server the literal text as Centrifugo's password. No parse error, no log
// line: the bus would come up with a password the manifest publishes to anyone who can
// read git. This test is what goes red if someone "fixes" the quoting.
func TestAccounts_CentrifugoPasswordReferenceMustNotBeQuoted(t *testing.T) {
	const reference = "password: $" + centrifugoPasswordVar

	raw, err := os.ReadFile(accountsConfPath)
	if err != nil {
		t.Fatalf("read %s: %v", accountsConfPath, err)
	}
	conf := string(raw)
	if !strings.Contains(conf, reference) {
		t.Fatalf("%s no longer contains the unquoted reference %q; quoting it silently disables "+
			"interpolation — see the rest of this test", accountsConfPath, reference)
	}

	// Proof rather than assertion: build both variants and read back what the server
	// resolved the password to.
	const sentinel = "SentinelPasswordValue"
	for _, tc := range []struct {
		name, form string
		want       string
	}{
		{"unquoted, as committed", reference, sentinel},
		{"double quoted", `password: "$` + centrifugoPasswordVar + `"`, "$" + centrifugoPasswordVar},
		{"single quoted", `password: '$` + centrifugoPasswordVar + `'`, "$" + centrifugoPasswordVar},
	} {
		t.Run(tc.name, func(t *testing.T) {
			perms(t)
			saved := os.Getenv(centrifugoPasswordVar)
			t.Cleanup(func() { _ = os.Setenv(centrifugoPasswordVar, saved) })
			if err := os.Setenv(centrifugoPasswordVar, sentinel); err != nil {
				t.Fatalf("setenv: %v", err)
			}

			opts, err := processConfWithCentrifugoLine(t, conf, reference, tc.form)
			if err != nil {
				t.Fatalf("parse with %q: %v", tc.form, err)
			}
			got := centrifugoPasswordFrom(t, opts)
			if got != tc.want {
				t.Fatalf("%q resolved Centrifugo's password to %q, want %q", tc.form, got, tc.want)
			}
		})
	}
}

// An unset variable is a parse error — the accounts file says so and
// TestAccounts_MissingKeyVariableRefusesToStart proves it for the NKeys. An EMPTY variable
// is not, and this is the gap that leaves: nats-server starts, the centrifugo user exists,
// and its password is "". Anyone who can reach the bus is then Centrifugo.
//
// This is a characterisation test, not an endorsement. It cannot be closed inside
// nats-accounts.conf — the conf language has no "must be non-empty" — so it is pinned here
// so the behaviour is visible, and it is closed upstream by whatever populates the Secret.
// If a future nats-server rejects an empty password user, this test goes red and the
// workaround can be deleted.
//
// DO NOT "fix" this test to assert that an empty password is rejected. It pins nats-server's
// PARSER, which is upstream and unchanged; the fix for finding 53 prevents an empty value
// from ever reaching it and does not alter what nats-server does when one does. Inverting
// the assertion would make this test state something false about a dependency, which is
// worse than not testing it — the failure would then be silent in the direction that
// matters.
//
// The fix's owner is determined (ADR 0005, amended 2026-07-31): an initContainer on the NATS
// StatefulSet in deploy/k8s/base/infra/nats.yaml, which is the only place that can deliver
// what nats-accounts.conf's header promises — a half-provisioned cluster failing to START,
// rather than being told about it once nats-server is already serving. It needs a matching
// removal patch in deploy/k8s/overlays/local/patches/nats-local.yaml, because the local
// overlay drops `-c nats.conf` and legitimately has no password.
func TestCentrifugoPassword_EmptyVariableIsAcceptedAndAuthenticates(t *testing.T) {
	if err := parseWithCentrifugoPassword(t, ""); err != nil {
		t.Fatalf("an empty password variable is now a parse error — good news, update this test: %v", err)
	}

	// The parse is not the interesting half. Take it to the wire.
	perms(t)
	saved := os.Getenv(centrifugoPasswordVar)
	t.Cleanup(func() { _ = os.Setenv(centrifugoPasswordVar, saved) })
	if err := os.Setenv(centrifugoPasswordVar, ""); err != nil {
		t.Fatalf("setenv: %v", err)
	}

	opts, err := serverOptsFromAccountsConf(t.TempDir())
	if err != nil {
		t.Fatalf("build options with an empty password: %v", err)
	}
	srv, err := server.NewServer(opts)
	if err != nil {
		t.Fatalf("new server: %v", err)
	}
	go srv.Start()
	t.Cleanup(srv.Shutdown)
	if !srv.ReadyForConnections(20 * time.Second) {
		t.Fatal("server with an empty centrifugo password did not become ready")
	}

	nc, err := nats.Connect(srv.ClientURL(), nats.UserInfo(centrifugoUser, ""))
	if err != nil {
		t.Logf("an empty password variable no longer authenticates (%v) — good news, update this test", err)
		return
	}
	nc.Close()
	t.Log("KNOWN GAP: HERMES_CENTRIFUGO_NATS_PASSWORD=\"\" starts the server and lets anyone " +
		"connect as the centrifugo user with no credential. Unset fails closed; empty does not.")
}

// The other interpolated variables in the same file are the NKey public keys, referenced
// the same unquoted way. They are not exposed, and this says why rather than assuming it:
// an nkeys user public key is 'U' followed by base32 [A-Z2-7], so it always starts with a
// letter and never contains '-'. That is exactly the safe shape above.
func TestAccounts_NKeyVariablesCannotHitTheFailingShapes(t *testing.T) {
	for i := 0; i < 64; i++ {
		kp, err := nkeys.CreateUser()
		if err != nil {
			t.Fatalf("create user: %v", err)
		}
		pub, err := kp.PublicKey()
		if err != nil {
			t.Fatalf("public key: %v", err)
		}
		if !strings.HasPrefix(pub, "U") {
			t.Fatalf("user public key %q does not start with 'U'; the safety argument for the "+
				"unquoted $HERMES_NKEY_* references rests on that prefix", pub)
		}
		for _, r := range pub {
			if !(r >= 'A' && r <= 'Z') && !(r >= '2' && r <= '7') {
				t.Fatalf("user public key %q contains %q, outside base32 [A-Z2-7]; the "+
					"unquoted reference is only safe over that alphabet", pub, r)
			}
		}
	}

	// And the same claim through the parser, since the alphabet argument is only as good
	// as the lexer's agreement with it.
	perms(t)
	const v = "HERMES_NKEY_WORKER_SMS"
	saved := os.Getenv(v)
	t.Cleanup(func() { _ = os.Setenv(v, saved) })
	for i := 0; i < 32; i++ {
		kp, err := nkeys.CreateUser()
		if err != nil {
			t.Fatalf("create user: %v", err)
		}
		pub, err := kp.PublicKey()
		if err != nil {
			t.Fatalf("public key: %v", err)
		}
		if err := os.Setenv(v, pub); err != nil {
			t.Fatalf("setenv: %v", err)
		}
		if _, err := serverOptsFromAccountsConf(t.TempDir()); err != nil {
			t.Fatalf("public key %q broke the parse: %v", pub, err)
		}
	}
}

// --- the fixture's own password ---------------------------------------------------------

// generateCentrifugoPassword is what startPermFixture uses, and it deliberately mirrors
// cmd/natskeys's newPassword rather than the plain base64 draw that used to live there:
// 32 random bytes as 43 base64url characters, redrawn until the first character is an
// ASCII letter.
//
// The redraw is not defensive tidiness. Without it this package fails roughly once in 43
// runs, because the fixture is generating a real credential and feeding it through the
// real parser — the flake IS the production bug, observed. Keeping the value random is
// what stops a test depending on it; keeping the shape constrained is what makes the test
// exercise a password a cluster could actually be given.
//
// The 12-of-64 rejection costs about 0.3 bits of the 256, which is not a security-relevant
// amount. TestCentrifugoPassword_ALeadingLetterIsAlwaysSafe is the proof that the
// surviving shape is safe; TestNewPassword* in cmd/natskeys is the proof that the
// generator holds to it.
func generateCentrifugoPassword() (string, error) {
	for attempt := 0; attempt < 64; attempt++ {
		buf := make([]byte, 32)
		if _, err := rand.Read(buf); err != nil {
			return "", err
		}
		pw := base64.RawURLEncoding.EncodeToString(buf)
		if isASCIILetter(rune(pw[0])) {
			return pw, nil
		}
	}
	return "", fmt.Errorf("could not draw a password starting with a letter in 64 attempts")
}

// The fixture's password is a credential the cluster could really be issued, so it has to
// satisfy the same invariant. This fires if the redraw above is removed — not on 2% of
// runs, but on the assertion.
func TestCentrifugoPassword_TheFixturePasswordIsOfTheSafeShape(t *testing.T) {
	f := perms(t)

	if got := len(f.centrifugoPassword); got != 43 {
		t.Errorf("fixture password is %d characters, want 43 (32 bytes of base64url)", got)
	}
	if !isASCIILetter(rune(f.centrifugoPassword[0])) {
		t.Errorf("fixture password %q does not start with a letter, so it is not a shape "+
			"cmd/natskeys can emit", f.centrifugoPassword)
	}

	// And every draw, not just the one this run happened to make.
	for i := 0; i < 512; i++ {
		pw, err := generateCentrifugoPassword()
		if err != nil {
			t.Fatalf("draw %d: %v", i, err)
		}
		if len(pw) != 43 || !isASCIILetter(rune(pw[0])) {
			t.Fatalf("draw %d produced %q, which is not the guaranteed shape", i, pw)
		}
	}
}

// --- helpers ---------------------------------------------------------------------------

func base64URLAlphabet() []rune {
	var out []rune
	for r := 'A'; r <= 'Z'; r++ {
		out = append(out, r)
	}
	for r := 'a'; r <= 'z'; r++ {
		out = append(out, r)
	}
	for r := '0'; r <= '9'; r++ {
		out = append(out, r)
	}
	return append(out, '-', '_')
}

func isASCIILetter(r rune) bool {
	return (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z')
}

// processConfWithCentrifugoLine parses the committed accounts file with the Centrifugo
// password line replaced, so the two spellings can be compared against the same file.
func processConfWithCentrifugoLine(t *testing.T, conf, oldLine, newLine string) (*server.Options, error) {
	t.Helper()
	if strings.Count(conf, oldLine) != 1 {
		t.Fatalf("expected exactly one %q in %s, found %d", oldLine, accountsConfPath,
			strings.Count(conf, oldLine))
	}
	dir := t.TempDir()
	patched := strings.Replace(conf, oldLine, newLine, 1)
	if err := os.WriteFile(filepath.Join(dir, "accounts.conf"), []byte(patched), 0o600); err != nil {
		t.Fatalf("write accounts.conf: %v", err)
	}
	top := fmt.Sprintf("port: -1\njetstream { store_dir: %q }\ninclude \"accounts.conf\"\n",
		filepath.Join(dir, "js"))
	confPath := filepath.Join(dir, "nats.conf")
	if err := os.WriteFile(confPath, []byte(top), 0o600); err != nil {
		t.Fatalf("write nats.conf: %v", err)
	}
	return server.ProcessConfigFile(confPath)
}

// centrifugoPasswordFrom digs the resolved password out of the parsed options. The users
// live on the HERMES account, not at the top level.
func centrifugoPasswordFrom(t *testing.T, opts *server.Options) string {
	t.Helper()
	for _, u := range opts.Users {
		if u.Username == centrifugoUser {
			return u.Password
		}
	}
	t.Fatalf("no %q user in the parsed configuration", centrifugoUser)
	return ""
}
