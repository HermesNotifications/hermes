// Copyright Hermes Notifications
// SPDX-License-Identifier: Apache-2.0
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

package apikeybootstrap

import (
	"context"
	"errors"
	"testing"

	"github.com/hermesnotifications/hermes/internal/auth"
)

const hmacSecret = "test-hmac-secret"

// fakeKeys is the api_keys table, as a map. Hand-written rather than mocked so the
// ON CONFLICT DO NOTHING semantics the real store must have are visible here.
type fakeKeys struct {
	rows        map[string]row
	insertErr   error
	existsErr   error
	insertCalls int
}

type row struct {
	hash        string
	name        string
	permissions []string
}

func newFakeKeys() *fakeKeys { return &fakeKeys{rows: map[string]row{}} }

func (f *fakeKeys) APIKeyExists(_ context.Context, keyID string) (bool, error) {
	if f.existsErr != nil {
		return false, f.existsErr
	}
	_, ok := f.rows[keyID]
	return ok, nil
}

func (f *fakeKeys) InsertAPIKey(_ context.Context, keyID, keyHash, name string, permissions []string) error {
	f.insertCalls++
	if f.insertErr != nil {
		return f.insertErr
	}
	if _, ok := f.rows[keyID]; ok {
		return nil // ON CONFLICT (id) DO NOTHING
	}
	f.rows[keyID] = row{hash: keyHash, name: name, permissions: permissions}
	return nil
}

type fakeSecrets struct {
	data        map[string]map[string][]byte
	getErr      error
	createErr   error
	createCalls int
}

func newFakeSecrets() *fakeSecrets {
	return &fakeSecrets{data: map[string]map[string][]byte{}}
}

func (f *fakeSecrets) GetSecret(_ context.Context, name string) (map[string][]byte, bool, error) {
	if f.getErr != nil {
		return nil, false, f.getErr
	}
	d, ok := f.data[name]
	return d, ok, nil
}

func (f *fakeSecrets) CreateSecret(_ context.Context, name string, data map[string][]byte) error {
	f.createCalls++
	if f.createErr != nil {
		return f.createErr
	}
	if _, ok := f.data[name]; ok {
		return ErrAlreadyExists
	}
	f.data[name] = data
	return nil
}

func opts() Options {
	return Options{SecretName: "hermes-bootstrap", KeyName: "Bootstrap", HMACSecret: hmacSecret}
}

func TestCreatesKeyAndSecretOnAFreshInstall(t *testing.T) {
	keys, secrets := newFakeKeys(), newFakeSecrets()

	out, err := Run(context.Background(), keys, secrets, opts())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if out.Action != "created" {
		t.Errorf("action = %q, want created", out.Action)
	}

	raw := string(secrets.data["hermes-bootstrap"][SecretKey])
	if raw == "" {
		t.Fatal("secret holds no key")
	}
	keyID, secret, err := auth.ParseAPIKey(raw)
	if err != nil {
		t.Fatalf("the key written to the Secret does not parse: %v", err)
	}
	if keyID != out.KeyID {
		t.Errorf("secret holds %s, outcome reported %s", keyID, out.KeyID)
	}
	// The whole point: the stored hash must verify against the raw key in the Secret.
	if !auth.HMACVerifyAPIKey(secret, keys.rows[keyID].hash, hmacSecret) {
		t.Error("stored hash does not verify the key in the Secret; the key is unusable")
	}
}

func TestGrantsAllPermissionsIncludingApiKeysManage(t *testing.T) {
	// DefaultPermissions omits apikeys:manage, which would leave the bootstrap key unable to
	// mint the narrow key meant to replace it -- so it could never be retired.
	keys, secrets := newFakeKeys(), newFakeSecrets()

	out, err := Run(context.Background(), keys, secrets, opts())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	var found bool
	for _, p := range keys.rows[out.KeyID].permissions {
		if p == auth.PermAPIKeysManage {
			found = true
		}
	}
	if !found {
		t.Errorf("permissions = %v, missing %s", keys.rows[out.KeyID].permissions, auth.PermAPIKeysManage)
	}
}

func TestSecondRunIsANoOp(t *testing.T) {
	// `helm upgrade` re-runs the Job on every revision. A second key per upgrade would be a
	// slow leak of live admin credentials.
	keys, secrets := newFakeKeys(), newFakeSecrets()

	first, err := Run(context.Background(), keys, secrets, opts())
	if err != nil {
		t.Fatalf("first Run: %v", err)
	}
	second, err := Run(context.Background(), keys, secrets, opts())
	if err != nil {
		t.Fatalf("second Run: %v", err)
	}

	if second.Action != "present" {
		t.Errorf("action = %q, want present", second.Action)
	}
	if second.KeyID != first.KeyID {
		t.Errorf("key id changed between runs: %s -> %s", first.KeyID, second.KeyID)
	}
	if len(keys.rows) != 1 {
		t.Errorf("%d keys in the table, want 1", len(keys.rows))
	}
}

