// Copyright Hermes Notifications
// SPDX-License-Identifier: Apache-2.0
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

package bootstrap

import (
	"log/slog"
	"os"

	"github.com/hermes-notifications/hermes/internal/cache"
	"github.com/hermes-notifications/hermes/internal/config"
	"github.com/hermes-notifications/hermes/internal/middleware"
)

// RateLimited is the slice of a service Server this package configures. All four
// HTTP services satisfy it.
type RateLimited interface {
	ConfigureRateLimit(enabled bool, burst, perSecond int)
	ConfigureIPRateLimit(enabled bool, burst, perSecond int, proxies *middleware.TrustedProxies)
	CredentialLimiter() *middleware.RateLimiter
}

// SetupRateLimiting configures a server's two limiters.
//
// When RateLimitDistributedEnabled is set, the credential limiter is backed by
// Redis so its limit is cluster-wide rather than per replica. The per-IP limiter
// is always local: it is a flood bound whose key space an attacker chooses, and
// sending that to Redis would turn an address scan into Redis load.
//
// A malformed HERMES_TRUSTED_PROXY_CIDRS is fatal rather than ignored. Silently
// falling back to "trust nothing" would look identical to a working
// configuration until someone noticed every caller behind the proxy sharing one
// bucket, and silently falling back to "trust everything" would be a
// vulnerability.
func SetupRateLimiting(
	srv RateLimited,
	cfg config.Config,
	redisClient *cache.Client,
	logger *slog.Logger,
) {
	proxies, err := middleware.ParseTrustedProxies(cfg.TrustedProxyCIDRs)
	if err != nil {
		logger.Error("invalid HERMES_TRUSTED_PROXY_CIDRS", "error", err)
		os.Exit(1)
	}

	srv.ConfigureRateLimit(cfg.RateLimitEnabled, cfg.RateLimitBurst, cfg.RateLimitPerSecond)
	srv.ConfigureIPRateLimit(cfg.RateLimitIPEnabled, cfg.RateLimitIPBurst, cfg.RateLimitIPPerSecond, proxies)

	// Enabled with nothing trusted is the one combination that silently misbehaves rather
	// than failing: behind any proxy every request presents that proxy's address, so all
	// callers share one bucket and the limit applies to the fleet instead of to each
	// client. It cannot be detected from in here — one address may equally be one very
	// busy caller — so it is warned about rather than refused.
	if cfg.RateLimitIPEnabled && len(cfg.TrustedProxyCIDRs) == 0 {
		logger.Warn("per-IP rate limiting is enabled with no trusted proxies; "+
			"behind a proxy every caller will share one bucket",
			"fix", "set HERMES_TRUSTED_PROXY_CIDRS to your ingress controller's pod CIDR")
	}

	if !cfg.RateLimitDistributedEnabled || !cfg.RateLimitEnabled || redisClient == nil {
		return
	}

	srv.CredentialLimiter().WithShared(redisClient, logger)
	logger.Info("distributed rate limiting enabled")
}
