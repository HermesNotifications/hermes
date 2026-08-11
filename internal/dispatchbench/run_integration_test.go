// Copyright Hermes Notifications
// SPDX-License-Identifier: Apache-2.0
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

//go:build integration

package dispatchbench_test

import (
	"context"
	"testing"
	"time"

	"github.com/hermes-notifications/hermes/internal/dispatchbench"
)

// TestRunnerDrainSmoke documents that the runner's control flow is verified for
// real by running the harness (`make dispatchbench`) against live infra, not by a
// fake jetstream.JetStream (a large interface). Kept as a skip so the package
// still builds under the integration tag. Do not expand it.
func TestRunnerDrainSmoke(t *testing.T) {
	t.Skip("covered by `make dispatchbench` against live infra; see cmd/dispatchbench")
	_ = context.Background()
	_ = time.Millisecond
	_ = dispatchbench.Runner{}
}
