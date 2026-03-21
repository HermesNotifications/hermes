package cli

import (
	"encoding/json"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/centrifugal/centrifuge-go"
	"github.com/hermes-notifications/hermes/pkg/client"
)

func setupWebSocket(centrifugoURL, jwt, internalUserID string, program *tea.Program) (*centrifuge.Client, *centrifuge.Subscription, error) {
	channel := "user#" + internalUserID

	wsClient := centrifuge.NewJsonClient(centrifugoURL, centrifuge.Config{})
	wsClient.SetToken(jwt)

	sub, err := wsClient.NewSubscription(channel)
	if err != nil {
		return nil, nil, err
	}

	sub.OnPublication(func(e centrifuge.PublicationEvent) {
		var payload struct {
			ID        string  `json:"id"`
			Title     string  `json:"title"`
			Body      string  `json:"body"`
			CreatedAt string  `json:"created_at"`
			Action    *struct {
				URL   string `json:"url,omitempty"`
				Label string `json:"label,omitempty"`
			} `json:"action,omitempty"`
		}
		if err := json.Unmarshal(e.Data, &payload); err != nil {
			return
		}

		createdAt, _ := time.Parse(time.RFC3339, payload.CreatedAt)
		if createdAt.IsZero() {
			createdAt = time.Now()
		}

		n := client.InboxNotification{
			ID:        payload.ID,
			Title:     payload.Title,
			Body:      payload.Body,
			Status:    "delivered",
			Channels:  []string{"inbox"},
			CreatedAt: createdAt,
		}
		if payload.Action != nil {
			if payload.Action.URL != "" {
				n.ActionURL = &payload.Action.URL
			}
			if payload.Action.Label != "" {
				n.ActionLabel = &payload.Action.Label
			}
		}

		program.Send(newNotifMsg{notif: n})
	})

	if err := wsClient.Connect(); err != nil {
		return nil, nil, err
	}
	if err := sub.Subscribe(); err != nil {
		wsClient.Close()
		return nil, nil, err
	}

	return wsClient, sub, nil
}
