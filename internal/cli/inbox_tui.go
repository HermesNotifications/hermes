package cli

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/hermes-notifications/hermes/pkg/client"
)

// -- Styles --

var (
	titleStyle  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("15"))
	dimStyle    = lipgloss.NewStyle().Faint(true)
	unreadDot   = lipgloss.NewStyle().Foreground(lipgloss.Color("33")).Render("●")
	readDot     = lipgloss.NewStyle().Faint(true).Render(" ")
	cursorStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("33")).Bold(true)
	selectedBg  = lipgloss.NewStyle().Background(lipgloss.Color("236"))
	statusOk    = lipgloss.NewStyle().Foreground(lipgloss.Color("34"))
	statusWarn  = lipgloss.NewStyle().Foreground(lipgloss.Color("214"))
	helpStyle   = lipgloss.NewStyle().Faint(true)
	headerStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("33"))
	hrStyle     = lipgloss.NewStyle().Faint(true)
	labelTUI    = lipgloss.NewStyle().Foreground(lipgloss.Color("33"))
	bodyStyle   = lipgloss.NewStyle()
)

// -- Messages --

type fetchedMsg struct {
	resp *client.InboxListResponse
	err  error
}

type actionDoneMsg struct {
	action string
	err    error
}

type newNotifMsg struct {
	notif client.InboxNotification
}

type clearStatusMsg struct{}

// -- Model --

type inboxModel struct {
	inbox   *client.InboxClient
	program *tea.Program

	items        []client.InboxNotification
	cursor       int
	unreadCount  int
	nextCursor   string
	showArchived bool
	view         string // "list" | "detail"
	width        int
	height       int
	loading      bool
	err          error
	statusMsg    string
}

func newInboxModel(inboxClient *client.InboxClient) inboxModel {
	return inboxModel{
		inbox: inboxClient,
		view:  "list",
	}
}

func (m inboxModel) Init() tea.Cmd {
	return m.fetchInbox()
}

func (m inboxModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case fetchedMsg:
		m.loading = false
		if msg.err != nil {
			m.err = msg.err
			return m, nil
		}
		m.items = msg.resp.Data
		m.unreadCount = msg.resp.UnreadCount
		m.nextCursor = msg.resp.Cursor
		if m.cursor >= len(m.items) && len(m.items) > 0 {
			m.cursor = len(m.items) - 1
		}
		return m, nil

	case actionDoneMsg:
		if msg.err != nil {
			m.statusMsg = fmt.Sprintf("Error: %v", msg.err)
		} else {
			m.statusMsg = msg.action
		}
		return m, tea.Batch(m.fetchInbox(), m.clearStatusAfter(2*time.Second))

	case newNotifMsg:
		// New notification arrived via WebSocket — refresh the list
		return m, m.fetchInbox()

	case clearStatusMsg:
		m.statusMsg = ""
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)
	}
	return m, nil
}

func (m inboxModel) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q", "ctrl+c":
		return m, tea.Quit

	case "tab":
		if m.view == "list" {
			m.showArchived = !m.showArchived
			m.cursor = 0
			m.loading = true
			return m, m.fetchInbox()
		}

	case "j", "down":
		if m.view == "list" && m.cursor < len(m.items)-1 {
			m.cursor++
		}

	case "k", "up":
		if m.view == "list" && m.cursor > 0 {
			m.cursor--
		}

	case "enter":
		if m.view == "list" && len(m.items) > 0 {
			m.view = "detail"
		}

	case "esc":
		if m.view == "detail" {
			m.view = "list"
		}

	case "r":
		if n := m.selected(); n != nil && n.Status != "read" {
			return m, m.doAction("Marked as read", func(ctx context.Context) error {
				return m.inbox.MarkRead(ctx, n.ID)
			})
		}

	case "u":
		if n := m.selected(); n != nil && n.Status == "read" {
			return m, m.doAction("Marked as unread", func(ctx context.Context) error {
				return m.inbox.MarkUnread(ctx, n.ID)
			})
		}

	case "a":
		if n := m.selected(); n != nil {
			if m.showArchived {
				return m, m.doAction("Unarchived", func(ctx context.Context) error {
					return m.inbox.Unarchive(ctx, n.ID)
				})
			}
			return m, m.doAction("Archived", func(ctx context.Context) error {
				return m.inbox.Archive(ctx, n.ID)
			})
		}

	case "d":
		if m.view == "detail" {
			if n := m.selected(); n != nil {
				m.view = "list"
				return m, m.doAction("Deleted", func(ctx context.Context) error {
					return m.inbox.Delete(ctx, n.ID)
				})
			}
		}

	case "R":
		if m.view == "list" && !m.showArchived {
			return m, m.doAction("All marked as read", func(ctx context.Context) error {
				return m.inbox.MarkAllRead(ctx)
			})
		}
	}
	return m, nil
}

