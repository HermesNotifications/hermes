// Copyright 2026 Hermes Notifications. Licensed under the Apache License, Version 2.0.
// See LICENSE and NOTICE in the project root for full terms and restrictions.

package models

type NotificationStatus string

const (
	StatusPending   NotificationStatus = "pending"
	StatusFailed    NotificationStatus = "failed"
	StatusSent      NotificationStatus = "sent"
	StatusDelivered NotificationStatus = "delivered"
	StatusRead      NotificationStatus = "read"
	StatusArchived  NotificationStatus = "archived"
)

var statusRanks = map[NotificationStatus]int{
	StatusPending:   0,
	StatusFailed:    0, // same rank as pending — failed is a terminal state, not an advancement
	StatusSent:      1,
	StatusDelivered: 2,
	StatusRead:      3,
	StatusArchived:  4,
}

func (s NotificationStatus) Rank() int {
	return statusRanks[s]
}

func (s NotificationStatus) String() string {
	return string(s)
}
