// Copyright Hermes Notifications
// SPDX-License-Identifier: Apache-2.0
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

package config

import (
	"fmt"
	"log"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

type EmailConfig struct {
	Provider     string
	From         string
	SMTPHost     string
	SMTPPort     int
	SMTPUsername string
	SMTPPassword string
	SESRegion    string
	LayoutPath   string
}

type Config struct {
	HTTPPort    int
	DatabaseURL string
	NATSUrl     string
	// NATSCABundlePath is a PEM file of the roots that verify the NATS server
	// certificate. cert-manager signs nats.hermes.svc with a private CA (ADR 0005
	// phase 2) that is in no system trust store, so this is how the connection can be
	// verified. Empty means "use the system pool", which is the local development
	// path — `make infra-up` runs NATS without TLS at all.
	NATSCABundlePath string
	// NATSNKeySeedPath is the file holding this service's NKey seed — the private half of
	// the identity that selects its user, and therefore its subject permissions, in
	// deploy/k8s/base/infra/nats-accounts.conf (ADR 0005 phase 3). Empty means "connect
	// anonymously", which only works against a server with no accounts: `make infra-up`
	// and the local overlay. It is not a silent downgrade — a server that defines accounts
	// answers an unauthenticated CONNECT with an authorization violation, so a deployment
	// that forgets the seed fails to start rather than running with fewer rights.
	//
	// nats.go re-reads the file on every authentication challenge, so a rotated Secret is
	// picked up on the next reconnect without a restart.
	NATSNKeySeedPath string

	// NATSStreamReplicas is the JetStream replication factor cmd/natsprovision declares the
	// streams with. Only that binary reads it; every other service verifies rather than
	// declares (ADR 0005 phase 4).
	//
	// The default of 1 matches a single-node bus — `make infra-up`, the local overlay,
	// staging. A clustered deployment must set 3, and until it does, losing the one node
	// holding a stream takes the pipeline down despite the PDB and the anti-affinity rules
	// that exist to prevent exactly that.
	NATSStreamReplicas int
	// NATSStreamReplicasAllowChange permits natsprovision to alter the replication factor of a
	// stream that already exists. Off by default because that migration moves the whole stream
	// between peers; see messaging.SetupStreams.
	NATSStreamReplicasAllowChange bool

	// ShutdownDrainDelay is how long a service keeps serving after flipping /readyz to 503,
	// giving kube-proxy and the ingress time to stop routing to it. In-process because every
	// image is FROM scratch and so cannot run a preStop exec hook.
	ShutdownDrainDelay time.Duration
	// ShutdownTimeout bounds the graceful HTTP shutdown that follows.
	ShutdownTimeout time.Duration
	// NATSDrainTimeout bounds waiting for in-flight message handlers. Set to 0 on services
	// that hold no NATS client, so the shutdown budget reflects what actually runs.
	NATSDrainTimeout time.Duration

	// DatabaseMaxConns bounds this service's Postgres pool.
	//
	// docs/observability/runbooks/db-pool-saturated.md told operators to set
	// HERMES_DATABASE_MAX_CONNS long before it existed. It exists now. A fixed default rather
	// than a CPU-derived one, because the derived value came from the *node's* core count and
	// so varied with wherever the scheduler happened to place the pod.
	DatabaseMaxConns int
	// DatabaseMinConns is the warm pool kept open between requests.
	DatabaseMinConns int
	// DatabaseMaxConnLifetime recycles a connection after this long, with jitter applied in
	// internal/database so a pool that filled during a burst does not replace every connection
	// at the same instant later.
	DatabaseMaxConnLifetime time.Duration
	// DatabaseMaxConnIdleTime closes connections idle for this long.
	DatabaseMaxConnIdleTime time.Duration

	// RedisPoolSize bounds the go-redis connection pool. Its own default is 10 × GOMAXPROCS,
	// which is node-derived for the same reason the pgx one was.
	RedisPoolSize int
	// RedisTimeout bounds a single Redis command.
	//
	// go-redis defaults to 3 seconds, and that default is the hazard: every inbox read consults
	// the unread-count cache, so a Redis hiccup means each request blocks three seconds before
	// falling back to Postgres, in-flight requests pile up, and the HTTP tier collapses because
	// of a dependency it can serve perfectly well without. Failing fast to the database is the
	// whole point of having a fallback.
	RedisTimeout time.Duration

	RedisURL           string
	JWTSecret          string
	CentrifugoAPIURL   string
	CentrifugoAPIKey   string
	Email              EmailConfig
	SMSWebhookURL      string
	APIKeyHMACSecret   string
	EventRetentionDays int

	// DispatchConcurrency is the size of the dispatch worker pool — how many
	// notification.send messages are processed in parallel. Distinct notifications
	// are independent (per-notification status rollup is monotonic downstream), so
	// they can be processed in parallel to lift dispatch throughput.
	DispatchConcurrency int
	// DispatchPrefetch is the dispatch fetcher's in-flight buffer (PullMaxMessages)
	// that feeds the worker pool. Decouples fetching from processing so the pull
	// pipeline stays full without hoarding the backlog. Tunable for load-test sweeps.
	DispatchPrefetch int

	// NATSStreamMaxBytes bounds each JetStream work stream on disk. Only the
	// provisioner Job's value takes effect — under ADR 0005 phase 4 it is the one
	// identity permitted to create or update a stream. Zero keeps
	// messaging.DefaultStreamMaxBytes. Size it against the NATS volume: three
	// work streams plus the 1 GiB DLQ must fit with room to spare.
	NATSStreamMaxBytes int64

	// RateLimit* tune the per-service HTTP rate limiter. Each service ships its
	// own defaults, so these are overrides: a zero Burst or PerSecond keeps the
	// service default, and RateLimitEnabled=false turns limiting off entirely.
	// Every HTTP service reads the same three variables, and because each runs as
	// its own Deployment they are set per service rather than fleet-wide.
	//
	// Without RateLimitDistributedEnabled these are enforced per replica, so the
	// cluster-wide ceiling is the configured rate times the replica count. With
	// it, the check runs in Redis and these become the cluster-wide numbers.
	// See docs/configuration.md.
	RateLimitEnabled   bool
	RateLimitBurst     int
	RateLimitPerSecond int

	// RateLimitIP* bound requests per source address BEFORE authentication.
	// The credential limiter cannot see an unauthenticated flood: such a request
	// is rejected by the auth middleware, after an HMAC or a signature check, and
	// never reaches a bucket. This is a flood bound, not a quota.
	RateLimitIPEnabled   bool
	RateLimitIPBurst     int
	RateLimitIPPerSecond int

	// TrustedProxyCIDRs lists the proxies whose X-Forwarded-For may be believed.
	// Empty means trust none and always key on the peer address, which is the
	// safe default: the header is caller-supplied, so honouring it from an
	// untrusted peer lets anyone pick their own bucket.
	TrustedProxyCIDRs []string

	// RateLimitDistributedEnabled moves the per-credential admission check into
	// Redis, making the limit cluster-wide rather than per replica. It adds one
	// Redis round trip to each authenticated request — on the Send path, which
	// already makes one or two. If Redis cannot answer, the request is decided
	// from the local bucket instead, so an outage degrades to per-replica
	// enforcement rather than failing requests or dropping the limit entirely.
	RateLimitDistributedEnabled bool

	// DynamoDB / ExtendDB — set DynamoEndpoint to an ExtendDB URL for local dev and
	// multi-cloud environments; leave empty to use native DynamoDB on AWS.
	DynamoEndpoint string
	DynamoRegion   string

	// Environment gates the transport-security and placeholder-secret checks in
	// Validate. Only the exact value "development" relaxes them — see ADR 0005.
	Environment string
}

func Load() Config {
	return Config{
		HTTPPort:         envInt("HERMES_HTTP_PORT", 8080),
		DatabaseURL:      envStr("HERMES_DATABASE_URL", "postgres://hermes:hermes@localhost:5432/hermes?sslmode=disable"),
		NATSUrl:          envStr("HERMES_NATS_URL", "nats://localhost:4222"),
		NATSCABundlePath: envStr("HERMES_NATS_CA_BUNDLE", ""),
		NATSNKeySeedPath: envStr("HERMES_NATS_NKEY_SEED", ""),

		NATSStreamReplicas:            envInt("HERMES_NATS_STREAM_REPLICAS", 1),
		NATSStreamMaxBytes:            envInt64("HERMES_NATS_STREAM_MAX_BYTES", 0),
		NATSStreamReplicasAllowChange: envBool("HERMES_NATS_STREAM_REPLICAS_ALLOW_CHANGE", false),

		ShutdownDrainDelay: envDuration("HERMES_SHUTDOWN_DRAIN_DELAY", 5*time.Second),
		ShutdownTimeout:    envDuration("HERMES_SHUTDOWN_TIMEOUT", 15*time.Second),
		NATSDrainTimeout:   envDuration("HERMES_NATS_DRAIN_TIMEOUT", 30*time.Second),

		DatabaseMaxConns:        envInt("HERMES_DATABASE_MAX_CONNS", 10),
		DatabaseMinConns:        envInt("HERMES_DATABASE_MIN_CONNS", 2),
		DatabaseMaxConnLifetime: envDuration("HERMES_DATABASE_MAX_CONN_LIFETIME", 30*time.Minute),
		DatabaseMaxConnIdleTime: envDuration("HERMES_DATABASE_MAX_CONN_IDLE_TIME", 5*time.Minute),

		RedisPoolSize:    envInt("HERMES_REDIS_POOL_SIZE", 16),
		RedisTimeout:     envDuration("HERMES_REDIS_TIMEOUT", 500*time.Millisecond),
		RedisURL:         envStr("HERMES_REDIS_URL", "redis://localhost:6379/0"),
		JWTSecret:        envStr("HERMES_JWT_SECRET", "hermes-jwt-secret"),
		CentrifugoAPIURL: envStr("HERMES_CENTRIFUGO_API_URL", "http://localhost:8000"),
		CentrifugoAPIKey: envStr("HERMES_CENTRIFUGO_API_KEY", "centrifugo-api-key"),
		Email: EmailConfig{
			Provider:     envStr("HERMES_EMAIL_PROVIDER", "smtp"),
			From:         envStr("HERMES_EMAIL_FROM", "noreply@example.com"),
			SMTPHost:     envStr("HERMES_EMAIL_SMTP_HOST", "localhost"),
			SMTPPort:     envInt("HERMES_EMAIL_SMTP_PORT", 1025),
			SMTPUsername: envStr("HERMES_EMAIL_SMTP_USERNAME", ""),
			SMTPPassword: envStr("HERMES_EMAIL_SMTP_PASSWORD", ""),
			SESRegion:    envStr("HERMES_EMAIL_SES_REGION", "us-east-1"),
			LayoutPath:   envStr("HERMES_EMAIL_LAYOUT_PATH", ""),
		},
		SMSWebhookURL:       envStr("HERMES_SMS_WEBHOOK_URL", "http://localhost:9090/sms"),
		APIKeyHMACSecret:    envStr("HERMES_API_KEY_HMAC_SECRET", "hermes-dev-hmac-secret"),
		EventRetentionDays:  envInt("HERMES_EVENT_RETENTION_DAYS", 90),
		DispatchConcurrency: envInt("HERMES_DISPATCH_CONCURRENCY", 8),
		DispatchPrefetch:    envInt("HERMES_DISPATCH_PREFETCH", 64),
		RateLimitEnabled:    envBool("HERMES_RATELIMIT_ENABLED", true),
		RateLimitBurst:      envInt("HERMES_RATELIMIT_BURST", 0),
		RateLimitPerSecond:  envInt("HERMES_RATELIMIT_PER_SECOND", 0),

		RateLimitIPEnabled:   envBool("HERMES_RATELIMIT_IP_ENABLED", true),
		RateLimitIPBurst:     envInt("HERMES_RATELIMIT_IP_BURST", 0),
		RateLimitIPPerSecond: envInt("HERMES_RATELIMIT_IP_PER_SECOND", 0),
		TrustedProxyCIDRs:    envStrSlice("HERMES_TRUSTED_PROXY_CIDRS", nil),

		// Off by default. Turning it on is a behaviour change an operator should
		// opt into: a caller's effective ceiling stops scaling with the replica
		// count, which is the point but is also a reduction in the throughput a
		// multi-replica deployment was getting.
		RateLimitDistributedEnabled: envBool("HERMES_RATELIMIT_DISTRIBUTED_ENABLED", false),

		DynamoEndpoint: envStr("HERMES_DYNAMO_ENDPOINT", ""),
		DynamoRegion:   envStr("HERMES_DYNAMO_REGION", "us-east-1"),
		Environment:    envStr("HERMES_ENV", EnvDevelopment),
	}
}

// EnvDevelopment is the one environment in which plaintext transports and the placeholder
// secrets below are tolerated. Any other value — including a misspelling — takes the
// strict path, so a typo in HERMES_ENV cannot silently disable every check.
const EnvDevelopment = "development"

// placeholderSecrets pairs each secret with the built-in default it must not still be.
// Those defaults are committed to a public repository, so a deployment still using one
// does not have a weak secret — it has a published constant. The variable name travels
// with the check so the error can say what to fix.
var placeholderSecrets = []struct {
	envVar      string
	get         func(Config) string
	placeholder string
}{
	{"HERMES_JWT_SECRET", func(c Config) string { return c.JWTSecret }, "hermes-jwt-secret"},
	{"HERMES_API_KEY_HMAC_SECRET", func(c Config) string { return c.APIKeyHMACSecret }, "hermes-dev-hmac-secret"},
	{"HERMES_CENTRIFUGO_API_KEY", func(c Config) string { return c.CentrifugoAPIKey }, "centrifugo-api-key"},
}

// MustLoad loads configuration and exits if it is not fit for the environment it names.
//
// Failing closed at startup is the point (ADR 0005): a service that cannot reach its
// datastore securely refuses to run, rather than connecting in the clear and continuing
// to serve. That failure is loud and recoverable; the alternative is silent and is not.
func MustLoad() Config {
	cfg := Load()
	if err := cfg.Validate(); err != nil {
		log.Fatalf("invalid configuration: %v", err)
	}
	return cfg
}

// Validate reports every problem it finds, not just the first.
//
// It inspects the connection strings themselves rather than separate "TLS enabled"
// settings. Two settings that can disagree are worse than one that cannot: the URL is
// what actually governs the connection, so the URL is what gets checked.
func (c Config) Validate() error {
	if c.Environment == EnvDevelopment {
		return nil
	}

	var problems []string

	// Postgres carries TLS in the sslmode query parameter. Absent is not a safe default:
	// libpq's "prefer" falls back to plaintext without telling anyone.
	if mode := sslMode(c.DatabaseURL); !secureSSLModes[mode] {
		if mode == "" {
			problems = append(problems, "HERMES_DATABASE_URL has no sslmode; require, verify-ca or verify-full is needed outside development")
		} else {
			problems = append(problems, fmt.Sprintf("HERMES_DATABASE_URL has sslmode=%s; require, verify-ca or verify-full is needed outside development", mode))
		}
	}

	if !strings.HasPrefix(c.RedisURL, "rediss://") {
		problems = append(problems, "HERMES_REDIS_URL is not rediss://; TLS is required outside development")
	}

	// NATS accepts tls:// to enable TLS; nats:// is plaintext.
	if !strings.HasPrefix(c.NATSUrl, "tls://") {
		problems = append(problems, "HERMES_NATS_URL is not tls://; TLS is required outside development")
	}

	for _, s := range placeholderSecrets {
		if s.get(c) == s.placeholder {
			problems = append(problems, fmt.Sprintf("%s is still the built-in default, which is committed to a public repository", s.envVar))
		}
	}

	if len(problems) == 0 {
		return nil
	}
	return fmt.Errorf("environment %q requires:\n  - %s", c.Environment, strings.Join(problems, "\n  - "))
}

// secureSSLModes are the libpq modes that actually encrypt. "allow" and "prefer" are
// excluded deliberately: both silently fall back to plaintext.
var secureSSLModes = map[string]bool{
	"require":     true,
	"verify-ca":   true,
	"verify-full": true,
}

// sslMode extracts the sslmode parameter, returning "" when absent or unparseable.
func sslMode(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	return u.Query().Get("sslmode")
}

func envStr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func envInt(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		if i, err := strconv.Atoi(v); err == nil {
			return i
		}
	}
	return fallback
}

