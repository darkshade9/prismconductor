package slack

import (
	"fmt"
	"strconv"
	"strings"
)

// ParsedCommand is the result of parsing a @conductor mention text.
type ParsedCommand struct {
	Verb string   // e.g. "plan", "approve", "cancel", "list", "status", "cost", "goal", "help"
	Args []string // remaining tokens after the verb
}

// ParseMention extracts the command from a raw app_mention text.
// It strips the bot mention (<@UXXXXXXXX>) and splits into verb + args.
// Returns ok=false when the text contains no recognizable command.
func ParseMention(text, botUserID string) (ParsedCommand, bool) {
	// Strip bot mention token(s), e.g. "<@U0123456>"
	mention := "<@" + botUserID + ">"
	t := strings.ReplaceAll(text, mention, "")
	t = strings.TrimSpace(t)
	if t == "" {
		return ParsedCommand{}, false
	}
	parts := strings.Fields(t)
	if len(parts) == 0 {
		return ParsedCommand{}, false
	}
	verb := strings.ToLower(parts[0])
	return ParsedCommand{Verb: verb, Args: parts[1:]}, true
}

// Dispatcher routes parsed commands to AppFacade calls and returns a
// human-readable Slack response string.
type Dispatcher struct {
	authz *AuthRegistry
	app   AppFacade
}

// NewDispatcher creates a Dispatcher.
func NewDispatcher(app AppFacade, authz *AuthRegistry) *Dispatcher {
	return &Dispatcher{app: app, authz: authz}
}

// Dispatch executes cmd on behalf of slackUserID in channelID and returns the
// response text to post back.
func (d *Dispatcher) Dispatch(cmd ParsedCommand, slackUserID, channelID string) string {
	switch cmd.Verb {
	case "help":
		return helpText()
	case "list":
		if !d.authz.CanRead("", slackUserID) {
			return d.app.SlackCommandListWorkspaces()
		}
		return d.app.SlackCommandListWorkspaces()
	case "status":
		wsID, err := d.resolveWorkspace(cmd.Args, channelID)
		if err != nil {
			return fmt.Sprintf(":warning: %s", err)
		}
		return d.app.SlackCommandStatus(wsID)
	case "plan":
		wsID, issueNum, err := d.resolveWorkspaceAndIssue(cmd.Args, channelID)
		if err != nil {
			return fmt.Sprintf(":warning: %s", err)
		}
		if !d.authz.CanMutate(wsID, slackUserID) {
			return ":lock: You are not authorized to run mutating commands in that workspace."
		}
		if err := d.app.SlackCommandPlan(wsID, issueNum); err != nil {
			return fmt.Sprintf(":x: plan failed: %s", err)
		}
		return fmt.Sprintf(":hourglass_flowing_sand: Planning started for #%d.", issueNum)
	case "approve":
		wsID, issueNum, err := d.resolveWorkspaceAndIssue(cmd.Args, channelID)
		if err != nil {
			return fmt.Sprintf(":warning: %s", err)
		}
		if !d.authz.CanMutate(wsID, slackUserID) {
			return ":lock: You are not authorized to run mutating commands in that workspace."
		}
		if err := d.app.SlackCommandApprove(wsID, issueNum); err != nil {
			return fmt.Sprintf(":x: approve failed: %s", err)
		}
		return fmt.Sprintf(":white_check_mark: Approved plan for #%d — execute session spawned.", issueNum)
	case "cancel":
		wsID, issueNum, err := d.resolveWorkspaceAndIssue(cmd.Args, channelID)
		if err != nil {
			return fmt.Sprintf(":warning: %s", err)
		}
		if !d.authz.CanMutate(wsID, slackUserID) {
			return ":lock: You are not authorized to run mutating commands in that workspace."
		}
		if err := d.app.SlackCommandCancel(wsID, issueNum); err != nil {
			return fmt.Sprintf(":x: cancel failed: %s", err)
		}
		return fmt.Sprintf(":octagonal_sign: Cancelled active session for #%d.", issueNum)
	case "cost":
		wsID, err := d.resolveWorkspace(cmd.Args, channelID)
		if err != nil {
			return fmt.Sprintf(":warning: %s", err)
		}
		return d.app.SlackCommandCostThisWeek(wsID)
	case "goal":
		// @conductor goal <goal-id-or-name> status
		if len(cmd.Args) < 2 || strings.ToLower(cmd.Args[len(cmd.Args)-1]) != "status" {
			return ":question: Usage: `@conductor goal <goal-id> status`"
		}
		goalID := strings.Join(cmd.Args[:len(cmd.Args)-1], " ")
		return d.app.SlackCommandGoalStatus(goalID)
	default:
		return fmt.Sprintf(":question: Unknown command `%s`. Try `@conductor help`.", cmd.Verb)
	}
}

