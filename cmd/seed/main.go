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

func main() {
	dbURL := flag.String("database-url", os.Getenv("HERMES_DATABASE_URL"), "PostgreSQL connection URL")
	hmacSecret := flag.String("hmac-secret", os.Getenv("HERMES_API_KEY_HMAC_SECRET"), "HMAC secret for key hashing")
	flag.Parse()

	if *dbURL == "" {
		log.Fatal("database-url is required (or set HERMES_DATABASE_URL)")
	}

	if *hmacSecret == "" {
		*hmacSecret = "hermes-dev-hmac-secret"
	}

	ctx := context.Background()
	pool, err := database.NewPool(ctx, *dbURL)
	if err != nil {
		log.Fatalf("connect to database: %v", err)
	}
	defer pool.Close()

	rawKey, keyID, err := auth.GenerateAPIKey("dev")
	if err != nil {
		log.Fatalf("generate api key: %v", err)
	}

	_, secret, err := auth.ParseAPIKey(rawKey)
	if err != nil {
		log.Fatalf("parse api key: %v", err)
	}

	keyHash := auth.HMACHashAPIKey(secret, *hmacSecret)
	allPerms := []string{auth.PermAPIKeysManage, auth.PermNotificationsSend, auth.PermTemplatesManage, auth.PermTenantsManage}

	tag, err := pool.Exec(ctx,
		`INSERT INTO api_keys (id, key_hash, name, permissions) VALUES ($1, $2, $3, $4) ON CONFLICT (id) DO NOTHING`,
		keyID, keyHash, "Development", allPerms,
	)
	if err != nil {
		log.Fatalf("insert dev API key: %v", err)
	}

	if tag.RowsAffected() == 0 {
		fmt.Println("dev API key already exists (delete old key from DB and re-run to generate new one)")
	} else {
		fmt.Printf("Dev API key seeded:\n  %s\n", rawKey)
	}
}
