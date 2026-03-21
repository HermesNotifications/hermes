package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"

	"github.com/hermes-notifications/hermes/internal/auth"
	"github.com/hermes-notifications/hermes/internal/database"
)

const (
	devKeyID  = "dev000000000000000000000001"
	devKeyRaw = "hms_dev_key"
)

func main() {
	dbURL := flag.String("database-url", os.Getenv("HERMES_DATABASE_URL"), "PostgreSQL connection URL")
	flag.Parse()

	if *dbURL == "" {
		log.Fatal("database-url is required (or set HERMES_DATABASE_URL)")
	}

	ctx := context.Background()
	pool, err := database.NewPool(ctx, *dbURL)
	if err != nil {
		log.Fatalf("connect to database: %v", err)
	}
	defer pool.Close()

	keyHash, err := auth.HashAPIKey(devKeyRaw)
	if err != nil {
		log.Fatalf("hash API key: %v", err)
	}

	tag, err := pool.Exec(ctx,
		`INSERT INTO api_keys (id, key_hash, name) VALUES ($1, $2, $3) ON CONFLICT (id) DO NOTHING`,
		devKeyID, keyHash, "Development",
	)
	if err != nil {
		log.Fatalf("insert dev API key: %v", err)
	}

	if tag.RowsAffected() == 0 {
		fmt.Println("dev API key already exists")
	} else {
		fmt.Printf("dev API key seeded: %s\n", devKeyRaw)
	}
}
