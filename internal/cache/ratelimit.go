// Copyright Hermes Notifications
// SPDX-License-Identifier: Apache-2.0
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

package cache

import (
	"context"
	"fmt"
	"time"

	"github.com/go-redis/redis_rate/v10"
)

// rateLimitTimeout bounds one admission check.
//
// It is deliberately far tighter than the client's general 500ms budget. This
// call is on the request path, and its caller has a local bucket to fall back
// to, so waiting is never the best available option: a Redis that has not
// answered in this long should be treated as absent rather than allowed to add
// its latency to every request. The general timeout is sized for calls with no
// fallback; this one is sized for a call that has one.
const rateLimitTimeout = 100 * time.Millisecond

// RateLimitDecision is the outcome of one distributed admission check.
type RateLimitDecision struct {
	Allowed bool
	// Remaining is the tokens left for this key across the whole cluster.
	Remaining int
	// RetryAfter is how long the caller should wait. Zero when allowed.
	RetryAfter time.Duration
}

// AllowRequest consumes one token for key against a cluster-wide limit.
//
// The algorithm is GCRA (a leaky bucket phrased as a virtual scheduling time),
// evaluated inside a single Lua script, so the whole check is one round trip and
// is atomic across replicas — there is no read-modify-write window in which two
// replicas can both believe they hold the last token.
//
// burst is the instantaneous allowance and perSecond the sustained rate, matching
// the local limiter's vocabulary so a key means the same thing on either path.
//
// An error means the decision could not be made, NOT that the request should be
// refused. Callers fall back to their local bucket; see
// middleware.RateLimiter.
func (c *Client) AllowRequest(ctx context.Context, key string, burst, perSecond int) (RateLimitDecision, error) {
	if perSecond <= 0 {
		return RateLimitDecision{Allowed: true}, nil
	}
	if burst <= 0 {
		burst = perSecond
	}

	ctx, cancel := context.WithTimeout(ctx, rateLimitTimeout)
	defer cancel()

	res, err := c.limiter.Allow(ctx, "rl:"+key, redis_rate.Limit{
		Rate:   perSecond,
		Burst:  burst,
		Period: time.Second,
	})
	if err != nil {
		return RateLimitDecision{}, fmt.Errorf("rate limit check: %w", err)
	}

	return RateLimitDecision{
		Allowed:    res.Allowed > 0,
		Remaining:  res.Remaining,
		RetryAfter: res.RetryAfter,
	}, nil
}
