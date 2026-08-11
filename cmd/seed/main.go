// Copyright Hermes Notifications
// SPDX-License-Identifier: Apache-2.0
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"math/big"
	"os"
	"path/filepath"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager"
	"github.com/hermes-notifications/hermes/internal/auth"
	"github.com/hermes-notifications/hermes/internal/database"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/scrypt"
	"golang.org/x/text/unicode/norm"
)

var allPermissions = []string{
	auth.PermAPIKeysManage,
	auth.PermNotificationsSend,
	auth.PermTemplatesManage,
	auth.PermOrganizationsManage,
}

func main() {
	dbURL := flag.String("database-url", os.Getenv("HERMES_DATABASE_URL"), "PostgreSQL connection URL")
	hmacSecret := flag.String("hmac-secret", os.Getenv("HERMES_API_KEY_HMAC_SECRET"), "HMAC secret for key hashing")
	env := flag.String("env", "dev", "Environment: dev, staging, production")
	force := flag.Bool("force", false, "Force rotation even if key exists")
	revokePrevious := flag.Bool("revoke-previous", false, "Revoke previous bootstrap key on rotation")
	awsRegion := flag.String("aws-region", "us-east-1", "AWS region for Secrets Manager")
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

	switch *env {
	case "dev":
		seedDev(ctx, pool, *hmacSecret)
	case "staging", "production":
		if *hmacSecret == "" {
			log.Fatal("hmac-secret is required for non-dev environments (or set HERMES_API_KEY_HMAC_SECRET)")
		}
		seedManaged(ctx, pool, *env, *hmacSecret, *awsRegion, *force, *revokePrevious)
	default:
		log.Fatalf("unknown environment: %s", *env)
	}
}

func seedDev(ctx context.Context, pool *pgxpool.Pool, hmacSecret string) {
	if hmacSecret == "" {
		hmacSecret = "hermes-dev-hmac-secret"
	}

	rawKey, keyID, err := auth.GenerateAPIKey("dev")
	if err != nil {
		log.Fatalf("generate api key: %v", err)
	}

	_, secret, err := auth.ParseAPIKey(rawKey)
	if err != nil {
		log.Fatalf("parse api key: %v", err)
	}

	keyHash := auth.HMACHashAPIKey(secret, hmacSecret)

	tag, err := pool.Exec(ctx,
		`INSERT INTO api_keys (id, key_hash, name, permissions) VALUES ($1, $2, $3, $4) ON CONFLICT (id) DO NOTHING`,
		keyID, keyHash, "Development", allPermissions,
	)
	if err != nil {
		log.Fatalf("insert dev API key: %v", err)
	}

	if tag.RowsAffected() == 0 {
		fmt.Println("dev API key already exists (delete old key from DB and re-run to generate new one)")
	} else {
		fmt.Printf("Dev API key seeded:\n  %s\n", rawKey)
	}

	seedAdminUser(ctx, pool)
	writeAdminEnvLocal(rawKey)
}

func seedAdminUser(ctx context.Context, pool *pgxpool.Pool) {
	const (
		userID = "admin-user-001"
		email  = "admin@hermes.local"
		name   = "Admin"
	)

	password, err := generatePassword(24)
	if err != nil {
		log.Fatalf("generate admin password: %v", err)
	}

	hashed, err := hashPasswordScrypt(password)
	if err != nil {
		log.Fatalf("hash admin password: %v", err)
	}

	userTag, err := pool.Exec(ctx,
		`INSERT INTO "user" (id, name, email, "emailVerified") VALUES ($1, $2, $3, true) ON CONFLICT DO NOTHING`,
		userID, name, email,
	)
	if err != nil {
		log.Fatalf("insert admin user: %v", err)
	}

	if userTag.RowsAffected() == 0 {
		fmt.Println("admin portal user already exists (admin@hermes.local)")
		return
	}

	_, err = pool.Exec(ctx,
		`INSERT INTO account (id, "accountId", "providerId", "userId", password)
		 VALUES ($1, $2, 'credential', $3, $4) ON CONFLICT DO NOTHING`,
		userID+"-cred", userID, userID, hashed,
	)
	if err != nil {
		log.Fatalf("insert admin account: %v", err)
	}

	fmt.Println("Admin portal user seeded:")
	fmt.Printf("  Email:    %s\n", email)
	fmt.Printf("  Password: %s\n", password)
}

func generatePassword(length int) (string, error) {
	const charset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789!@#$%&*"
	b := make([]byte, length)
	for i := range b {
		n, err := rand.Int(rand.Reader, big.NewInt(int64(len(charset))))
		if err != nil {
			return "", err
		}
		b[i] = charset[n.Int64()]
	}
	return string(b), nil
}

// hashPasswordScrypt produces a hash compatible with Better Auth's scrypt format: "salt:key" (hex-encoded).
// Parameters match better-auth/src/crypto/password.ts: N=16384, r=16, p=1, dkLen=64.
func hashPasswordScrypt(password string) (string, error) {
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	saltHex := hex.EncodeToString(salt)
	normalized := norm.NFKC.String(password)
	key, err := scrypt.Key([]byte(normalized), []byte(saltHex), 16384, 16, 1, 64)
	if err != nil {
		return "", err
	}
	return saltHex + ":" + hex.EncodeToString(key), nil
}