// resolveWorkspace derives a workspace ID from args or falls back to channel mapping.
func (d *Dispatcher) resolveWorkspace(args []string, channelID string) (string, error) {
	// If first arg looks like a workspace slug, try it.
	if len(args) > 0 && !strings.HasPrefix(args[0], "#") {
		return args[0], nil
	}
	wsID, ok := d.app.SlackResolveWorkspaceByChannel(channelID)
	if !ok {
		return "", fmt.Errorf("no workspace is mapped to this channel — use `@conductor status <workspace-id>`")
	}
	return wsID, nil
}

// resolveWorkspaceAndIssue extracts workspace + issue number from args like
// "#42" or "workspace-id #42", falling back to channel mapping for the workspace.
func (d *Dispatcher) resolveWorkspaceAndIssue(args []string, channelID string) (string, int, error) {
	if len(args) == 0 {
		return "", 0, fmt.Errorf("missing issue number — e.g. `@conductor plan #42`")
	}

	// Find the issue number token (starts with #).
	issueToken := ""
	wsSlug := ""
	for _, a := range args {
		if strings.HasPrefix(a, "#") {
			issueToken = strings.TrimPrefix(a, "#")
		} else {
			wsSlug = a
		}
	}
	if issueToken == "" {
		// Try bare number as last arg.
		last := args[len(args)-1]
		n, err := strconv.Atoi(last)
		if err != nil {
			return "", 0, fmt.Errorf("cannot parse issue number from `%s`", strings.Join(args, " "))
		}
		issueToken = strconv.Itoa(n)
		if len(args) > 1 {
			wsSlug = strings.Join(args[:len(args)-1], " ")
		}
	}

	issueNum, err := strconv.Atoi(issueToken)
	if err != nil {
		return "", 0, fmt.Errorf("invalid issue number: %s", issueToken)
	}

	var wsID string
	if wsSlug != "" {
		wsID = wsSlug
	} else {
		var ok bool
		wsID, ok = d.app.SlackResolveWorkspaceByChannel(channelID)
		if !ok {
			return "", 0, fmt.Errorf("no workspace mapped to this channel — specify a workspace: `@conductor plan <workspace-id> #42`")
		}
	}
	return wsID, issueNum, nil
}

func helpText() string {
	return `:robot_face: *PrismConductor commands*

• ` + "`@conductor list`" + ` — list workspaces with status
• ` + "`@conductor status [workspace]`" + ` — workspace overview (cards, cost, active workers)
• ` + "`@conductor plan [workspace] #<issue>`" + ` — spawn planner for an issue
• ` + "`@conductor approve [workspace] #<issue>`" + ` — approve ready plan and start execute
• ` + "`@conductor cancel [workspace] #<issue>`" + ` — cancel active session
• ` + "`@conductor cost [workspace]`" + ` — spending this week
• ` + "`@conductor goal <goal-id> status`" + ` — goal progress
• ` + "`@conductor help`" + ` — this message

Mutating commands (plan/approve/cancel) require your Slack user to be mapped in Settings → Slack.`
}
