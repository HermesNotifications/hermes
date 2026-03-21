package cli

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/centrifugal/centrifuge-go"
	"github.com/hermes-notifications/hermes/pkg/client"
	"github.com/spf13/cobra"
)

func newInboxCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "inbox", Short: "Inbox operations"}
	cmd.AddCommand(newInboxListenCmd())
	return cmd
}

func newInboxListenCmd() *cobra.Command {
	var (
		tenantID      string
		userID        string
		centrifugoURL string
	)

	cmd := &cobra.Command{
		Use: "listen", Short: "Listen for real-time inbox notifications",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			out := cmd.OutOrStdout()
			outputFmt := getOutput(cmd)

			// Step 1: Get unified JWT
			c := newClientFromCmd(cmd)
			tokenResp, err := c.Auth.ExchangeToken(ctx, client.TokenRequest{
				TenantID: tenantID, UserID: userID,
			})
			if err != nil {
				return fmt.Errorf("token exchange failed: %w", err)
			}

			// Parse JWT sub claim to get internal user ID for channel name
			internalUserID, err := parseJWTSubject(tokenResp.Token)
			if err != nil {
				return fmt.Errorf("failed to parse JWT: %w", err)
			}

			channel := "user#" + internalUserID

			// Step 2: Connect to Centrifugo
			wsClient := centrifuge.NewJsonClient(centrifugoURL, centrifuge.Config{})
			wsClient.SetToken(tokenResp.Token)

			wsClient.OnConnecting(func(e centrifuge.ConnectingEvent) {
				fmt.Fprintf(os.Stderr, "Connecting to %s...\n", centrifugoURL)
			})
			wsClient.OnConnected(func(e centrifuge.ConnectedEvent) {
				fmt.Fprintf(os.Stderr, "Connected. Subscribing to %s\n", channel)
			})
			wsClient.OnDisconnected(func(e centrifuge.DisconnectedEvent) {
				fmt.Fprintf(os.Stderr, "Disconnected: %s\n", e.Reason)
			})
			wsClient.OnError(func(e centrifuge.ErrorEvent) {
				fmt.Fprintf(os.Stderr, "Error: %s\n", e.Error.Error())
			})

			// Step 3: Subscribe
			sub, err := wsClient.NewSubscription(channel)
			if err != nil {
				return fmt.Errorf("subscription failed: %w", err)
			}

			w := newTabWriter(out)
			if outputFmt == "table" {
				fmt.Fprintf(os.Stderr, "Listening on %s ...\n", channel)
				printRow(w, "TIME", "ID", "TITLE", "BODY")
				w.Flush()
			}

			sub.OnPublication(func(e centrifuge.PublicationEvent) {
				if outputFmt == "json" {
					out.Write(e.Data)
					fmt.Fprintln(out)
					return
				}
				var event struct {
					ID    string `json:"id"`
					Title string `json:"title"`
					Body  string `json:"body"`
				}
				json.Unmarshal(e.Data, &event)
				printRow(w, dim(time.Now().Format("2006-01-02 15:04:05")), event.ID, bold(event.Title), event.Body)
				w.Flush()
			})

			// Connect first, then subscribe (matches spec flow)
			if err := wsClient.Connect(); err != nil {
				return fmt.Errorf("connect failed: %w", err)
			}
			if err := sub.Subscribe(); err != nil {
				return fmt.Errorf("subscribe failed: %w", err)
			}

			// Step 4: Wait for interrupt
			quit := make(chan os.Signal, 1)
			signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
			<-quit

			fmt.Fprintln(os.Stderr, "\nDisconnecting...")
			wsClient.Close()
			return nil
		},
	}
	cmd.Flags().StringVar(&tenantID, "tenant-id", "", "Tenant ID (required)")
	cmd.Flags().StringVar(&userID, "user-id", "", "User ID (required)")
	cmd.Flags().StringVar(&centrifugoURL, "centrifugo-url", os.Getenv("HERMES_CENTRIFUGO_URL"), "Centrifugo WebSocket URL (env: HERMES_CENTRIFUGO_URL)")
	cmd.MarkFlagRequired("tenant-id")
	cmd.MarkFlagRequired("user-id")
	cmd.MarkFlagRequired("centrifugo-url")
	return cmd
}

func parseJWTSubject(tokenStr string) (string, error) {
	parts := strings.Split(tokenStr, ".")
	if len(parts) != 3 {
		return "", fmt.Errorf("invalid JWT format")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return "", err
	}
	var claims struct {
		Sub string `json:"sub"`
	}
	if err := json.Unmarshal(payload, &claims); err != nil {
		return "", err
	}
	if claims.Sub == "" {
		return "", fmt.Errorf("JWT has no sub claim")
	}
	return claims.Sub, nil
}
