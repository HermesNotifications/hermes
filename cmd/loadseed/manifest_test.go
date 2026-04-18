package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestManifest_RoundTrip(t *testing.T) {
	m := &Manifest{
		SeededAt:  "2026-04-17T00:00:00Z",
		RunSeedID: "abc123",
		APIKey:    "hms_dev_key_xxx_yyy",
		Tenants: []Tenant{
			{
				ID:    "t1",
				Users: []string{"u1", "u2"},
				Categories: []Category{
					{ID: "c1", Subscriptions: []Subscription{
						{ID: "s1", Templates: []Template{
							{ID: "tmpl1", Channels: []string{"inbox", "email"}},
						}},
					}},
				},
			},
		},
	}
	path := filepath.Join(t.TempDir(), "manifest.json")
	if err := m.Write(path); err != nil {
		t.Fatalf("write: %v", err)
	}
	got, err := ReadManifest(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if got.APIKey != m.APIKey || len(got.Tenants) != 1 || got.Tenants[0].Categories[0].Subscriptions[0].Templates[0].ID != "tmpl1" {
		t.Fatalf("mismatch: %+v", got)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("file not created: %v", err)
	}
}
