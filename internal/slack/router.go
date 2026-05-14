package slack

import (
	"fmt"
	"log"
	"strings"
	"sync"

	"github.com/slack-go/slack/slackevents"
	"github.com/slack-go/slack/socketmode"
)

// EventType identifies conductor lifecycle events that can be routed to Slack.
type EventType string

const (
	EvtPlanReady    EventType = "plan_ready"
	EvtBlocked      EventType = "blocked"
	EvtCompleted    EventType = "completed"
	EvtFailed       EventType = "failed"
	EvtBudgetAlert  EventType = "budget_alert"
	EvtAutoArchive  EventType = "auto_archive"
)

// EventRouting controls which lifecycle events are posted to Slack per workspace.
type EventRouting struct {
	PlanReady   bool `json:"plan_ready"`
	Blocked     bool `json:"blocked"`
	Completed   bool `json:"completed"`
	BudgetAlert bool `json:"budget_alert"`
	AutoArchive bool `json:"auto_archive"`
}

// DefaultEventRouting returns a routing config with sensible on/off defaults.
func DefaultEventRouting() EventRouting {
	return EventRouting{
		PlanReady:   true,
		Blocked:     true,
		Completed:   false,
		BudgetAlert: true,
		AutoArchive: false,
	}
}

// WorkspaceRoute is the per-workspace routing configuration used by the Router.
type WorkspaceRoute struct {
	Channel  string
	Routing  EventRouting
	Muted    bool
}

// Router fans conductor lifecycle events out to Slack channels, respecting
// per-workspace mute/routing toggles. It is safe for concurrent use.
type Router struct {
	mu     sync.RWMutex
	routes map[string]WorkspaceRoute // wsID → route
	mgr    *Manager
}

// NewRouter creates a Router backed by mgr.
func NewRouter(mgr *Manager) *Router {
	return &Router{
		routes: make(map[string]WorkspaceRoute),
		mgr:    mgr,
	}
}

// SetWorkspaceRoute replaces the routing config for workspaceID.
func (r *Router) SetWorkspaceRoute(workspaceID string, route WorkspaceRoute) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.routes[workspaceID] = route
}

// RemoveWorkspaceRoute removes routing for a workspace (e.g. on disconnect).
func (r *Router) RemoveWorkspaceRoute(workspaceID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.routes, workspaceID)
}

// Notify posts a lifecycle notification to the configured channel for workspaceID,
// if the event type is enabled and the workspace is not muted.
func (r *Router) Notify(workspaceID string, evt EventType, text string) {
	r.mu.RLock()
	route, ok := r.routes[workspaceID]
	r.mu.RUnlock()
	if !ok || route.Channel == "" || route.Muted {
		return
	}
	if !r.eventEnabled(route.Routing, evt) {
		return
	}
	if err := r.mgr.PostMessage(route.Channel, text); err != nil {
		log.Printf("slack router: post to %s: %v", route.Channel, err)
	}
}

func (r *Router) eventEnabled(routing EventRouting, evt EventType) bool {
	switch evt {
	case EvtPlanReady:
		return routing.PlanReady
	case EvtBlocked, EvtFailed:
		return routing.Blocked
	case EvtCompleted:
		return routing.Completed
	case EvtBudgetAlert:
		return routing.BudgetAlert
	case EvtAutoArchive:
		return routing.AutoArchive
	}
	return false
}

// NotifyPlanReady posts a plan-ready notification with issue context.
func (r *Router) NotifyPlanReady(workspaceID, workspaceName string, issueNum int, issueTitle string) {
	text := fmt.Sprintf(":pencil: *%s* — Plan ready for *#%d: %s*\nApprove with `@conductor approve #%d`",
		workspaceName, issueNum, issueTitle, issueNum)
	r.Notify(workspaceID, EvtPlanReady, text)
}

// NotifyBlocked posts a blocked/failed notification.
func (r *Router) NotifyBlocked(workspaceID, workspaceName string, issueNum int, reason string) {
	short := reason
	if len(short) > 200 {
		short = short[:197] + "..."
	}
	text := fmt.Sprintf(":red_circle: *%s* — #%d is blocked\n```%s```", workspaceName, issueNum, short)
	r.Notify(workspaceID, EvtBlocked, text)
}

// NotifyCompleted posts a completion notification.
func (r *Router) NotifyCompleted(workspaceID, workspaceName string, issueNum int) {
	text := fmt.Sprintf(":white_check_mark: *%s* — #%d completed", workspaceName, issueNum)
	r.Notify(workspaceID, EvtCompleted, text)
}

// Handler dispatches Socket Mode events to command handlers.
type Handler struct {
	mgr    *Manager
	disp   *Dispatcher
}

func newHandler(mgr *Manager, authz *AuthRegistry) *Handler {
	return &Handler{
		mgr:  mgr,
		disp: NewDispatcher(mgr.app, authz),
	}
}

func (h *Handler) dispatch(evt socketmode.Event) {
	switch evt.Type {
	case socketmode.EventTypeEventsAPI:
		h.mgr.ack(evt)
		h.handleEventsAPI(evt)
	case socketmode.EventTypeSlashCommand:
		h.mgr.ack(evt)
	case socketmode.EventTypeInteractive:
		h.mgr.ack(evt)
	default:
		h.mgr.ack(evt)
	}
}

func (h *Handler) handleEventsAPI(evt socketmode.Event) {
	payload, ok := evt.Data.(slackevents.EventsAPIEvent)
	if !ok {
		return
	}
	switch payload.Type {
	case "event_callback":
		h.handleInnerEvent(payload)
	}
}

func (h *Handler) handleInnerEvent(payload slackevents.EventsAPIEvent) {
	switch payload.InnerEvent.Type {
	case "app_mention":
		h.handleMention(payload)
	}
}

func (h *Handler) handleMention(payload slackevents.EventsAPIEvent) {
	ev, ok := payload.InnerEvent.Data.(*slackevents.AppMentionEvent)
	if !ok {
		return
	}

	// Ignore messages from the bot itself to avoid loops.
	if ev.User == h.mgr.BotUserID() {
		return
	}

	cmd, ok := ParseMention(ev.Text, h.mgr.BotUserID())
	if !ok {
		return
	}

	// Resolve a default workspace via channel mapping.
	channelID := strings.TrimSpace(ev.Channel)
	slackUserID := ev.User

	resp := h.disp.Dispatch(cmd, slackUserID, channelID)

	if err := h.mgr.PostMessage(channelID, resp); err != nil {
		log.Printf("slack handler: reply to mention: %v", err)
	}
}
