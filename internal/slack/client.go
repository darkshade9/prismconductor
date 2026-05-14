// Package slack manages the Slack bot integration for PrismConductor.
// v1 scope: OAuth/connect, notifications, core commands via Socket Mode.
// Thread-reply verbs and mid-run question routing are deferred to v1.1.
package slack

import (
	"context"
	"fmt"
	"log"
	"sync"

	goslack "github.com/slack-go/slack"
	"github.com/slack-go/slack/socketmode"
)

// AppFacade is the minimal interface the Manager calls back into app.go.
// Keeping it narrow prevents the slack package from importing the entire App.
type AppFacade interface {
	SlackCommandListWorkspaces() string
	SlackCommandStatus(workspaceID string) string
	SlackCommandPlan(workspaceID string, issueNumber int) error
	SlackCommandApprove(workspaceID string, issueNumber int) error
	SlackCommandCancel(workspaceID string, issueNumber int) error
	SlackCommandCostThisWeek(workspaceID string) string
	SlackCommandGoalStatus(goalID string) string
	SlackResolveWorkspaceByChannel(channelID string) (workspaceID string, ok bool)
}

// Manager owns the Socket Mode connection and coordinates inbound events.
type Manager struct {
	mu      sync.Mutex
	client  *goslack.Client
	sm      *socketmode.Client
	handler *Handler

	cancel context.CancelFunc
	done   chan struct{}

	app AppFacade
	// botID is the bot's own Slack user ID; used to strip self-mentions.
	botID string
}

// Config holds the runtime credentials for one workspace's Slack bot.
type Config struct {
	BotToken      string
	AppLevelToken string // required for Socket Mode (xapp- prefix)
	WorkspaceID   string // conductor workspace ID for channel resolution
}

// NewManager creates a Manager. Call Start to open the socket.
func NewManager(cfg Config, app AppFacade, authz *AuthRegistry) (*Manager, error) {
	if cfg.BotToken == "" || cfg.AppLevelToken == "" {
		return nil, fmt.Errorf("slack: bot token and app-level token are required")
	}

	slackClient := goslack.New(cfg.BotToken,
		goslack.OptionAppLevelToken(cfg.AppLevelToken),
	)

	// Verify the token and capture the bot user ID.
	ai, err := slackClient.AuthTest()
	if err != nil {
		return nil, fmt.Errorf("slack auth test: %w", err)
	}

	sm := socketmode.New(slackClient,
		socketmode.OptionDebug(false),
		socketmode.OptionLog(log.New(log.Writer(), "slack-sm: ", log.LstdFlags|log.Lshortfile)),
	)

	m := &Manager{
		client: slackClient,
		sm:     sm,
		app:    app,
		botID:  ai.UserID,
		done:   make(chan struct{}),
	}
	m.handler = newHandler(m, authz)
	return m, nil
}

// Start opens the Socket Mode connection and processes events until ctx is done.
func (m *Manager) Start(ctx context.Context) {
	ctx, cancel := context.WithCancel(ctx)
	m.mu.Lock()
	m.cancel = cancel
	m.mu.Unlock()

	go func() {
		defer close(m.done)
		go m.sm.RunContext(ctx) //nolint:errcheck — run loop errors are logged internally

		for {
			select {
			case <-ctx.Done():
				return
			case evt, ok := <-m.sm.Events:
				if !ok {
					return
				}
				m.handler.dispatch(evt)
			}
		}
	}()
}

// Stop gracefully shuts down the socket connection.
func (m *Manager) Stop() {
	m.mu.Lock()
	cancel := m.cancel
	m.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	<-m.done
}

// PostMessage sends a plain text message to a channel.
func (m *Manager) PostMessage(channelID, text string) error {
	_, _, err := m.client.PostMessage(channelID,
		goslack.MsgOptionText(text, false),
	)
	return err
}

// PostMessageBlocks sends a rich block-kit message to a channel.
func (m *Manager) PostMessageBlocks(channelID string, blocks ...goslack.Block) error {
	_, _, err := m.client.PostMessage(channelID,
		goslack.MsgOptionBlocks(blocks...),
	)
	return err
}

// PostThreadReply sends a reply in an existing thread.
func (m *Manager) PostThreadReply(channelID, threadTS, text string) error {
	_, _, err := m.client.PostMessage(channelID,
		goslack.MsgOptionText(text, false),
		goslack.MsgOptionTS(threadTS),
	)
	return err
}

// ack acknowledges a socket-mode event so Slack doesn't retry it.
func (m *Manager) ack(evt socketmode.Event) {
	m.sm.Ack(*evt.Request)
}

// BotUserID returns the bot's Slack user ID.
func (m *Manager) BotUserID() string { return m.botID }
