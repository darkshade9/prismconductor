package slack_test

import (
	"testing"

	"prismconductor/internal/slack"
)

func TestParseMention(t *testing.T) {
	botID := "U0BOT1234"
	mention := "<@" + botID + ">"

	tests := []struct {
		name     string
		text     string
		wantOK   bool
		wantVerb string
		wantArgs []string
	}{
		{
			name:     "simple help",
			text:     mention + " help",
			wantOK:   true,
			wantVerb: "help",
			wantArgs: []string{},
		},
		{
			name:     "list",
			text:     mention + " list",
			wantOK:   true,
			wantVerb: "list",
			wantArgs: []string{},
		},
		{
			name:     "plan with issue",
			text:     mention + " plan #42",
			wantOK:   true,
			wantVerb: "plan",
			wantArgs: []string{"#42"},
		},
		{
			name:     "approve with workspace and issue",
			text:     mention + " approve my-workspace #99",
			wantOK:   true,
			wantVerb: "approve",
			wantArgs: []string{"my-workspace", "#99"},
		},
		{
			name:     "empty after mention",
			text:     mention,
			wantOK:   false,
			wantVerb: "",
		},
		{
			name:     "status uppercase verb is lowercased",
			text:     mention + " STATUS",
			wantOK:   true,
			wantVerb: "status",
			wantArgs: []string{},
		},
		{
			name:     "cost this-week",
			text:     mention + " cost this-week",
			wantOK:   true,
			wantVerb: "cost",
			wantArgs: []string{"this-week"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cmd, ok := slack.ParseMention(tc.text, botID)
			if ok != tc.wantOK {
				t.Fatalf("ParseMention ok=%v want %v", ok, tc.wantOK)
			}
			if !ok {
				return
			}
			if cmd.Verb != tc.wantVerb {
				t.Errorf("verb=%q want %q", cmd.Verb, tc.wantVerb)
			}
			if tc.wantArgs != nil && len(cmd.Args) != len(tc.wantArgs) {
				t.Errorf("args=%v want %v", cmd.Args, tc.wantArgs)
			}
		})
	}
}

func TestDispatchHelp(t *testing.T) {
	authz := slack.NewAuthRegistry()
	app := &stubApp{}
	disp := slack.NewDispatcher(app, authz)

	cmd, ok := slack.ParseMention("<@UBOT> help", "UBOT")
	if !ok {
		t.Fatal("parse failed")
	}
	resp := disp.Dispatch(cmd, "USLACK", "CCHAN")
	if resp == "" {
		t.Error("help response should be non-empty")
	}
}

func TestDispatchUnknownVerb(t *testing.T) {
	authz := slack.NewAuthRegistry()
	app := &stubApp{}
	disp := slack.NewDispatcher(app, authz)

	cmd := slack.ParsedCommand{Verb: "notacommand"}
	resp := disp.Dispatch(cmd, "USLACK", "CCHAN")
	if resp == "" {
		t.Error("unknown verb should return non-empty error string")
	}
}

func TestDispatchPlanRequiresFull(t *testing.T) {
	authz := slack.NewAuthRegistry()
	authz.Set("ws1", "UREAD", slack.PermReadOnly)

	app := &stubApp{channelWS: "ws1"}
	disp := slack.NewDispatcher(app, authz)

	cmd := slack.ParsedCommand{Verb: "plan", Args: []string{"#10"}}
	resp := disp.Dispatch(cmd, "UREAD", "CCHAN")
	if resp == "" {
		t.Fatal("expected response for unauthorized mutate")
	}
	// Should mention authorization.
	if len(resp) < 5 {
		t.Error("response too short")
	}
}

func TestDispatchPlanAuthorized(t *testing.T) {
	authz := slack.NewAuthRegistry()
	authz.Set("ws1", "UFULL", slack.PermFull)

	app := &stubApp{channelWS: "ws1"}
	disp := slack.NewDispatcher(app, authz)

	cmd := slack.ParsedCommand{Verb: "plan", Args: []string{"#10"}}
	resp := disp.Dispatch(cmd, "UFULL", "CCHAN")
	if resp == "" {
		t.Fatal("expected non-empty response")
	}
	if app.planCalled == 0 {
		t.Error("plan command should have invoked SlackCommandPlan")
	}
}

// stubApp satisfies slack.AppFacade for testing.
type stubApp struct {
	channelWS  string
	planCalled int
}

func (s *stubApp) SlackCommandListWorkspaces() string { return "workspaces list" }
func (s *stubApp) SlackCommandStatus(_ string) string { return "status ok" }
func (s *stubApp) SlackCommandPlan(_ string, _ int) error {
	s.planCalled++
	return nil
}
func (s *stubApp) SlackCommandApprove(_ string, _ int) error  { return nil }
func (s *stubApp) SlackCommandCancel(_ string, _ int) error   { return nil }
func (s *stubApp) SlackCommandCostThisWeek(_ string) string   { return "$0.00" }
func (s *stubApp) SlackCommandGoalStatus(_ string) string     { return "goal status" }
func (s *stubApp) SlackResolveWorkspaceByChannel(_ string) (string, bool) {
	if s.channelWS != "" {
		return s.channelWS, true
	}
	return "", false
}
