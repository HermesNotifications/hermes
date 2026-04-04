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

	tea "github.com/charmbracelet/bubbletea"
	"github.com/centrifugal/centrifuge-go"
	"github.com/hermes-notifications/hermes/pkg/client"
	"github.com/spf13/cobra"
)

func newInboxCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "inbox", Short: "Inbox operations"}
	cmd.AddCommand(newInboxListenCmd())
	cmd.AddCommand(newInboxOpenCmd())
	return cmd
}

func newInboxOpenCmd() *cobra.Command {
	var (
		tenantID      string
		userID        string
		inboxURL      string
		wsURL string
	)

	cmd := &cobra.Command{
		Use: "open", Short: "Interactive inbox viewer",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			baseURL, _ := cmd.Root().PersistentFlags().GetString("url")
			if inboxURL == "" {
				inboxURL = baseURL
			}
			if wsURL == "" {
				wsURL = httpToWS(baseURL) + "/realtime/connection/websocket"
			}

			// Exchange API key for JWT
			c := newClientFromCmd(cmd)
			tokenResp, err := c.Auth.ExchangeToken(ctx, client.TokenRequest{
				TenantID: tenantID, UserID: userID,
			})
			if err != nil {
				return fmt.Errorf("token exchange failed: %w", err)
			}

			internalUserID, err := parseJWTSubject(tokenResp.Token)
			if err != nil {
				return fmt.Errorf("failed to parse JWT: %w", err)
			}

			// Create inbox client with JWT auth
			inboxClient := client.NewInboxClient(inboxURL, tokenResp.Token)

			// Create and run Bubble Tea program
			model := newInboxModel(inboxClient)
			p := tea.NewProgram(model, tea.WithAltScreen())

			// Setup WebSocket to feed real-time events into the TUI
			wsClient, sub, err := setupWebSocket(wsURL, tokenResp.Token, internalUserID, p)
			if err != nil {
				return fmt.Errorf("websocket setup failed: %w", err)
			}
			defer func() {
				sub.Unsubscribe()
				wsClient.Close()
			}()

			if _, err := p.Run(); err != nil {
				return err
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&tenantID, "tenant-id", "", "Tenant ID (required)")
	cmd.Flags().StringVar(&userID, "user-id", "", "User ID (required)")
	cmd.Flags().StringVar(&inboxURL, "inbox-url", os.Getenv("HERMES_INBOX_URL"), "Inbox service URL override (env: HERMES_INBOX_URL)")
	cmd.Flags().StringVar(&wsURL, "ws-url", os.Getenv("HERMES_WS_URL"), "WebSocket URL override (env: HERMES_WS_URL)")
	cmd.MarkFlagRequired("tenant-id")
	cmd.MarkFlagRequired("user-id")
	return cmd
}

func newInboxListenCmd() *cobra.Command {
	var (
		tenantID string
		userID   string
		wsURL    string
	)

	cmd := &cobra.Command{
		Use: "listen", Short: "Listen for real-time inbox notifications",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			out := cmd.OutOrStdout()
			outputFmt := getOutput(cmd)
			baseURL, _ := cmd.Root().PersistentFlags().GetString("url")
			if wsURL == "" {
				wsURL = httpToWS(baseURL) + "/realtime/connection/websocket"
			}

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

			// Step 2: Connect to WebSocket for real-time events
			wsClient := centrifuge.NewJsonClient(wsURL, centrifuge.Config{})
			wsClient.SetToken(tokenResp.Token)

			wsClient.OnConnecting(func(e centrifuge.ConnectingEvent) {
				fmt.Fprintf(os.Stderr, "Connecting to %s...\n", wsURL)
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
	cmd.Flags().StringVar(&wsURL, "ws-url", os.Getenv("HERMES_WS_URL"), "WebSocket URL override (env: HERMES_WS_URL)")
	cmd.MarkFlagRequired("tenant-id")
	cmd.MarkFlagRequired("user-id")
	return cmd
}

// httpToWS converts an HTTP(S) URL to a WS(S) URL.
func httpToWS(url string) string {
	url = strings.TrimRight(url, "/")
	url = strings.Replace(url, "https://", "wss://", 1)
	url = strings.Replace(url, "http://", "ws://", 1)
	return url
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
