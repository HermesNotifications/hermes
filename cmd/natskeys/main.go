// Copyright 2026 Hermes Notifications. Licensed under the Apache License, Version 2.0.
// See LICENSE and NOTICE in the project root for full terms and restrictions.

// Command natskeys generates the NATS NKey set that ADR 0005 phase 3 needs: one user
// keypair per service, printed in a form you can hand straight to Kubernetes or to AWS
// Secrets Manager.
//
//	# a Secret you can apply to a cluster without cert-manager or ESO (kind, k3s, k3d)
//	go run ./cmd/natskeys | sh
//
//	# the payload the staging/production ExternalSecrets read
//	go run ./cmd/natskeys -format json |
//	  aws secretsmanager put-secret-value --secret-id hermes/staging/nats-nkeys \
//	    --secret-string file:///dev/stdin
//
// Each key's PUBLIC half goes into the nats-server's environment, where
// deploy/k8s/base/infra/nats-accounts.conf resolves it as $HERMES_NKEY_<ROLE> and attaches
// that service's subject permissions. The SEED is the private half and is mounted into that
// one service's pod as /etc/nats-nkey/seed.nk. They are generated together because they are
// only useful together: rotating one half alone locks the service out of the bus.
//
// The seeds are printed to stdout in the clear, which is the point of a tool you run
// deliberately rather than a step buried in a deployment. Do not commit the output.
package main

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/nats-io/nkeys"
)

// role ties one service to the three names its credential is known by. All three appear in
// checked-in manifests, so they are a contract, not an implementation detail:
//
//	ConfVar         the $VARIABLE in nats-accounts.conf, and the Secret key the NATS
//	                StatefulSet reads as an environment variable
//	Service         the Deployment name; also the Secret key "<Service>.nk" holding the
//	                seed, and the _INBOX.<Service> prefix messaging.WithIdentity installs
//	*Property       the property names inside the remote AWS secret that the staging and
//	                production ExternalSecrets map from
type role struct {
	Service        string
	ConfVar        string
	PublicProperty string
	SeedProperty   string
}

var roles = []role{
	{"hermes-send", "HERMES_NKEY_SEND", "send_public", "send_seed"},
	// ADR 0005 phase 4. Not a long-running service: cmd/natsprovision runs as a Job, declares
	// the streams and exits. It gets a keypair like any other identity because it is the only
	// one holding STREAM.CREATE and STREAM.UPDATE — the point of the phase is that no service
	// shares it. Must match messaging.ProvisionerService.
	{"hermes-natsprovision", "HERMES_NKEY_PROVISION", "provision_public", "provision_seed"},
	{"hermes-dispatch", "HERMES_NKEY_DISPATCH", "dispatch_public", "dispatch_seed"},
	{"hermes-worker-email", "HERMES_NKEY_WORKER_EMAIL", "worker_email_public", "worker_email_seed"},
	{"hermes-worker-sms", "HERMES_NKEY_WORKER_SMS", "worker_sms_public", "worker_sms_seed"},
	{"hermes-worker-inbox", "HERMES_NKEY_WORKER_INBOX", "worker_inbox_public", "worker_inbox_seed"},
	{"hermes-worker-events", "HERMES_NKEY_WORKER_EVENTS", "worker_events_public", "worker_events_seed"},
}

type keypair struct {
	Role   role
	Public string
	Seed   string
}

// ADR 0005 phase 4. Centrifugo is the one bus client that is not an NKey user. centrifugo:v5
// (5.4.9) exposes no NKey setting in any form — verified against the image's registered
// configuration keys and its full --help — and the only credential channel it offers is the
// userinfo of nats_url. So it authenticates with a password, which NATS accounts support
// alongside NKey users.
const (
	centrifugoUser    = "centrifugo"
	centrifugoConfVar = "HERMES_CENTRIFUGO_NATS_PASSWORD"
	// centrifugoNATSAddress must match HERMES_NATS_URL in base/kustomization.yaml: the
	// scheme is what makes Centrifugo's connection encrypted, and a nats:// URL here would
	// be a silent downgrade to plaintext against a server that requires TLS.
	centrifugoNATSAddress = "tls://nats:4222"

	centrifugoPasswordProperty = "centrifugo_password"
	centrifugoURLProperty      = "centrifugo_nats_url"
)

// credentials is everything nats-server and its clients need for one environment. The
// Centrifugo password is here rather than in a separate tool because it has to be generated in
// the same breath as the URL that carries it — see CentrifugoURL.
type credentials struct {
	Keys               []keypair
	CentrifugoPassword string
}

