// Copyright 2026 Hermes Notifications. Licensed under the Apache License, Version 2.0.
// See LICENSE and NOTICE in the project root for full terms and restrictions.

package main

import (
	"encoding/json"
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

// The two halves of a keypair must actually correspond, and the seed must be a user seed —
// a signing or account key would be accepted by nkeys and rejected by nats.go at connect.
func TestGenerateProducesUsableUserKeypairs(t *testing.T) {
	set, err := generate()
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if len(set) != len(roles) {
		t.Fatalf("generated %d keys for %d roles", len(set), len(roles))
	}

	seen := map[string]bool{}
	for _, k := range set {
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
	if len(got) != 2*len(roles) {
		t.Errorf("expected a public and a seed property per role, got %d entries", len(got))
	}
	for _, r := range roles {
		if got[r.PublicProperty] == "" {
			t.Errorf("missing property %q", r.PublicProperty)
		}
		if got[r.SeedProperty] == "" {
			t.Errorf("missing property %q", r.SeedProperty)
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
	for _, r := range roles {
		if !strings.Contains(out, "--from-literal="+r.ConfVar+"=") {
			t.Errorf("missing --from-literal for %s", r.ConfVar)
		}
		if !strings.Contains(out, "--from-literal="+r.Service+".nk=") {
			t.Errorf("missing --from-literal for %s.nk", r.Service)
		}
	}
}
