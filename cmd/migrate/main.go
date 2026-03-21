package main

import (
	"flag"
	"fmt"
	"log"
	"os"

	"github.com/hermes-notifications/hermes/internal/database"
)

func main() {
	dbURL := flag.String("database-url", os.Getenv("HERMES_DATABASE_URL"), "PostgreSQL connection URL")
	migrationsPath := flag.String("migrations-path", "/migrations", "Path to migration files")
	flag.Parse()

	if *dbURL == "" {
		log.Fatal("database-url is required (or set HERMES_DATABASE_URL)")
	}

	if err := database.RunMigrations(*dbURL, *migrationsPath); err != nil {
		log.Fatalf("migration failed: %v", err)
	}
	fmt.Println("migrations applied successfully")
}