// CentrifugoURL is the value Centrifugo reads as CENTRIFUGO_NATS_URL. It embeds the same
// password nats-server is given, which is the whole reason both are emitted by one command: a
// password stored in two places by hand drifts, and the only symptom of drift is an
// authorization violation at startup with nothing pointing at the cause.
func (c credentials) CentrifugoURL() string {
	// The password is base64url, whose alphabet is entirely RFC 3986 unreserved characters,
	// so it needs no percent-encoding. That is not a detail to leave implicit: a password
	// requiring encoding would reach nats-server differently from the way Centrifugo sends
	// it. TestCentrifugoURLCarriesTheGeneratedPassword round-trips this through url.Parse.
	scheme, host, _ := strings.Cut(centrifugoNATSAddress, "://")
	return scheme + "://" + centrifugoUser + ":" + c.CentrifugoPassword + "@" + host
}

// newPassword returns 32 random bytes rendered as 43 base64url characters.
func newPassword() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("read random bytes: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

func generate() (credentials, error) {
	set := make([]keypair, 0, len(roles))
	for _, r := range roles {
		kp, err := nkeys.CreateUser()
		if err != nil {
			return credentials{}, fmt.Errorf("create user key for %s: %w", r.Service, err)
		}
		pub, err := kp.PublicKey()
		if err != nil {
			return credentials{}, fmt.Errorf("public key for %s: %w", r.Service, err)
		}
		seed, err := kp.Seed()
		if err != nil {
			return credentials{}, fmt.Errorf("seed for %s: %w", r.Service, err)
		}
		set = append(set, keypair{Role: r, Public: pub, Seed: string(seed)})
	}
	password, err := newPassword()
	if err != nil {
		return credentials{}, fmt.Errorf("centrifugo password: %w", err)
	}
	return credentials{Keys: set, CentrifugoPassword: password}, nil
}

func formatJSON(creds credentials) string {
	out := make(map[string]string, 2*len(creds.Keys)+2)
	for _, k := range creds.Keys {
		out[k.Role.PublicProperty] = k.Public
		out[k.Role.SeedProperty] = k.Seed
	}
	// Both Centrifugo properties live in this one remote secret even though the
	// ExternalSecrets project them into different target Secrets — nats-nkeys for the
	// password the server reads, hermes-secrets for the URL Centrifugo reads. Keeping them
	// in one remote artifact is what makes "generated together" survive into deployment.
	out[centrifugoPasswordProperty] = creds.CentrifugoPassword
	out[centrifugoURLProperty] = creds.CentrifugoURL()
	// Indented so a human reviewing what they are about to upload can read it.
	b, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		panic(err) // a map[string]string cannot fail to marshal
	}
	return string(b) + "\n"
}

func formatKubectl(creds credentials) string {
	var b strings.Builder
	b.WriteString("kubectl -n hermes create secret generic nats-nkeys \\\n")
	for _, k := range creds.Keys {
		fmt.Fprintf(&b, "  --from-literal=%s=%s \\\n", k.Role.ConfVar, k.Public)
	}
	fmt.Fprintf(&b, "  --from-literal=%s=%s \\\n", centrifugoConfVar, creds.CentrifugoPassword)
	for _, k := range creds.Keys {
		fmt.Fprintf(&b, "  --from-literal=%s.nk=%s \\\n", k.Role.Service, k.Seed)
	}
	// The URL is a second Secret because Centrifugo's Deployment reads it from
	// hermes-secrets, not from nats-nkeys. Printed as a patch rather than a create so it
	// does not clobber the other keys hermes-secrets already holds.
	fmt.Fprintf(&b, "  --dry-run=client -o yaml | kubectl apply -f -\n\n")
	fmt.Fprintf(&b, "kubectl -n hermes patch secret hermes-secrets --type merge \\\n")
	fmt.Fprintf(&b, "  -p '{\"stringData\":{\"HERMES_CENTRIFUGO_NATS_URL\":\"%s\"}}'\n", creds.CentrifugoURL())
	return b.String()
}

func main() {
	format := flag.String("format", "kubectl", "output format: kubectl or json")
	flag.Parse()

	set, err := generate()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	switch *format {
	case "kubectl":
		fmt.Print(formatKubectl(set))
	case "json":
		fmt.Print(formatJSON(set))
	default:
		fmt.Fprintf(os.Stderr, "unknown -format %q; want kubectl or json\n", *format)
		os.Exit(1)
	}
}
