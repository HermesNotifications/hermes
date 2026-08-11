// Copyright 2026 Hermes Notifications. Licensed under the Apache License, Version 2.0.
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

package postgres

import (
	"context"
	"fmt"
)

// GetUserContacts returns the user's contact points: address key -> address.
func (s *Store) GetUserContacts(ctx context.Context, userID string) (map[string]string, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT address_key, address FROM user_contact_points WHERE user_id = $1`, userID)
	if err != nil {
		return nil, fmt.Errorf("get user contacts: %w", err)
	}
	defer rows.Close()

	out := map[string]string{}
	for rows.Next() {
		var key, addr string
		if err := rows.Scan(&key, &addr); err != nil {
			return nil, fmt.Errorf("scan user contact: %w", err)
		}
		out[key] = addr
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(out) == 0 {
		return nil, nil
	}
	return out, nil
}

// SetUserContact upserts a single contact point (verified defaults to false on insert,
// preserved on update).
func (s *Store) SetUserContact(ctx context.Context, userID, addressKey, address string) error {
	if _, err := s.pool.Exec(ctx,
		`INSERT INTO user_contact_points (user_id, address_key, address)
		 VALUES ($1, $2, $3)
		 ON CONFLICT (user_id, address_key) DO UPDATE SET address = EXCLUDED.address`,
		userID, addressKey, address); err != nil {
		return fmt.Errorf("set user contact: %w", err)
	}
	return nil
}