// envDuration reads a Go duration string ("30s", "1m"). A bare number is rejected rather than
// guessed at: "30" could plausibly mean seconds or milliseconds, and silently choosing wrong
// would either shorten a drain to nothing or stretch it past the grace period.
func envDuration(key string, fallback time.Duration) time.Duration {
	if v := os.Getenv(key); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return fallback
}

// envStrSlice reads a comma-separated list. Blank entries are dropped, so a
// trailing comma or a value indented across lines in a ConfigMap does not become
// an empty element that a parser would then have to special-case.
func envStrSlice(key string, fallback []string) []string {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	var out []string
	for _, part := range strings.Split(v, ",") {
		if s := strings.TrimSpace(part); s != "" {
			out = append(out, s)
		}
	}
	return out
}

func envInt64(key string, fallback int64) int64 {
	if v := os.Getenv(key); v != "" {
		if i, err := strconv.ParseInt(v, 10, 64); err == nil {
			return i
		}
	}
	return fallback
}

// envBool accepts the forms strconv.ParseBool does ("true"/"false", "1"/"0",
// "t"/"f"). An unparseable value falls back rather than failing, matching envInt
// — a typo in a limit knob must not stop a service from starting.
func envBool(key string, fallback bool) bool {
	if v := os.Getenv(key); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			return b
		}
	}
	return fallback
}