func writeAdminEnvLocal(rawKey string) {
	envPath := filepath.Join("web", "admin", ".env.local")

	// If file exists, update just the API key in place.
	if data, err := os.ReadFile(envPath); err == nil {
		lines := strings.Split(string(data), "\n")
		found := false
		for i, line := range lines {
			if strings.HasPrefix(line, "HERMES_API_KEY=") {
				lines[i] = "HERMES_API_KEY=" + rawKey
				found = true
				break
			}
		}
		if !found {
			// Insert after HERMES_API_URL if possible, otherwise append.
			inserted := false
			for i, line := range lines {
				if strings.HasPrefix(line, "HERMES_API_URL=") {
					lines = append(lines[:i+1], append([]string{"HERMES_API_KEY=" + rawKey}, lines[i+1:]...)...)
					inserted = true
					break
				}
			}
			if !inserted {
				lines = append(lines, "HERMES_API_KEY="+rawKey)
			}
		}
		if err := os.WriteFile(envPath, []byte(strings.Join(lines, "\n")), 0600); err != nil {
			log.Printf("update %s: %v", envPath, err)
			return
		}
		fmt.Printf("Admin portal API key updated in %s\n", envPath)
		return
	}

	content := fmt.Sprintf(
		"HERMES_API_URL=http://localhost:8080\nHERMES_API_KEY=%s\nBETTER_AUTH_SECRET=hermes-dev-better-auth-secret\nDATABASE_URL=postgres://hermes:hermes@localhost:5432/hermes?sslmode=disable\n",
		rawKey,
	)
	if err := os.WriteFile(envPath, []byte(content), 0600); err != nil {
		log.Printf("write %s: %v (create it manually)", envPath, err)
		return
	}
	fmt.Printf("Admin portal config written to %s\n", envPath)
}

func seedManaged(ctx context.Context, pool *pgxpool.Pool, env, hmacSecret, awsRegion string, force, revokePrevious bool) {
	secretID := "hermes/" + env

	cfg, err := awsconfig.LoadDefaultConfig(ctx, awsconfig.WithRegion(awsRegion))
	if err != nil {
		log.Fatalf("load AWS config: %v", err)
	}
	sm := secretsmanager.NewFromConfig(cfg)

	if !force {
		existing, err := getSecretProperty(ctx, sm, secretID, "admin_api_key")
		if err != nil {
			log.Fatalf("check existing secret: %v", err)
		}
		if existing != "" {
			fmt.Printf("Bootstrap key already exists in %s. Use --force to rotate.\n", secretID)
			return
		}
	}

	var oldKeyID string
	if force {
		old, _ := getSecretProperty(ctx, sm, secretID, "admin_api_key")
		if old != "" {
			if id, _, err := auth.ParseAPIKey(old); err == nil {
				oldKeyID = id
			}
		}
	}

	envPrefix := ""
	if env == "staging" {
		envPrefix = "stg"
	}
	rawKey, keyID, err := auth.GenerateAPIKey(envPrefix)
	if err != nil {
		log.Fatalf("generate api key: %v", err)
	}

	_, secret, err := auth.ParseAPIKey(rawKey)
	if err != nil {
		log.Fatalf("parse api key: %v", err)
	}

	keyHash := auth.HMACHashAPIKey(secret, hmacSecret)
	keyName := fmt.Sprintf("Bootstrap (%s)", env)

	tag, err := pool.Exec(ctx,
		`INSERT INTO api_keys (id, key_hash, name, permissions) VALUES ($1, $2, $3, $4) ON CONFLICT (id) DO NOTHING`,
		keyID, keyHash, keyName, allPermissions,
	)
	if err != nil {
		log.Fatalf("insert API key: %v", err)
	}
	if tag.RowsAffected() == 0 {
		log.Fatalf("key ID collision — this should not happen, try again")
	}

	if err := setSecretProperty(ctx, sm, secretID, "admin_api_key", rawKey); err != nil {
		log.Fatalf("write to Secrets Manager: %v", err)
	}

	fmt.Printf("Bootstrap key created for %s:\n  ID: %s\n  Stored in: %s -> admin_api_key\n", env, keyID, secretID)

	if revokePrevious && oldKeyID != "" {
		_, err := pool.Exec(ctx, `DELETE FROM api_keys WHERE id = $1`, oldKeyID)
		if err != nil {
			log.Printf("WARNING: failed to revoke previous key %s: %v", oldKeyID, err)
		} else {
			fmt.Printf("  Previous key %s revoked.\n", oldKeyID)
		}
	} else if oldKeyID != "" {
		fmt.Printf("  WARNING: Previous key %s is still valid. Revoke it via:\n", oldKeyID)
		fmt.Printf("    hermes apikey revoke --id %s\n", oldKeyID)
	}
}

func getSecretProperty(ctx context.Context, sm *secretsmanager.Client, secretID, property string) (string, error) {
	out, err := sm.GetSecretValue(ctx, &secretsmanager.GetSecretValueInput{
		SecretId: aws.String(secretID),
	})
	if err != nil {
		return "", nil
	}
	var m map[string]string
	if err := json.Unmarshal([]byte(*out.SecretString), &m); err != nil {
		return "", err
	}
	return m[property], nil
}

func setSecretProperty(ctx context.Context, sm *secretsmanager.Client, secretID, property, value string) error {
	out, err := sm.GetSecretValue(ctx, &secretsmanager.GetSecretValueInput{
		SecretId: aws.String(secretID),
	})
	var m map[string]string
	if err != nil {
		m = make(map[string]string)
	} else {
		if err := json.Unmarshal([]byte(*out.SecretString), &m); err != nil {
			m = make(map[string]string)
		}
	}

	m[property] = value
	updated, err := json.Marshal(m)
	if err != nil {
		return err
	}

	_, err = sm.PutSecretValue(ctx, &secretsmanager.PutSecretValueInput{
		SecretId:     aws.String(secretID),
		SecretString: aws.String(string(updated)),
	})
	return err
}
