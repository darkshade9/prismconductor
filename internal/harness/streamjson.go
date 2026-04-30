package harness

import (
	"encoding/json"
	"io"
)

// emitter writes line-delimited stream-json events that the session package's
// StreamParser already understands (PRISMCONDUCTOR_PLAN.md §10.3 / §10.5).
// Each method serializes one event matching what `claude -p --output-format
// stream-json` emits, so the parser can't tell harness output apart from
// Claude CLI output — which means matchPatterns and the role-prefixed transcript
// behave identically for both spawn strategies.
type emitter struct {
	out io.Writer
}

func newEmitter(out io.Writer) *emitter { return &emitter{out: out} }

func (e *emitter) write(v map[string]any) {
	if e == nil || e.out == nil {
		return
	}
	b, err := json.Marshal(v)
	if err != nil {
		return
	}
	b = append(b, '\n')
	_, _ = e.out.Write(b)
}

// system emits a system.init event. Mirrors the StreamParser arm that
// produces "@sys session initialized — model thinking…".
func (e *emitter) systemInit() {
	e.write(map[string]any{
		"type":    "system",
		"subtype": "init",
	})
}

// systemStatus emits a system.status event so a free-text status line shows
// up in the transcript with the @sys role prefix.
func (e *emitter) systemStatus(msg string) {
	e.write(map[string]any{
		"type":    "system",
		"subtype": "status",
		"status":  msg,
	})
}

// assistantMessage emits the full assistant turn so the session
// StreamParser produces matching @asst / @tool lines. The parser's text
// handling lives in the `stream_event` arm (it expects content_block_delta
// events with text_delta), so we emit the text as a single content_block_delta
// then close with content_block_stop. Tool_use blocks go through the
// `assistant` arm — that's what the parser uses to emit @tool lines.
//
// This is the propagation channel for the §10.3 sentinels (Plan written /
// BLOCKED: / QUESTION_PENDING: / Work complete. / PR_OPENED:).
func (e *emitter) assistantMessage(text string, calls []toolCallEvent) {
	if text != "" {
		// Ensure the text ends with a newline so the parser's @asst flush
		// fires; otherwise the line stays in the @live tail and matchPatterns
		// never sees it.
		body := text
		if body[len(body)-1] != '\n' {
			body += "\n"
		}
		e.write(map[string]any{
			"type": "stream_event",
			"event": map[string]any{
				"type": "content_block_delta",
				"delta": map[string]any{
					"type": "text_delta",
					"text": body,
				},
			},
		})
		e.write(map[string]any{
			"type":  "stream_event",
			"event": map[string]any{"type": "content_block_stop"},
		})
	}
	if len(calls) == 0 {
		return
	}
	content := []map[string]any{}
	for _, c := range calls {
		input := json.RawMessage(c.Args)
		if len(input) == 0 {
			input = json.RawMessage("{}")
		}
		content = append(content, map[string]any{
			"type":  "tool_use",
			"id":    c.ID,
			"name":  c.Name,
			"input": input,
		})
	}
	e.write(map[string]any{
		"type": "assistant",
		"message": map[string]any{
			"content": content,
		},
	})
}

// toolResult emits a user.tool_result event so the parser produces an "@res"
// (or "@err" if isError) line for the model's tool call.
func (e *emitter) toolResult(callID, content string, isError bool) {
	e.write(map[string]any{
		"type": "user",
		"message": map[string]any{
			"content": []map[string]any{
				{
					"type":         "tool_result",
					"tool_use_id":  callID,
					"content":      content,
					"is_error":     isError,
				},
			},
		},
	})
}

// done emits the terminal `result` event. The parser's "result" arm appends a
// "@done completed" line.
func (e *emitter) done() {
	e.write(map[string]any{
		"type":   "result",
		"result": "completed",
	})
}

// toolCallEvent is the harness-internal alias for an OpenAI-shape tool call,
// used when serializing assistant messages. Mirrors llm.ToolCall but kept
// local so streamjson.go has no llm import (avoids cycles).
type toolCallEvent struct {
	ID   string
	Name string
	Args json.RawMessage
}
