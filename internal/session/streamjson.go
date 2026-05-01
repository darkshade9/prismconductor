package session

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"
)

// Line role prefixes. The frontend recognizes these and renders with role-
// specific colors. Anything without a prefix is shown verbatim (e.g. raw
// stderr from a failed spawn).
const (
	RoleUser      = "@user "
	RoleAssistant = "@asst "
	RoleSystem    = "@sys  "
	RoleTool      = "@tool "
	RoleResult    = "@res  "
	RoleError     = "@err  "
	RoleLive      = "@live " // partial assistant text; frontend replaces the previous @live line in place
	RoleDone      = "@done "
)

// CostData holds token counts and dollar cost captured from a Claude
// stream-json "result" event (issue #47). Zero-value means no data yet.
type CostData struct {
	TotalCostUSD float64
	InputTokens  int64
	OutputTokens int64
}

// StreamParser is per-session state for converting the line-delimited
// stream-json events from `claude -p --output-format stream-json` into
// role-prefixed display lines.
type StreamParser struct {
	mu       sync.Mutex
	enabled  bool
	pending  strings.Builder // in-flight assistant text (between newlines)
	cost     *CostData       // populated when result event arrives (issue #47)
}

// NewStreamParser returns a parser that's enabled by default. If the spawn
// fell back to non-streaming mode, the parser auto-disables on the first
// non-JSON line.
func NewStreamParser() *StreamParser { return &StreamParser{enabled: true} }

// LastCost returns the cost data captured from the most recent "result" event,
// or nil if no result event has been observed yet (issue #47).
func (p *StreamParser) LastCost() *CostData {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.cost
}

// Feed processes one raw line from the PTY and returns 0+ display lines.
func (p *StreamParser) Feed(raw string) []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.enabled {
		return []string{raw}
	}
	if !strings.HasPrefix(raw, "{") {
		// Pre-stream banners or fallback bytes. Once we've seen a non-JSON
		// line *before* any JSON line, leave streaming on and just pass
		// through; we don't disable globally because the CLI sometimes
		// interleaves a status line.
		return []string{raw}
	}
	var generic struct {
		Type    string          `json:"type"`
		Subtype string          `json:"subtype"`
		Status  string          `json:"status"`
		Result  string          `json:"result"`
		Event   json.RawMessage `json:"event"`
	}
	if err := json.Unmarshal([]byte(raw), &generic); err != nil {
		return []string{raw}
	}

	var out []string
	flushPending := func() {
		if p.pending.Len() > 0 {
			out = append(out, RoleAssistant+p.pending.String())
			p.pending.Reset()
		}
	}

	switch generic.Type {
	case "system":
		flushPending()
		switch generic.Subtype {
		case "init":
			out = append(out, RoleSystem+"session initialized — model thinking…")
		case "status":
			if generic.Status != "" {
				out = append(out, RoleSystem+generic.Status)
			}
		}
		// Drop hook chatter.

	case "stream_event":
		var ev struct {
			Type         string `json:"type"`
			ContentBlock struct {
				Type string `json:"type"`
				Name string `json:"name"`
			} `json:"content_block"`
			Delta struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"delta"`
		}
		if err := json.Unmarshal(generic.Event, &ev); err != nil {
			return nil
		}
		switch ev.Type {
		case "content_block_start":
			if ev.ContentBlock.Type == "tool_use" && ev.ContentBlock.Name != "" {
				flushPending()
				out = append(out, RoleTool+ev.ContentBlock.Name)
			}
		case "content_block_delta":
			if ev.Delta.Type == "text_delta" && ev.Delta.Text != "" {
				p.pending.WriteString(ev.Delta.Text)
				// Emit complete lines as they form; keep a partial @live tail.
				cur := p.pending.String()
				for {
					nl := strings.IndexAny(cur, "\n\r")
					if nl < 0 {
						break
					}
					line := cur[:nl]
					if line != "" {
						out = append(out, RoleAssistant+line)
					}
					cur = cur[nl+1:]
				}
				p.pending.Reset()
				p.pending.WriteString(cur)
				if cur != "" {
					out = append(out, RoleLive+cur)
				}
			}
		case "content_block_stop":
			flushPending()
		case "message_stop":
			flushPending()
		}

	case "assistant":
		// The full message arrives after content_block_delta has already
		// streamed the text. Use it to surface tool_use blocks (which we may
		// have missed if the CLI elided their content_block_start).
		var m struct {
			Message struct {
				Content []struct {
					Type  string          `json:"type"`
					Name  string          `json:"name"`
					Input json.RawMessage `json:"input"`
				} `json:"content"`
			} `json:"message"`
		}
		if err := json.Unmarshal([]byte(raw), &m); err != nil {
			return nil
		}
		for _, c := range m.Message.Content {
			if c.Type == "tool_use" {
				flushPending()
				out = append(out, fmt.Sprintf("%s%s %s", RoleTool, c.Name, snippetBytes(c.Input, 140)))
			}
		}

	case "user":
		var m struct {
			Message struct {
				Content []struct {
					Type    string          `json:"type"`
					Content json.RawMessage `json:"content"`
					IsError bool            `json:"is_error"`
				} `json:"content"`
			} `json:"message"`
		}
		if err := json.Unmarshal([]byte(raw), &m); err != nil {
			return nil
		}
		for _, c := range m.Message.Content {
			if c.Type == "tool_result" {
				flushPending()
				role := RoleResult
				if c.IsError {
					role = RoleError
				}
				out = append(out, role+snippetBytes(c.Content, 220))
			}
		}

	case "result":
		flushPending()
		// Extract token counts and cost from the result event (issue #47).
		// These fields are only present on Claude stream-json output.
		var res struct {
			TotalCostUSD float64 `json:"total_cost_usd"`
			Usage        struct {
				InputTokens  int64 `json:"input_tokens"`
				OutputTokens int64 `json:"output_tokens"`
			} `json:"usage"`
		}
		if err := json.Unmarshal([]byte(raw), &res); err == nil {
			if res.TotalCostUSD > 0 || res.Usage.InputTokens > 0 {
				p.cost = &CostData{
					TotalCostUSD: res.TotalCostUSD,
					InputTokens:  res.Usage.InputTokens,
					OutputTokens: res.Usage.OutputTokens,
				}
			}
		}
		if generic.Result != "" {
			// Result text is already streamed as @asst lines; add a small
			// completion marker.
			out = append(out, RoleDone+"completed")
		}
	}
	return out
}

func snippetBytes(b []byte, n int) string {
	s := string(b)
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