func (m inboxModel) View() string {
	if m.err != nil {
		return fmt.Sprintf("\n  Error: %v\n\n  Press q to quit.\n", m.err)
	}
	if m.width == 0 {
		return "  Loading..."
	}
	if m.view == "detail" {
		return m.viewDetail()
	}
	return m.viewList()
}

func (m inboxModel) viewList() string {
	var b strings.Builder
	w := m.width
	if w > 100 {
		w = 100
	}

	// Header
	tag := "[active]"
	if m.showArchived {
		tag = "[archived]"
	}
	title := fmt.Sprintf("Inbox (%d unread)", m.unreadCount)
	padding := w - lipgloss.Width(title) - lipgloss.Width(tag)
	if padding < 1 {
		padding = 1
	}
	b.WriteString(" " + headerStyle.Render(title) + strings.Repeat(" ", padding) + dimStyle.Render(tag) + "\n")
	b.WriteString(" " + hrStyle.Render(strings.Repeat("─", w-1)) + "\n")

	// Items
	if m.loading && len(m.items) == 0 {
		b.WriteString("  Loading...\n")
	} else if len(m.items) == 0 {
		msg := "No notifications"
		if m.showArchived {
			msg = "No archived notifications"
		}
		b.WriteString("  " + dimStyle.Render(msg) + "\n")
	}

	// Calculate visible items for scrolling
	listHeight := m.height - 5 // header(2) + hr(1) + footer(2)
	if listHeight < 3 {
		listHeight = 3
	}
	start := 0
	if m.cursor >= listHeight {
		start = m.cursor - listHeight + 1
	}
	end := start + listHeight
	if end > len(m.items) {
		end = len(m.items)
	}

	for i := start; i < end; i++ {
		n := m.items[i]
		isCurrent := i == m.cursor

		// Cursor + unread indicator
		prefix := "   "
		if isCurrent {
			prefix = cursorStyle.Render(" > ")
		}
		dot := readDot
		if n.Status == "delivered" || n.Status == "sent" || n.Status == "pending" {
			dot = unreadDot
		}

		// Title (truncated)
		titleWidth := w - 30 // room for prefix, dot, timestamp
		if titleWidth < 10 {
			titleWidth = 10
		}
		title := n.Title
		if lipgloss.Width(title) > titleWidth {
			title = title[:titleWidth-1] + "…"
		}

		// Relative time
		ago := relativeTime(n.CreatedAt)

		// Compose line
		line := fmt.Sprintf("%s %s %s", prefix, dot, title)
		padLen := w - lipgloss.Width(line) - lipgloss.Width(ago) - 1
		if padLen < 1 {
			padLen = 1
		}
		line += strings.Repeat(" ", padLen) + dimStyle.Render(ago)

		if isCurrent {
			line = selectedBg.Render(line)
		}
		b.WriteString(line + "\n")
	}

	// Footer
	b.WriteString("\n " + hrStyle.Render(strings.Repeat("─", w-1)) + "\n")

	// Status message or help
	if m.statusMsg != "" {
		b.WriteString(" " + statusOk.Render(m.statusMsg))
	} else {
		help := "tab archived  enter open  r read  u unread  a archive  R all read  q quit"
		if m.showArchived {
			help = "tab active  enter open  a unarchive  q quit"
		}
		b.WriteString(" " + helpStyle.Render(help))
	}

	return b.String()
}