func TestReadoptsTheSavedKeyWhenTheDatabaseLostItsRow(t *testing.T) {
	// Restore a backup taken before the key was created and the Secret still holds a key the
	// database has never heard of. Minting a new one would silently invalidate the credential
	// the operator has in their password manager and in every CI system.
	keys, secrets := newFakeKeys(), newFakeSecrets()

	first, err := Run(context.Background(), keys, secrets, opts())
	if err != nil {
		t.Fatalf("first Run: %v", err)
	}
	savedKey := string(secrets.data["hermes-bootstrap"][SecretKey])

	keys.rows = map[string]row{} // the restore

	out, err := Run(context.Background(), keys, secrets, opts())
	if err != nil {
		t.Fatalf("Run after restore: %v", err)
	}
	if out.Action != "readopted" {
		t.Errorf("action = %q, want readopted", out.Action)
	}
	if out.KeyID != first.KeyID {
		t.Errorf("key id changed: %s -> %s", first.KeyID, out.KeyID)
	}
	if got := string(secrets.data["hermes-bootstrap"][SecretKey]); got != savedKey {
		t.Error("the Secret was rewritten; the operator's saved key would have stopped working")
	}
	_, secret, _ := auth.ParseAPIKey(savedKey)
	if !auth.HMACVerifyAPIKey(secret, keys.rows[out.KeyID].hash, hmacSecret) {
		t.Error("re-inserted hash does not verify the saved key")
	}
}

func TestSuppliedKeyIsInsertedAndNoSecretIsWritten(t *testing.T) {
	// The escape hatch for clusters whose policy forbids workloads creating Secrets: the key
	// arrives from ESO/Vault/SOPS and the Job only ensures the row.
	keys, secrets := newFakeKeys(), newFakeSecrets()
	raw, keyID, err := auth.GenerateAPIKey("")
	if err != nil {
		t.Fatalf("GenerateAPIKey: %v", err)
	}

	o := opts()
	o.ExistingKey = raw
	out, err := Run(context.Background(), keys, secrets, o)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if out.Action != "supplied" {
		t.Errorf("action = %q, want supplied", out.Action)
	}
	if out.KeyID != keyID {
		t.Errorf("key id = %s, want %s", out.KeyID, keyID)
	}
	if secrets.createCalls != 0 {
		t.Error("wrote a Secret despite the key being supplied; the RBAC to do so should not be needed")
	}
	if _, ok := keys.rows[keyID]; !ok {
		t.Error("supplied key was not inserted, so it would not authenticate")
	}
}

func TestALostRaceAdoptsTheWinnersSecret(t *testing.T) {
	keys, secrets := newFakeKeys(), newFakeSecrets()
	secrets.createErr = ErrAlreadyExists

	out, err := Run(context.Background(), keys, secrets, opts())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if out.Action != "raced" {
		t.Errorf("action = %q, want raced", out.Action)
	}
}

func TestRefusesASecretWithNoKeyInIt(t *testing.T) {
	// Creating a second key beside it would leave two live admin credentials where the
	// operator believes there is one.
	keys, secrets := newFakeKeys(), newFakeSecrets()
	secrets.data["hermes-bootstrap"] = map[string][]byte{"unrelated": []byte("x")}

	_, err := Run(context.Background(), keys, secrets, opts())
	if err == nil {
		t.Fatal("expected an error")
	}
	if secrets.createCalls != 0 {
		t.Error("overwrote or added beside an existing Secret")
	}
}

func TestRefusesAnUnparseableStoredKey(t *testing.T) {
	keys, secrets := newFakeKeys(), newFakeSecrets()
	secrets.data["hermes-bootstrap"] = map[string][]byte{SecretKey: []byte("not-a-key")}

	if _, err := Run(context.Background(), keys, secrets, opts()); err == nil {
		t.Fatal("expected an error")
	}
}

func TestRefusesAnEmptyHMACSecret(t *testing.T) {
	// The hash would be computed against "" and no key would ever verify.
	o := opts()
	o.HMACSecret = ""

	if _, err := Run(context.Background(), newFakeKeys(), newFakeSecrets(), o); err == nil {
		t.Fatal("expected an error")
	}
}

func TestSurfacesStoreFailuresRatherThanCreatingASecondKey(t *testing.T) {
	keys, secrets := newFakeKeys(), newFakeSecrets()
	keys.insertErr = errors.New("connection refused")

	_, err := Run(context.Background(), keys, secrets, opts())
	if err == nil {
		t.Fatal("expected an error")
	}
	if secrets.createCalls != 0 {
		t.Error("wrote the Secret despite the insert failing; the key would not authenticate")
	}
}

func TestDoesNotWriteTheSecretBeforeTheRowExists(t *testing.T) {
	// Ordering matters on the fresh-install path: a Secret written before a failed insert
	// leaves a key that looks issued and authenticates against nothing, and the next run
	// takes the readopt path and hides the original failure.
	keys, secrets := newFakeKeys(), newFakeSecrets()
	keys.insertErr = errors.New("db down")

	_, _ = Run(context.Background(), keys, secrets, opts())

	if len(secrets.data) != 0 {
		t.Error("a Secret exists for a key with no database row")
	}
}
