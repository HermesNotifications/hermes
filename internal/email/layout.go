// Copyright Hermes Notifications
// SPDX-License-Identifier: Apache-2.0
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

package email

import (
	_ "embed"
	"fmt"
	"log/slog"
	"os"
	"text/template"
)

//go:embed layout.html
var defaultLayout string

// MustLoadLayout loads the email layout template. If layoutPath is empty, the
// embedded default is used. If layoutPath is "none", nil is returned (no layout
// wrapping). Otherwise the file at layoutPath is read from disk.
func MustLoadLayout(layoutPath string, logger *slog.Logger) *template.Template {
	if layoutPath == "none" {
		logger.Info("email layout disabled")
		return nil
	}

	var content string
	if layoutPath == "" {
		content = defaultLayout
		logger.Info("using default email layout")
	} else {
		data, err := os.ReadFile(layoutPath)
		if err != nil {
			logger.Error("read email layout", "path", layoutPath, "error", err)
			fmt.Fprintf(os.Stderr, "failed to read email layout %s: %v\n", layoutPath, err)
			os.Exit(1)
		}
		content = string(data)
		logger.Info("loaded custom email layout", "path", layoutPath)
	}

	tmpl, err := template.New("layout").Parse(content)
	if err != nil {
		logger.Error("parse email layout", "error", err)
		fmt.Fprintf(os.Stderr, "failed to parse email layout: %v\n", err)
		os.Exit(1)
	}

	return tmpl
}
