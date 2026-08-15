// Copyright Hermes Notifications
// SPDX-License-Identifier: Apache-2.0
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

package observability

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

// LogThrottle rate-limits one log call site, emitting at most one record per
// interval and reporting how many it swallowed in between.
//
// It exists for a specific shape of problem: a fault in a dependency that is
// detected per request. "shared rate limiter unavailable" and "unread count cache
// read failed" are both correct things to say once, and both attached to code
// that runs on every request — so a Redis outage turned each of them into a log
// record per request, for as long as the outage lasted. That is the worst
// possible time to be spending log budget, because the volume scales with
// traffic while telling you nothing that the first record did not.
//
// Throttling rather than demoting is deliberate. These really are warnings — the
// service is running degraded and someone should know — so the fix is to say it
// once and keep saying it occasionally, not to stop saying it. The suppressed
// count is what keeps the record honest: an operator seeing suppressed=41892 is
// being told the rate as well as the fact, and both call sites also feed a
// counter that carries the precise rate.
//
// The zero value is not usable; construct with NewLogThrottle.
type LogThrottle struct {
	every time.Duration

	mu         sync.Mutex
	last       time.Time
	suppressed int64
}

// NewLogThrottle returns a throttle admitting at most one record per every.
func NewLogThrottle(every time.Duration) *LogThrottle {
	return &LogThrottle{every: every}
}

// admit reports whether this occurrence should be logged, and if so how many
// occurrences were suppressed since the last one that was.
func (t *LogThrottle) admit(now time.Time) (bool, int64) {
	t.mu.Lock()
	defer t.mu.Unlock()

	// The zero value of last admits the first occurrence, which is the one that
	// matters most — an operator should learn about a degradation when it starts,
	// not up to `every` later.
	if !t.last.IsZero() && now.Sub(t.last) < t.every {
		t.suppressed++
		return false, 0
	}
	n := t.suppressed
	t.suppressed = 0
	t.last = now
	return true, n
}

// Log emits msg at level unless an identical call was admitted less than the
// throttle interval ago. When records were suppressed since the last emission, a
// suppressed=N attribute is appended.
//
// args follows slog's alternating key/value form.
func (t *LogThrottle) Log(ctx context.Context, logger *slog.Logger, level slog.Level, msg string, args ...any) {
	if logger == nil {
		return
	}
	ok, suppressed := t.admit(time.Now())
	if !ok {
		return
	}
	if suppressed > 0 {
		args = append(args, "suppressed", suppressed)
	}
	logger.Log(ctx, level, msg, args...)
}
