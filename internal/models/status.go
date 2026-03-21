package models

type NotificationStatus string

const (
	StatusPending   NotificationStatus = "pending"
	StatusSent      NotificationStatus = "sent"
	StatusDelivered NotificationStatus = "delivered"
	StatusRead      NotificationStatus = "read"
	StatusArchived  NotificationStatus = "archived"
)

var statusRanks = map[NotificationStatus]int{
	StatusPending:   0,
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
