// Copyright Hermes Notifications
// SPDX-License-Identifier: Apache-2.0
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

package bootstrap

import (
	"log/slog"
	"testing"
)

func TestLogLevel_FromEnv(t *testing.T) {
	tests := []struct {
		name     string
		logLevel string
		env      string
		want     slog.Level
	}{
		{"explicit debug", "debug", "", slog.LevelDebug},
		{"explicit info", "info", "", slog.LevelInfo},
		{"explicit warn", "warn", "", slog.LevelWarn},
		{"warning alias", "warning", "", slog.LevelWarn},
		{"explicit error", "error", "", slog.LevelError},
		{"case insensitive", "WARN", "", slog.LevelWarn},
		{"whitespace tolerated", "  info  ", "", slog.LevelInfo},

		// The production default. Everything demoted in this change lives at Debug,
		// so this is the setting that makes steady-state volume track incidents
		// rather than traffic.
		{"unset defaults to info", "", "", slog.LevelInfo},
		{"unset in production is info", "", "production", slog.LevelInfo},

		// Development keeps the firehose, which is what these records were before
		// they were demoted — local behaviour is unchanged.
		{"unset in development is debug", "", "development", slog.LevelDebug},

		// An unparseable value must not be fatal: this resolves before config
		// validation runs, and a typo should not stop a service from starting.
		{"garbage falls back to info", "verbose", "", slog.LevelInfo},
		{"garbage in development falls back to debug", "verbose", "development", slog.LevelDebug},

		// An explicit level beats the environment default in both directions.
		{"explicit info overrides development", "info", "development", slog.LevelInfo},
		{"explicit debug overrides production", "debug", "production", slog.LevelDebug},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("HERMES_LOG_LEVEL", tc.logLevel)
			t.Setenv("HERMES_ENV", tc.env)
			if got := logLevel(); got != tc.want {
				t.Errorf("logLevel() = %v, want %v", got, tc.want)
			}
		})
	}
}

// NewLogger must actually apply the level, not just compute it.
func TestNewLogger_AppliesLevel(t *testing.T) {
	t.Setenv("HERMES_LOG_LEVEL", "warn")
	t.Setenv("HERMES_ENV", "")

	logger := NewLogger()
	if logger.Enabled(t.Context(), slog.LevelInfo) {
		t.Error("Info should be disabled at level=warn")
	}
	if !logger.Enabled(t.Context(), slog.LevelWarn) {
		t.Error("Warn should be enabled at level=warn")
	}
}
