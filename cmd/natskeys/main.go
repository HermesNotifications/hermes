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

func generate() ([]keypair, error) {
	set := make([]keypair, 0, len(roles))
	for _, r := range roles {
		kp, err := nkeys.CreateUser()
		if err != nil {
			return nil, fmt.Errorf("create user key for %s: %w", r.Service, err)
		}
		pub, err := kp.PublicKey()
		if err != nil {
			return nil, fmt.Errorf("public key for %s: %w", r.Service, err)
		}
		seed, err := kp.Seed()
		if err != nil {
			return nil, fmt.Errorf("seed for %s: %w", r.Service, err)
		}
		set = append(set, keypair{Role: r, Public: pub, Seed: string(seed)})
	}
	return set, nil
}

func formatJSON(set []keypair) string {
	out := make(map[string]string, 2*len(set))
	for _, k := range set {
		out[k.Role.PublicProperty] = k.Public
		out[k.Role.SeedProperty] = k.Seed
	}
	// Indented so a human reviewing what they are about to upload can read it.
	b, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		panic(err) // a map[string]string cannot fail to marshal
	}
	return string(b) + "\n"
}

func formatKubectl(set []keypair) string {
	var b strings.Builder
	b.WriteString("kubectl -n hermes create secret generic nats-nkeys \\\n")
	for _, k := range set {
		fmt.Fprintf(&b, "  --from-literal=%s=%s \\\n", k.Role.ConfVar, k.Public)
	}
	for i, k := range set {
		cont := " \\"
		if i == len(set)-1 {
			cont = ""
		}
		fmt.Fprintf(&b, "  --from-literal=%s.nk=%s%s\n", k.Role.Service, k.Seed, cont)
	}
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
