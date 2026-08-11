// Copyright Hermes Notifications
// SPDX-License-Identifier: Apache-2.0
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

package main

import (
	"encoding/json"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/nats-io/nkeys"
)

const accountsConf = "../../deploy/k8s/base/infra/nats-accounts.conf"

// Every role this tool generates a key for must have a user in the permissions file, and
// vice versa. Otherwise an operator provisions a Secret the server does not read, or the
// server refuses to start on a variable nobody generated — both of which look like a broken
// bus rather than a provisioning mistake.
func TestRolesMatchTheAccountsConfiguration(t *testing.T) {
	raw, err := os.ReadFile(accountsConf)
	if err != nil {
		t.Fatalf("read %s: %v", accountsConf, err)
	}
	conf := string(raw)

	for _, r := range roles {
		if !strings.Contains(conf, "nkey: $"+r.ConfVar) {
			t.Errorf("%s generates $%s but %s has no user using it", r.Service, r.ConfVar, accountsConf)
		}
		if !strings.Contains(conf, `"_INBOX.`+r.Service+`.>"`) {
			t.Errorf("%s has no matching inbox permission in %s", r.Service, accountsConf)
		}
	}

	if got, want := strings.Count(conf, "nkey: $HERMES_NKEY_"), len(roles); got != want {
		t.Errorf("%s declares %d users but this tool generates %d keys", accountsConf, got, want)
	}
}

// ADR 0005 phase 4. Centrifugo is a password user because centrifugo:v5 can present no NKey,
// so this tool has to generate a password too — and the accounts file has to be expecting it
// under exactly this variable, or nats-server refuses to start on an unresolved $VARIABLE.
func TestCentrifugoPasswordVariableMatchesTheAccountsConfiguration(t *testing.T) {
	raw, err := os.ReadFile(accountsConf)
	if err != nil {
		t.Fatalf("read %s: %v", accountsConf, err)
	}
	conf := string(raw)

	if !strings.Contains(conf, "password: $"+centrifugoConfVar) {
		t.Errorf("%s has no password user reading $%s", accountsConf, centrifugoConfVar)
	}
	if !strings.Contains(conf, "user: "+centrifugoUser) {
		t.Errorf("%s declares no %q user", accountsConf, centrifugoUser)
	}
}

// The password and the URL that carries it are generated together and must agree. They end up
// in two different Kubernetes Secret keys read by two different processes — nats-server reads
// the password, Centrifugo parses the URL — and nothing in the cluster cross-checks them, so
// this is the only place the agreement is enforced.
func TestCentrifugoURLCarriesTheGeneratedPassword(t *testing.T) {
	creds, err := generate()
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if creds.CentrifugoPassword == "" {
		t.Fatal("no Centrifugo password generated")
	}

	u, err := url.Parse(creds.CentrifugoURL())
	if err != nil {
		t.Fatalf("the generated URL does not parse: %v", err)
	}
	if u.Scheme != "tls" {
		t.Errorf("scheme is %q; a nats:// URL would leave the bus unencrypted", u.Scheme)
	}
	if got := u.User.Username(); got != centrifugoUser {
		t.Errorf("URL user is %q, want %q", got, centrifugoUser)
	}
	pass, set := u.User.Password()
	if !set {
		t.Fatal("the URL carries no password; Centrifugo has no other way to authenticate")
	}
	// Round-tripping through url.Parse is the assertion that matters: if the generated
	// password needed percent-encoding, the value nats-server compares against would differ
	// from the value Centrifugo sends, and the only symptom is an authorization violation.
	if pass != creds.CentrifugoPassword {
		t.Errorf("URL password %q does not match the generated password %q", pass, creds.CentrifugoPassword)
	}
}

// A password short enough to guess is worse than no password, because it looks like security.
func TestCentrifugoPasswordIsLongAndFreshEachRun(t *testing.T) {
	first, err := generate()
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	second, err := generate()
	if err != nil {
		t.Fatalf("generate: %v", err)
	}

	if len(first.CentrifugoPassword) < 32 {
		t.Errorf("password is %d characters; want at least 32", len(first.CentrifugoPassword))
	}
	if first.CentrifugoPassword == second.CentrifugoPassword {
		t.Error("two runs produced the same password, so it is not random")
	}
}