func (m inboxModel) viewDetail() string {
	n := m.items[m.cursor]
	var b strings.Builder
	w := m.width
	if w > 100 {
		w = 100
	}

	// Header: title + status
	statusStr := colorStatusTUI(n.Status)
	titleStr := titleStyle.Render(n.Title)
	padding := w - lipgloss.Width(n.Title) - lipgloss.Width(n.Status) - 2
	if padding < 1 {
		padding = 1
	}
	b.WriteString(" " + titleStr + strings.Repeat(" ", padding) + statusStr + "\n")
	b.WriteString(" " + hrStyle.Render(strings.Repeat("─", w-1)) + "\n")

	// Body
	b.WriteString(" " + bodyStyle.Render(n.Body) + "\n")

	// Action
	if n.ActionURL != nil && *n.ActionURL != "" {
		lbl := "Open"
		if n.ActionLabel != nil && *n.ActionLabel != "" {
			lbl = *n.ActionLabel
		}
		b.WriteString("\n " + labelTUI.Render("→") + " " + lbl + "  " + dimStyle.Render(*n.ActionURL) + "\n")
	}

	// Timestamps
	b.WriteString("\n " + labelTUI.Render("Created:") + " " + fmtTimeTUI(n.CreatedAt) + "\n")

	b.WriteString("\n " + hrStyle.Render(strings.Repeat("─", w-1)) + "\n")

	// Footer
	if m.statusMsg != "" {
		b.WriteString(" " + statusOk.Render(m.statusMsg))
	} else {
		b.WriteString(" " + helpStyle.Render("esc back  r read  u unread  a archive  d delete  q quit"))
	}

	return b.String()
}

// -- Helpers --

func (m inboxModel) selected() *client.InboxNotification {
	if m.cursor < 0 || m.cursor >= len(m.items) {
		return nil
	}
	return &m.items[m.cursor]
}

func (m inboxModel) fetchInbox() tea.Cmd {
	return func() tea.Msg {
		resp, err := m.inbox.List(context.Background(), m.showArchived, "", 50)
		return fetchedMsg{resp: resp, err: err}
	}
}

func (m inboxModel) doAction(label string, fn func(ctx context.Context) error) tea.Cmd {
	return func() tea.Msg {
		err := fn(context.Background())
		return actionDoneMsg{action: label, err: err}
	}
}

func (m inboxModel) clearStatusAfter(d time.Duration) tea.Cmd {
	return tea.Tick(d, func(time.Time) tea.Msg {
		return clearStatusMsg{}
	})
}

func colorStatusTUI(s string) string {
	switch s {
	case "delivered", "read", "sent":
		return statusOk.Render(s)
	case "pending":
		return statusWarn.Render(s)
	default:
		return dimStyle.Render(s)
	}
}

func relativeTime(t time.Time) string {
	ago := time.Since(t).Truncate(time.Second)
	switch {
	case ago < time.Minute:
		return fmt.Sprintf("%ds ago", int(ago.Seconds()))
	case ago < time.Hour:
		return fmt.Sprintf("%dm ago", int(ago.Minutes()))
	case ago < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(ago.Hours()))
	default:
		return fmt.Sprintf("%dd ago", int(ago.Hours()/24))
	}
}

func fmtTimeTUI(t time.Time) string {
	return t.Local().Format("2006-01-02 15:04:05") + " " + dimStyle.Render("("+relativeTime(t)+")")
}
