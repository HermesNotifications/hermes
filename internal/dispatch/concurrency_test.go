// Copyright Hermes Notifications
// SPDX-License-Identifier: Apache-2.0
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

package dispatch_test

import (
	"testing"

	"github.com/hermesnotifications/hermes/internal/dispatch"
)

func TestClampWorkersToPool(t *testing.T) {
	tests := []struct {
		name          string
		requested     int
		dbMaxConns    int
		wantEffective int
		wantClamped   bool
	}{
		{"under pool passes through", 4, 10, 4, false},
		{"equal to pool passes through", 10, 10, 10, false},
		{"over pool is clamped", 32, 10, 10, true},
		{"unknown pool size does not clamp", 32, 0, 32, false},
		{"negative pool size does not clamp", 8, -1, 8, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			eff, clamped := dispatch.ClampWorkersToPool(tt.requested, tt.dbMaxConns)
			if eff != tt.wantEffective {
				t.Errorf("effective = %d, want %d", eff, tt.wantEffective)
			}
			if clamped != tt.wantClamped {
				t.Errorf("clamped = %v, want %v", clamped, tt.wantClamped)
			}
		})
	}
}
