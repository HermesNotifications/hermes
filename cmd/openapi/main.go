// Copyright 2026 Hermes Notifications. Licensed under the Apache License, Version 2.0.
// See LICENSE and NOTICE in the project root for full terms and restrictions.

// Command openapi generates OpenAPI 3.1 specs from huma route registrations.
// Usage: go run ./cmd/openapi -service admin -out api/admin/openapi.yaml
package main

import (
	"flag"
	"fmt"
	"log/slog"
	"os"

	"github.com/danielgtaylor/huma/v2"
	"github.com/hermes-notifications/hermes/internal/admin"
	"github.com/hermes-notifications/hermes/internal/inbox"
	"github.com/hermes-notifications/hermes/internal/send"
	"github.com/hermes-notifications/hermes/internal/userservice"
)

func main() {
	service := flag.String("service", "", "Service to generate spec for (admin, inbox)")
	out := flag.String("out", "", "Output file path (default: stdout)")
	format := flag.String("format", "yaml", "Output format: yaml or json")
	flag.Parse()

	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))

	switch *service {
	case "admin":
		adminSrv := admin.NewServer(nil, nil, nil, nil, nil, "", logger)
		sendSrv := send.NewServer(nil, nil, nil, nil, "", logger)
		// Merge send paths into admin spec for a combined SDK spec
		adminSpec := adminSrv.API().OpenAPI()
		sendSpec := sendSrv.API().OpenAPI()
		if adminSpec.Paths == nil {
			adminSpec.Paths = map[string]*huma.PathItem{}
		}
		for path, pathItem := range sendSpec.Paths {
			adminSpec.Paths[path] = pathItem
		}
		// Merge send schemas into admin spec
		if sendSpec.Components != nil && sendSpec.Components.Schemas != nil {
			if adminSpec.Components == nil {
				adminSpec.Components = &huma.Components{}
			}
			if adminSpec.Components.Schemas == nil {
				adminSpec.Components.Schemas = huma.NewMapRegistry("#/components/schemas/", huma.DefaultSchemaNamer)
			}
			for name, schema := range sendSpec.Components.Schemas.Map() {
				adminSpec.Components.Schemas.Map()[name] = schema
			}
		}
		writeSpec(adminSrv.API(), *out, *format)
	case "inbox":
		srv := inbox.NewServer(nil, nil, nil, nil, nil, logger)
		writeSpec(srv.API(), *out, *format)
	case "user":
		srv := userservice.NewServer(nil, nil, logger)
		writeSpec(srv.API(), *out, *format)
	default:
		fmt.Fprintf(os.Stderr, "unknown or missing service: %q (supported: admin, inbox, user)\n", *service)
		os.Exit(1)
	}
}

func writeSpec(api huma.API, outPath, format string) {
	var (
		data []byte
		err  error
	)
	switch format {
	case "yaml":
		data, err = api.OpenAPI().YAML()
	case "json":
		data, err = api.OpenAPI().MarshalJSON()
	default:
		fmt.Fprintf(os.Stderr, "unknown format: %q\n", format)
		os.Exit(1)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to marshal spec: %v\n", err)
		os.Exit(1)
	}

	if outPath == "" {
		os.Stdout.Write(data)
		return
	}

	if err := os.WriteFile(outPath, data, 0644); err != nil {
		fmt.Fprintf(os.Stderr, "failed to write %s: %v\n", outPath, err)
		os.Exit(1)
	}
	fmt.Fprintf(os.Stderr, "wrote %s\n", outPath)
}
