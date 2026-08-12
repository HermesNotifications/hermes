// Copyright Hermes Notifications
// SPDX-License-Identifier: Apache-2.0
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"

	"github.com/hermesnotifications/hermes/internal/cache"
	"github.com/hermesnotifications/hermes/internal/database"
	"github.com/hermesnotifications/hermes/internal/dispatchbench"
	"github.com/hermesnotifications/hermes/internal/messaging"
	"github.com/hermesnotifications/hermes/internal/store"
	"github.com/hermesnotifications/hermes/internal/store/postgres"
)

func intList(s string) []int {
	var out []int
	for _, p := range strings.Split(s, ",") {
		if n, err := strconv.Atoi(strings.TrimSpace(p)); err == nil {
			out = append(out, n)
		}
	}
	return out
}

func main() {
	var (
		dbURL       = flag.String("db", envOr("HERMES_DATABASE_URL", "postgres://hermes:hermes@localhost:5432/hermes?sslmode=disable"), "Postgres URL (set pool_max_conns >= max workers)")
		natsURL     = flag.String("nats", envOr("HERMES_NATS_URL", "nats://localhost:4222"), "NATS URL")
		redisURL    = flag.String("redis", envOr("HERMES_REDIS_URL", "redis://localhost:6379/0"), "Redis URL")
		dynamoEP    = flag.String("dynamo", os.Getenv("HERMES_DYNAMO_ENDPOINT"), "DynamoDB Local endpoint (empty => skip dynamo cells)")
		dynamoRgn   = flag.String("dynamo-region", envOr("HERMES_DYNAMO_REGION", "us-east-1"), "DynamoDB region")
		workersCSV  = flag.String("workers", "1,2,4,8,16", "worker counts")
		prefetchCSV = flag.String("prefetch", "1,16,64,256", "prefetch values")
		backendCSV  = flag.String("backends", "postgres", "backends: postgres,dynamo")
		n           = flag.Int("n", 20000, "messages per drain")
		reps        = flag.Int("reps", 5, "measured repetitions per cell")
		warmups     = flag.Int("warmups", 1, "discarded warmup repetitions per cell")
		users       = flag.Int("users", 1000, "seeded bench users")
		seed        = flag.Int64("seed", 1, "shuffle seed")
		drainTO     = flag.Duration("drain-timeout", 2*time.Minute, "max time for one drain before it is abandoned")
		csvOut      = flag.String("csv", "dispatch-tuning.csv", "CSV output path")
		mdOut       = flag.String("md", "dispatch-tuning.md", "markdown summary output path")
	)
	flag.Parse()

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	ctx := context.Background()

	pool, err := database.NewPool(ctx, *dbURL)
	must(err, "db")
	defer pool.Close()
	pgStore := postgres.New(pool)

	redisClient, err := cache.Connect(*redisURL)
	must(err, "redis")
	defer redisClient.Close()

	admin, err := messaging.Connect(*natsURL)
	must(err, "nats admin")
	defer admin.Close()
	must(admin.SetupStreams(ctx, messaging.StreamOptions{}), "setup streams")

	nc, err := nats.Connect(*natsURL)
	must(err, "nats raw")
	defer nc.Close()
	js, err := jetstream.New(nc)
	must(err, "jetstream")

	benchOrganization := uuid.New().String() // organizations.id is a UUID column
	userIDs := seedBench(ctx, pool, pgStore, redisClient, benchOrganization, *users)
	fmt.Fprintf(os.Stderr, "seeded organization %s with %d users\n", benchOrganization, len(userIDs))

	backends := strings.Split(*backendCSV, ",")
	cells := dispatchbench.Cells(intList(*workersCSV), intList(*prefetchCSV), backends)
	dispatchbench.Shuffle(cells, *seed)

	repos := map[string]store.NotificationRepository{}
	for _, b := range backends {
		if _, done := repos[b]; done {
			continue
		}
		repo, cleanup := storeForBackend(ctx, b, pgStore, *dynamoEP, *dynamoRgn, logger)
		if cleanup != nil {
			defer cleanup()
		}
		repos[b] = repo // may be nil => backend unavailable; handled in the loop
	}

	var results []dispatchbench.Result
	for _, cell := range cells {
		notifRepo := repos[cell.Backend]
		if notifRepo == nil {
			fmt.Fprintf(os.Stderr, "skip %s cells: backend unavailable\n", cell.Backend)
			continue
		}
		runner := newRunner(js, *natsURL, *n, benchOrganization, userIDs, notifRepo, pgStore, redisClient, admin, pool, logger)

		drain := func() (float64, error) {
			dctx, cancel := context.WithTimeout(ctx, *drainTO)
			defer cancel()
			return runner.Drain(dctx, cell)
		}
		for i := 0; i < *warmups; i++ {
			if _, err := drain(); err != nil {
				fmt.Fprintf(os.Stderr, "warmup %+v: %v\n", cell, err)
			}
		}
		var samples []float64
		for i := 0; i < *reps; i++ {
			tp, err := drain()
			if err != nil {
				fmt.Fprintf(os.Stderr, "rep %+v: %v\n", cell, err)
				continue
			}
			samples = append(samples, tp)
		}
		results = append(results, dispatchbench.Result{Cell: cell, Throughput: samples})
		fmt.Fprintf(os.Stderr, "%s w=%d p=%d -> %.0f msgs/s\n",
			cell.Backend, cell.Workers, cell.Prefetch, dispatchbench.Summarize(samples).Mean)
	}

	writeOutputs(*csvOut, *mdOut, results)
	fmt.Fprintf(os.Stderr, "wrote %s and %s\n", *csvOut, *mdOut)
}