// The two halves of a keypair must actually correspond, and the seed must be a user seed —
// a signing or account key would be accepted by nkeys and rejected by nats.go at connect.
func TestGenerateProducesUsableUserKeypairs(t *testing.T) {
	set, err := generate()
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if len(set.Keys) != len(roles) {
		t.Fatalf("generated %d keys for %d roles", len(set.Keys), len(roles))
	}

	seen := map[string]bool{}
	for _, k := range set.Keys {
		if !nkeys.IsValidPublicUserKey(k.Public) {
			t.Errorf("%s: %q is not a public user key", k.Role.Service, k.Public)
		}
		kp, err := nkeys.FromSeed([]byte(k.Seed))
		if err != nil {
			t.Fatalf("%s: seed does not load: %v", k.Role.Service, err)
		}
		pub, err := kp.PublicKey()
		if err != nil {
			t.Fatalf("%s: public key: %v", k.Role.Service, err)
		}
		if pub != k.Public {
			t.Errorf("%s: seed derives %s, not the reported %s", k.Role.Service, pub, k.Public)
		}
		if seen[k.Public] {
			t.Errorf("%s reuses a key issued to another service", k.Role.Service)
		}
		seen[k.Public] = true
	}
}

// The JSON form seeds the remote secret the ExternalSecrets read, so its property names are
// a contract with deploy/k8s/overlays/*/external-secrets.yaml.
func TestFormatJSONUsesThePropertyNamesExternalSecretsExpect(t *testing.T) {
	set, err := generate()
	if err != nil {
		t.Fatalf("generate: %v", err)
	}

	var got map[string]string
	if err := json.Unmarshal([]byte(formatJSON(set)), &got); err != nil {
		t.Fatalf("output is not a JSON object: %v", err)
	}
	// A public and a seed property per role, plus Centrifugo's password and the URL that
	// carries it. Counted exactly so a property added here without a matching remoteRef in
	// the overlays — or left behind after one is removed — shows up as a failure.
	if len(got) != 2*len(roles)+2 {
		t.Errorf("expected %d properties, got %d: %v", 2*len(roles)+2, len(got), got)
	}
	for _, r := range roles {
		if got[r.PublicProperty] == "" {
			t.Errorf("missing property %q", r.PublicProperty)
		}
		if got[r.SeedProperty] == "" {
			t.Errorf("missing property %q", r.SeedProperty)
		}
	}
	if got[centrifugoPasswordProperty] == "" {
		t.Errorf("missing property %q", centrifugoPasswordProperty)
	}
	if got[centrifugoURLProperty] == "" {
		t.Errorf("missing property %q", centrifugoURLProperty)
	}
	// The URL property has to contain the password property, or the two Secret keys the
	// cluster ends up with disagree.
	if !strings.Contains(got[centrifugoURLProperty], got[centrifugoPasswordProperty]) {
		t.Errorf("%s does not carry %s", centrifugoURLProperty, centrifugoPasswordProperty)
	}
}

// The property names are only worth anything if the ExternalSecrets actually ask for them.
func TestExternalSecretsReferenceEveryGeneratedProperty(t *testing.T) {
	for _, overlay := range []string{
		"../../deploy/k8s/overlays/staging/external-secrets.yaml",
		"../../deploy/k8s/overlays/production/external-secrets.yaml",
	} {
		raw, err := os.ReadFile(overlay)
		if err != nil {
			t.Fatalf("read %s: %v", overlay, err)
		}
		conf := string(raw)
		want := make([]string, 0, 2*len(roles)+2)
		for _, r := range roles {
			want = append(want, r.PublicProperty, r.SeedProperty)
		}
		want = append(want, centrifugoPasswordProperty, centrifugoURLProperty)
		for _, property := range want {
			if !strings.Contains(conf, "property: "+property) {
				t.Errorf("%s has no remoteRef for property %q", overlay, property)
			}
		}
	}
}

// The kubectl form is what an operator runs by hand, so the Secret keys have to be the ones
// the manifests project: the configuration variable for the server, <service>.nk for the pod.
func TestFormatKubectlUsesTheSecretKeysTheManifestsProject(t *testing.T) {
	set, err := generate()
	if err != nil {
		t.Fatalf("generate: %v", err)
	}

	out := formatKubectl(set)
	if !strings.Contains(out, "create secret generic nats-nkeys") {
		t.Errorf("output does not create the nats-nkeys Secret:\n%s", out)
	}
	if !strings.Contains(out, "--from-literal="+centrifugoConfVar+"=") {
		t.Errorf("missing --from-literal for %s", centrifugoConfVar)
	}
	// Centrifugo reads the URL from hermes-secrets, not nats-nkeys, so the operator path has
	// to touch both Secrets or the bus works for the six services and not for Centrifugo.
	if !strings.Contains(out, "HERMES_CENTRIFUGO_NATS_URL") {
		t.Errorf("output never sets HERMES_CENTRIFUGO_NATS_URL:\n%s", out)
	}
	for _, r := range roles {
		if !strings.Contains(out, "--from-literal="+r.ConfVar+"=") {
			t.Errorf("missing --from-literal for %s", r.ConfVar)
		}
		if !strings.Contains(out, "--from-literal="+r.Service+".nk=") {
			t.Errorf("missing --from-literal for %s.nk", r.Service)
		}
	}
}
