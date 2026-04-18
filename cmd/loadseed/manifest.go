package main

import (
	"encoding/json"
	"fmt"
	"os"
)

type Manifest struct {
	SeededAt  string   `json:"seeded_at"`
	RunSeedID string   `json:"run_seed_id"`
	APIKey    string   `json:"api_key"`
	Tenants   []Tenant `json:"tenants"`
}

type Tenant struct {
	ID         string     `json:"id"`
	Users      []string   `json:"users"`
	Categories []Category `json:"categories"`
}

type Category struct {
	ID            string         `json:"id"`
	Subscriptions []Subscription `json:"subscriptions"`
}

type Subscription struct {
	ID        string     `json:"id"`
	Templates []Template `json:"templates"`
}

type Template struct {
	ID       string   `json:"id"`
	Slug     string   `json:"slug"`
	Channels []string `json:"channels"`
}

func (m *Manifest) Write(path string) error {
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal manifest: %w", err)
	}
	if err := os.WriteFile(path, b, 0o600); err != nil {
		return fmt.Errorf("write manifest: %w", err)
	}
	return nil
}

func ReadManifest(path string) (*Manifest, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read manifest: %w", err)
	}
	var m Manifest
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, fmt.Errorf("parse manifest: %w", err)
	}
	return &m, nil
}
