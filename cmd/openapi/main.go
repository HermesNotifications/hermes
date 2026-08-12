// Copyright Hermes Notifications
// SPDX-License-Identifier: Apache-2.0
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

// Command openapi generates OpenAPI 3.1 specs from huma route registrations.
// Usage: go run ./cmd/openapi -service admin -out api/admin/openapi.yaml
package main

import (
	"flag"
	"fmt"
	"log/slog"
	"os"

	"github.com/danielgtaylor/huma/v2"
	"github.com/hermesnotifications/hermes/internal/admin"
	"github.com/hermesnotifications/hermes/internal/inbox"
	"github.com/hermesnotifications/hermes/internal/send"
	"github.com/hermesnotifications/hermes/internal/userservice"
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
		srv := inbox.NewServer(nil, nil, nil, nil, logger)
		writeSpec(srv.API(), *out, *format)
	case "user":
		srv := userservice.NewServer(nil, nil, logger)
		writeSpec(srv.API(), *out, *format)
	default:
		fmt.Fprintf(os.Stderr, "unknown or missing service: %q (supported: admin, inbox, user)\n", *service)
		os.Exit(1)
	}
}

// rateLimitErrorSchema names the shared 429 body component. It is part of the generated
// SDKs' public surface, so renaming it is a breaking change to every SDK.
const rateLimitErrorSchema = "RateLimitError"

// addRateLimitResponse documents 429 on every operation.
//
// It is applied here rather than on each huma.Register call because the limiter
// is chi middleware wrapping the whole router (see each service's Handler): huma
// never sees the request, so it cannot infer the response from a handler
// signature. Without this the generated SDKs have no 429 shape at all, and a
// client generated from the spec treats a rate-limited response as an unexpected
// status.
//
// Note the body is the middleware envelope — {"error": "..."} from
// httputil.ClientError — not huma's error shape, because the rejection happens
// before huma is reached.
func addRateLimitResponse(spec *huma.OpenAPI) {
	if spec.Paths == nil {
		return
	}

	// The body is registered as a named component and referenced, not inlined on each
	// operation.
	//
	// An identical inline schema on 35 operations is deduplicated by openapi-generator into
	// a single model named after whichever operation it happened to reach first —
	// "ListApiKeys429Response" on one run. spec.Paths is a Go map, so that order is not
	// stable, and the generated SDKs differed between a local run and CI for no reason
	// anyone could see from the diff. A named component is both deterministic and a better
	// name for a shape every endpoint shares.
	if spec.Components == nil {
		spec.Components = &huma.Components{}
	}
	if spec.Components.Schemas == nil {
		spec.Components.Schemas = huma.NewMapRegistry("#/components/schemas/", huma.DefaultSchemaNamer)
	}
	spec.Components.Schemas.Map()[rateLimitErrorSchema] = &huma.Schema{
		Type:        "object",
		Description: "The error envelope written by the rate limit middleware.",
		Properties: map[string]*huma.Schema{
			"error": {Type: "string", Description: "Human-readable reason."},
		},
	}

	body := &huma.Schema{Ref: "#/components/schemas/" + rateLimitErrorSchema}

	seconds := &huma.Schema{Type: "integer"}
	newResponse := func() *huma.Response {
		return &huma.Response{
			Description: "Too Many Requests. The caller exceeded its rate limit. " +
				"Honour Retry-After; retrying sooner does not shorten the wait. " +
				"A 429 from the pre-authentication per-address bound carries only " +
				"Retry-After, without the RateLimit-* headers.",
			Headers: map[string]*huma.Param{
				"Retry-After": {
					Description: "Whole seconds to wait before retrying. Always at least 1.",
					Schema:      seconds,
				},
				"RateLimit-Limit": {
					Description: "Sustained requests per second allowed for this credential.",
					Schema:      seconds,
				},
				"RateLimit-Remaining": {
					Description: "Requests available right now.",
					Schema:      seconds,
				},
				"RateLimit-Reset": {
					Description: "Seconds until capacity is available.",
					Schema:      seconds,
				},
			},
			Content: map[string]*huma.MediaType{
				"application/json": {Schema: body},
			},
		}
	}

	for _, item := range spec.Paths {
		if item == nil {
			continue
		}
		for _, op := range []*huma.Operation{
			item.Get, item.Put, item.Post, item.Delete,
			item.Options, item.Head, item.Patch, item.Trace,
		} {
			if op == nil {
				continue
			}
			if op.Responses == nil {
				op.Responses = map[string]*huma.Response{}
			}
			if _, exists := op.Responses["429"]; !exists {
				op.Responses["429"] = newResponse()
			}
		}
	}
}

func writeSpec(api huma.API, outPath, format string) {
	addRateLimitResponse(api.OpenAPI())

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
