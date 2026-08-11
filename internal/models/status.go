// Copyright Hermes Notifications
// SPDX-License-Identifier: Apache-2.0
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

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
