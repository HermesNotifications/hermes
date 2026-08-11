// Copyright Hermes Notifications
// SPDX-License-Identifier: Apache-2.0
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

package messaging_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hermes-notifications/hermes/internal/messaging"
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// ADR 0005 phase 3. These are the client-side halves that need no server: that the
// development opt-out is a real no-op, and that a misconfigured seed fails loudly and
// names the file. The permissions themselves are proven against a real NATS server in
// permissions_test.go.

// The development path. `make infra-up` runs NATS with no accounts and no auth, and
// HERMES_NATS_NKEY_SEED is unset there, so an empty seed path must leave the connection
// exactly as it was before this option existed — the same contract WithCABundle has.
func TestWithIdentity_EmptySeedPathLeavesPlaintextWorking(t *testing.T) {
	srv := newPlaintextTestServer(t)

	client, err := messaging.Connect("nats://localhost:"+srv.port,
		messaging.WithIdentity("hermes-send", ""))
	if err != nil {
		t.Fatalf("an empty seed path must not change a plaintext connection, got: %v", err)
	}
	client.Close()
}

// A seed that is configured but not mounted is a deployment mistake. It must fail the
// connection — never fall back to an anonymous one — and the error must name the path or
// the operator is guessing which of nine Secrets did not land.
func TestWithIdentity_MissingSeedNamesTheFile(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "not-mounted", "seed.nk")

	client, err := messaging.Connect("nats://localhost:4222",
		messaging.WithIdentity("hermes-send", missing))
	if err == nil {
		client.Close()
		t.Fatal("expected a missing NKey seed to fail the connection")
	}
	if !strings.Contains(err.Error(), missing) {
		t.Errorf("error must name the missing seed %q, got: %v", missing, err)
	}
}

// A file that exists but is not a user seed (a public key pasted where a seed belongs,
// say) must also fail at connect rather than at first publish.
func TestWithIdentity_MalformedSeedIsRejected(t *testing.T) {
	path := filepath.Join(t.TempDir(), "seed.nk")
	writeFile(t, path, "not-an-nkey-seed\n")

	client, err := messaging.Connect("nats://localhost:4222",
		messaging.WithIdentity("hermes-send", path))
	if err == nil {
		client.Close()
		t.Fatal("expected a malformed NKey seed to fail the connection")
	}
	if !strings.Contains(err.Error(), path) {
		t.Errorf("error must name the seed file %q, got: %v", path, err)
	}
}

// The seed is only half of an identity. The other half is the reply inbox: JetStream
// delivers pulled messages and publish acks to a subject the client picks, so if every
// service could subscribe to _INBOX.> a compromised worker would receive copies of every
// other service's fetched messages — the whole subject scheme bypassed through the reply
// path. WithIdentity therefore confines the connection to _INBOX.<service>, which is what
// the per-user subscribe permission can then be narrowed to.
func TestWithIdentity_ConfinesInboxToTheService(t *testing.T) {
	if got, want := messaging.InboxPrefixForTest("hermes-worker-email"), "_INBOX.hermes-worker-email"; got != want {
		t.Errorf("inbox prefix = %q, want %q", got, want)
	}
	if got := messaging.InboxPrefixForTest(""); got != "" {
		t.Errorf("an unnamed service must not get a custom inbox prefix, got %q", got)
	}
}
