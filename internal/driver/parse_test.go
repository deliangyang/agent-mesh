package driver

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
	"unicode/utf8"

	"agent-mesh/internal/protocol"
)

func TestParseJSONLToolUse(t *testing.T) {
	obj := map[string]any{
		"type": "assistant",
		"message": map[string]any{
			"content": []any{
				map[string]any{"type": "tool_use", "name": "Bash", "input": map[string]any{"command": "ls"}},
				map[string]any{"type": "text", "text": "done"},
			},
		},
	}
	evs := ParseJSONL(obj)
	if len(evs) != 2 {
		t.Fatalf("len %d %#v", len(evs), evs)
	}
	if evs[0].Type != "tool_call" || evs[0].Tool != "Bash" {
		t.Fatalf("tool %#v", evs[0])
	}
	if evs[1].Type != "assistant" || evs[1].Text != "done" {
		t.Fatalf("text %#v", evs[1])
	}
}

func TestParseJSONLThinking(t *testing.T) {
	obj := map[string]any{
		"type": "assistant",
		"message": map[string]any{
			"content": []any{
				map[string]any{"type": "thinking", "thinking": "look at the directory first"},
				map[string]any{"type": "tool_use", "name": "Bash", "input": map[string]any{"command": "ls"}},
				map[string]any{"type": "text", "text": "done"},
			},
		},
	}
	evs := ParseJSONL(obj)
	if len(evs) != 3 {
		t.Fatalf("len %d %#v", len(evs), evs)
	}
	if evs[0].Type != "thinking" || evs[0].Text != "look at the directory first" {
		t.Fatalf("thinking %#v", evs[0])
	}
	if evs[1].Type != "tool_call" || evs[2].Type != "assistant" {
		t.Fatalf("rest %#v", evs)
	}
}

func TestParseJSONLRedactedPlaceholder(t *testing.T) {
	evs := ParseJSONL(map[string]any{
		"type": "assistant",
		"message": map[string]any{
			"content": "[REDACTED]",
		},
	})
	if len(evs) != 0 {
		t.Fatalf("literal redacted string %#v", evs)
	}
	evs = ParseJSONL(map[string]any{
		"type": "assistant",
		"message": map[string]any{
			"content": []any{
				map[string]any{"type": "redacted_thinking", "text": "[REDACTED]"},
				map[string]any{"type": "text", "text": "done"},
			},
		},
	})
	if len(evs) != 1 || evs[0].Type != "assistant" || evs[0].Text != "done" {
		t.Fatalf("mixed content %#v", evs)
	}
	evs = ParseJSONL(map[string]any{"type": "thinking", "subtype": "delta", "text": "[REDACTED]"})
	if len(evs) != 0 {
		t.Fatalf("thinking placeholder %#v", evs)
	}
}

func TestParseJSONLWorkBuddy(t *testing.T) {
	think := ParseJSONL(map[string]any{
		"type":    "reasoning",
		"content": []any{},
		"rawContent": []any{
			map[string]any{"type": "reasoning_text", "text": "look at the directory first"},
		},
	})
	if len(think) != 1 || think[0].Type != "thinking" || think[0].Text != "look at the directory first" {
		t.Fatalf("reasoning %#v", think)
	}

	call := ParseJSONL(map[string]any{
		"type":      "function_call",
		"name":      "Write",
		"arguments": `{"file_path":"/tmp/a.html","content":"<!DOCTYPE html> huge"}`,
		"providerData": map[string]any{
			"argumentsDisplayText": "a.html",
		},
	})
	if len(call) != 1 || call[0].Type != "tool_call" || call[0].Tool != "Write" {
		t.Fatalf("function_call %#v", call)
	}
	if !strings.Contains(call[0].Preview, "a.html") {
		t.Fatalf("preview %#v", call[0])
	}
	if strings.Contains(call[0].Text, "<!DOCTYPE") {
		t.Fatalf("should compact write content %#v", call[0])
	}

	res := ParseJSONL(map[string]any{
		"type":   "function_call_result",
		"name":   "Write",
		"status": "completed",
		"output": map[string]any{"type": "text", "text": "wrote 12 lines"},
	})
	if len(res) != 1 || res[0].Type != "tool_result" || res[0].Tool != "Write" || res[0].Text != "wrote 12 lines" {
		t.Fatalf("function_call_result %#v", res)
	}

	cases := []struct {
		name string
		obj  map[string]any
		want string
	}{
		{
			name: "workbuddy-top-only",
			obj: map[string]any{
				"type": "message",
				"role": "assistant",
				"content": []any{
					map[string]any{"type": "output_text", "text": "done"},
				},
			},
			want: "done",
		},
		{
			name: "workbuddy-usage-sidecar",
			obj: map[string]any{
				"type": "message",
				"role": "assistant",
				"content": []any{
					map[string]any{"type": "output_text", "text": "目录是空的"},
				},
				"message": map[string]any{
					"usage": map[string]any{"input_tokens": 1, "output_tokens": 2},
				},
			},
			want: "目录是空的",
		},
		{
			name: "workbuddy-empty-nested-content",
			obj: map[string]any{
				"type": "message",
				"role": "assistant",
				"content": []any{
					map[string]any{"type": "output_text", "text": "from-top"},
				},
				"message": map[string]any{
					"content": []any{},
					"usage":   map[string]any{"input_tokens": 1},
				},
			},
			want: "from-top",
		},
		{
			name: "claude-nested-content",
			obj: map[string]any{
				"type": "assistant",
				"message": map[string]any{
					"role": "assistant",
					"content": []any{
						map[string]any{"type": "text", "text": "nested-ok"},
					},
				},
			},
			want: "nested-ok",
		},
		{
			name: "role-inside-message",
			obj: map[string]any{
				"type": "message",
				"content": []any{
					map[string]any{"type": "output_text", "text": "role-nested"},
				},
				"message": map[string]any{"role": "assistant"},
			},
			want: "role-nested",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ParseJSONL(tc.obj)
			if len(got) != 1 || got[0].Type != "assistant" || got[0].Text != tc.want {
				t.Fatalf("got %#v want assistant %q", got, tc.want)
			}
		})
	}
}

func TestParseJSONLUserToolResult(t *testing.T) {
	obj := map[string]any{
		"type": "user",
		"message": map[string]any{
			"content": []any{
				map[string]any{"type": "tool_result", "text": "ok output"},
			},
		},
	}
	evs := ParseJSONL(obj)
	if len(evs) != 1 || evs[0].Type != "tool_result" {
		t.Fatalf("%#v", evs)
	}
}

func TestFormatJSONLStillWorks(t *testing.T) {
	obj := map[string]any{"role": "user", "content": "hello world"}
	if got := FormatJSONL(obj); got != "[user] hello world" {
		t.Fatalf("got %q", got)
	}
}

func TestParseJSONLTitleAndSession(t *testing.T) {
	title := ParseJSONL(map[string]any{"type": "ai-title", "aiTitle": "fix the compile script"})
	if len(title) != 1 || title[0].Type != "title" || title[0].Text != "fix the compile script" {
		t.Fatalf("ai-title %#v", title)
	}
	custom := ParseJSONL(map[string]any{"type": "custom-title", "customTitle": "manual title"})
	if len(custom) != 1 || custom[0].Text != "manual title" {
		t.Fatalf("custom-title %#v", custom)
	}
	sess := ParseJSONL(map[string]any{"type": "system", "subtype": "init", "session_id": "abc-1"})
	if len(sess) != 1 || sess[0].Type != "session" || sess[0].Session != "abc-1" || sess[0].Text != "" {
		t.Fatalf("session %#v", sess)
	}
	evs := EventsFromJSONL(map[string]any{"type": "assistant", "session_id": "s9", "message": map[string]any{"content": "ok"}}, "cursor", "")
	if len(evs) != 1 || evs[0].Session != "s9" {
		t.Fatalf("bind session %#v", evs)
	}
}

func TestJSONLTitle(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/s.jsonl"
	raw := `{"type":"user","message":{"content":"hello world"}}
{"type":"ai-title","aiTitle":"greeting"}
`
	if err := os.WriteFile(path, []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := JSONLTitle(path); got != "greeting" {
		t.Fatalf("title %q", got)
	}
	s := WithJSONLTitle(protocol.Session{JSONL: path})
	if s.Title != "greeting" {
		t.Fatalf("fill %#v", s)
	}
}

func TestJSONLTitleCodexEventMsg(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/codex.jsonl"
	raw := `{"type":"session_meta","payload":{"id":"abc"}}
{"type":"event_msg","payload":{"type":"user_message","message":"fix login timeout"}}
`
	if err := os.WriteFile(path, []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := JSONLTitle(path); got != "fix login timeout" {
		t.Fatalf("title %q", got)
	}
}

func TestParseJSONLCodexRollout(t *testing.T) {
	sid := ParseJSONL(map[string]any{
		"type": "session_meta",
		"payload": map[string]any{
			"id":  "01a17b9e-55a2-7953-af98-b9a36cd6bf93",
			"cwd": "/tmp/work",
		},
	})
	if len(sid) != 1 || sid[0].Type != "session" || sid[0].Session != "01a17b9e-55a2-7953-af98-b9a36cd6bf93" {
		t.Fatalf("session_meta %#v", sid)
	}

	asst := ParseJSONL(map[string]any{
		"type": "event_msg",
		"payload": map[string]any{
			"type":    "agent_message",
			"message": "got two solutions",
		},
	})
	if len(asst) != 1 || asst[0].Type != "assistant" || asst[0].Text != "got two solutions" {
		t.Fatalf("agent_message %#v", asst)
	}

	think := ParseJSONL(map[string]any{
		"type": "event_msg",
		"payload": map[string]any{
			"type": "agent_reasoning",
			"text": "look at the equation first",
		},
	})
	if len(think) != 1 || think[0].Type != "thinking" || think[0].Text != "look at the equation first" {
		t.Fatalf("agent_reasoning %#v", think)
	}

	injected := ParseJSONL(map[string]any{
		"type": "response_item",
		"payload": map[string]any{
			"type": "message",
			"role": "user",
			"content": []any{
				map[string]any{"type": "input_text", "text": "<environment_context>\n  <cwd>/tmp</cwd>\n</environment_context>"},
			},
		},
	})
	if len(injected) != 0 {
		t.Fatalf("injected %#v", injected)
	}

	user := ParseJSONL(map[string]any{
		"type": "response_item",
		"payload": map[string]any{
			"type": "message",
			"role": "user",
			"content": []any{
				map[string]any{"type": "input_text", "text": "compute x + y = z"},
			},
		},
	})
	if len(user) != 1 || user[0].Type != "user" || user[0].Text != "compute x + y = z" {
		t.Fatalf("response user %#v", user)
	}

	reply := ParseJSONL(map[string]any{
		"type": "response_item",
		"payload": map[string]any{
			"type": "message",
			"role": "assistant",
			"content": []any{
				map[string]any{"type": "output_text", "text": "feat: add zed extension"},
			},
		},
	})
	if len(reply) != 1 || reply[0].Type != "assistant" || reply[0].Text != "feat: add zed extension" {
		t.Fatalf("response assistant %#v", reply)
	}

	call := ParseJSONL(map[string]any{
		"type": "response_item",
		"payload": map[string]any{
			"type":      "function_call",
			"name":      "shell_command",
			"arguments": `{"command":"ls","workdir":"/tmp"}`,
			"call_id":   "call_1",
		},
	})
	if len(call) != 1 || call[0].Type != "tool_call" || call[0].Tool != "shell_command" {
		t.Fatalf("function_call %#v", call)
	}
	if !strings.Contains(call[0].Preview, "ls") {
		t.Fatalf("preview %#v", call[0])
	}

	out := ParseJSONL(map[string]any{
		"type": "response_item",
		"payload": map[string]any{
			"type":    "function_call_output",
			"call_id": "call_1",
			"output":  "ok\n",
		},
	})
	if len(out) != 1 || out[0].Type != "tool_result" || out[0].Text != "ok" {
		t.Fatalf("function_call_output %#v", out)
	}

	dev := ParseJSONL(map[string]any{
		"type": "response_item",
		"payload": map[string]any{
			"type": "message",
			"role": "developer",
			"content": []any{
				map[string]any{"type": "input_text", "text": "You are Codex"},
			},
		},
	})
	if len(dev) != 0 {
		t.Fatalf("developer %#v", dev)
	}
}

func TestJSONLTitleReadsHeadNotJustTail(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/big.jsonl"
	var b strings.Builder
	b.WriteString(`{"type":"user","message":{"content":"first user question"}}` + "\n")
	b.WriteString(`{"type":"ai-title","aiTitle":"head title"}` + "\n")
	pad := `{"type":"assistant","message":{"content":"` + strings.Repeat("x", 200) + `"}}` + "\n"
	for b.Len() < 80*1024 {
		b.WriteString(pad)
	}
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := JSONLTitle(path); got != "head title" {
		t.Fatalf("title %q", got)
	}
}

func TestExtractUserTextDropsTimestamp(t *testing.T) {
	raw := `<timestamp>Saturday, Oct 31, 2026, 11:07 PM (UTC+8)</timestamp>
<user_query>
pick an engine, then press /
</user_query>`
	if got := ExtractUserText(raw); got != "pick an engine, then press /" {
		t.Fatalf("query %q", got)
	}
	only := `<timestamp>Saturday, Oct 31, 2026, 11:07 PM (UTC+8)</timestamp>`
	if got := ExtractUserText(only); got != "" {
		t.Fatalf("timestamp-only %q", got)
	}
	notify := `<system_notification>
The following task has finished.
</system_notification>`
	if got := ExtractUserText(notify); got != "" {
		t.Fatalf("notify %q", got)
	}
	plain := ExtractUserText("a plain sentence")
	if plain != "a plain sentence" {
		t.Fatalf("plain %q", plain)
	}
	if got := ExtractUserText("<user_query>当前session</user_query>"); got != "" {
		t.Fatalf("session label %q", got)
	}
	if got := ExtractUserText("<user_query>abc-1</user_query>"); got != "abc-1" {
		t.Fatalf("short id should keep %q", got)
	}
	if got := ExtractUserText("<user_query>3e22a1a4-8293-4ddc-838f-d9ac4e27d50a</user_query>"); got != "" {
		t.Fatalf("uuid session %q", got)
	}
	if got := ExtractUserText("<user_query>Briefly inform the user about the task result and perform any follow-up actions (if needed). If there's no follow-ups needed, don't explicitly say that.\n\nis it fixed</user_query>"); got != "is it fixed" {
		t.Fatalf("task prefix %q", got)
	}
	if got := ExtractUserText("<user_query>当前session  what is this?</user_query>"); got != "当前session  what is this?" {
		t.Fatalf("real question %q", got)
	}
	if got := ExtractUserText("look at this image\n\n<<MESH_ATTACH>>\nThe user uploaded these attachments; inspect them with the Read tool:\n- /tmp/proj/.agent-mesh/inbox/a.png (image/png)\n<</MESH_ATTACH>>"); got != "look at this image" {
		t.Fatalf("attach block %q", got)
	}
	attachEvs := UserEvent("look at this image\n\n<<MESH_ATTACH>>\nThe user uploaded these attachments; inspect them with the Read tool:\n- /tmp/proj/.agent-mesh/inbox/a.png (image/png)\n<</MESH_ATTACH>>")
	if len(attachEvs) != 1 || attachEvs[0].Text != "look at this image" || len(attachEvs[0].Files) != 1 || attachEvs[0].Files[0].Name != "a.png" {
		t.Fatalf("UserEvent %#v", attachEvs)
	}
	models := `<available_subagent_models>
inherit
claude-opus-5-5-medium
</available_subagent_models>`
	if got := ExtractUserText(models); got != "" {
		t.Fatalf("subagent models %q", got)
	}
	mixed := `<available_subagent_models>
inherit
</available_subagent_models>
<dynamic_tool_namespaces></dynamic_tools>
all became my input`
	if got := ExtractUserText(mixed); got != "all became my input" {
		t.Fatalf("mixed envelopes %q", got)
	}
	evs := ParseJSONL(map[string]any{"type": "user", "message": map[string]any{"content": only}})
	if len(evs) != 0 {
		t.Fatalf("should drop timestamp user event %#v", evs)
	}
}

func TestFilterSinceKeepsUntimed(t *testing.T) {
	now := int64(1_800_000_000)
	evs := []protocol.Event{
		{Type: "user", Text: "old", Unix: now - 3*86400},
		{Type: "user", Text: "fresh", Unix: now - 3600},
		{Type: "assistant", Text: "no-time"},
	}
	got := FilterSince(evs, now-2*86400)
	if len(got) != 2 || got[0].Text != "fresh" || got[1].Text != "no-time" {
		t.Fatalf("%#v", got)
	}
}

func TestLastTurnsKeepsRecentUserRounds(t *testing.T) {
	evs := []protocol.Event{
		{Type: "user", Text: "1"},
		{Type: "assistant", Text: "a1"},
		{Type: "user", Text: "2"},
		{Type: "assistant", Text: "a2"},
		{Type: "user", Text: "3"},
		{Type: "tool_call", Tool: "read", Text: "x"},
		{Type: "assistant", Text: "a3"},
	}
	got := LastTurns(evs, 2)
	if len(got) != 5 || got[0].Text != "2" || got[len(got)-1].Text != "a3" {
		t.Fatalf("%#v", got)
	}
	if keep := LastTurns(evs, 20); len(keep) != len(evs) {
		t.Fatalf("keep all %#v", keep)
	}
}

func TestParseAskUserQuestion(t *testing.T) {
	obj := map[string]any{
		"type": "assistant",
		"message": map[string]any{
			"content": []any{
				map[string]any{
					"type": "tool_use",
					"id":   "q1",
					"name": "AskUserQuestion",
					"input": map[string]any{
						"questions": []any{
							map[string]any{
								"question": "Which style?",
								"header":   "style",
								"options": []any{
									map[string]any{"label": "rewrite", "description": "Start over"},
									map[string]any{"label": "patch", "description": "Only fix the broken bits"},
								},
							},
						},
					},
				},
			},
		},
	}
	evs := ParseJSONL(obj)
	var ask protocol.Event
	for _, ev := range evs {
		if ev.Type == "ask" {
			ask = ev
		}
	}
	if ask.Type != "ask" || ask.Tool != "AskUserQuestion" {
		t.Fatalf("ask %#v from %#v", ask, evs)
	}
	if !strings.Contains(ask.Text, "rewrite") || !strings.Contains(ask.Preview, "style") {
		t.Fatalf("payload %#v", ask)
	}
}

func TestParsePermissionControl(t *testing.T) {
	evs := ParseJSONL(map[string]any{
		"type":       "control_request",
		"request_id": "r9",
		"request": map[string]any{
			"subtype":   "can_use_tool",
			"tool_name": "Bash",
			"input":     map[string]any{"command": "rm tmp"},
		},
	})
	if len(evs) != 1 || evs[0].Type != "ask" {
		t.Fatalf("%#v", evs)
	}
	if !strings.Contains(evs[0].Text, "Allow") || !strings.Contains(evs[0].Text, "Bash") {
		t.Fatalf("perm %#v", evs[0])
	}
}

func TestParseOpenCodeEvents(t *testing.T) {
	evs := ParseJSONL(map[string]any{
		"type":      "text",
		"sessionID": "ses_abc",
		"part":      map[string]any{"type": "text", "text": "hello"},
	})
	if len(evs) != 1 || evs[0].Type != "assistant" || evs[0].Text != "hello" {
		t.Fatalf("text %#v", evs)
	}
	evs = ParseJSONL(map[string]any{
		"type": "tool_use",
		"part": map[string]any{
			"tool": "bash",
			"state": map[string]any{
				"input":  map[string]any{"command": "ls"},
				"output": "ok\n",
			},
		},
	})
	if len(evs) != 2 || evs[0].Type != "tool_call" || evs[1].Type != "tool_result" {
		t.Fatalf("tool %#v", evs)
	}
}

func TestParseCursorStreamJSON(t *testing.T) {
	think := ParseJSONL(map[string]any{
		"type":       "thinking",
		"subtype":    "completed",
		"session_id": "s1",
	})
	if len(think) != 0 {
		t.Fatalf("empty thinking %#v", think)
	}
	delta := ParseJSONL(map[string]any{
		"type":    "thinking",
		"subtype": "delta",
		"text":    "look at the directory first",
	})
	if len(delta) != 1 || delta[0].Type != "thinking" || delta[0].Text != "look at the directory first" {
		t.Fatalf("thinking delta %#v", delta)
	}

	started := ParseJSONL(map[string]any{
		"type":    "tool_call",
		"subtype": "started",
		"call_id": "c-1",
		"tool_call": map[string]any{
			"shellToolCall": map[string]any{
				"args": map[string]any{"command": "ls"},
			},
		},
	})
	if len(started) != 1 || started[0].Type != "tool_call" || started[0].Tool != "Shell" {
		t.Fatalf("started %#v", started)
	}
	if !strings.Contains(started[0].Preview, "ls") {
		t.Fatalf("preview %#v", started[0])
	}

	done := ParseJSONL(map[string]any{
		"type":    "tool_call",
		"subtype": "completed",
		"call_id": "c-1",
		"tool_call": map[string]any{
			"shellToolCall": map[string]any{
				"args": map[string]any{"command": "ls"},
				"result": map[string]any{
					"success": map[string]any{"stdout": "bin\nclaude_ctl.py\ncmd\n"},
				},
			},
		},
	})
	if len(done) != 2 || done[0].Type != "tool_call" || done[1].Type != "tool_result" {
		t.Fatalf("completed %#v", done)
	}
	if done[1].Text != "bin\nclaude_ctl.py\ncmd" {
		t.Fatalf("ls text %q", done[1].Text)
	}

	listed := ParseJSONL(map[string]any{
		"type":    "tool_call",
		"subtype": "completed",
		"tool_call": map[string]any{
			"lsToolCall": map[string]any{
				"args": map[string]any{"path": "."},
				"result": map[string]any{
					"success": map[string]any{
						"files": []any{"bin", "claude_ctl.py", "cmd"},
					},
				},
			},
		},
	})
	if len(listed) != 2 || listed[1].Text != "bin\nclaude_ctl.py\ncmd" {
		t.Fatalf("ls files %#v", listed)
	}
}

func TestContentToTextJoinsNames(t *testing.T) {
	got := ContentToText([]any{"bin", "claude_ctl.py", "cmd"})
	if got != "bin\nclaude_ctl.py\ncmd" {
		t.Fatalf("names %q", got)
	}
	got = ContentToText([]any{
		map[string]any{"type": "text", "text": "hello"},
		map[string]any{"type": "text", "text": "world"},
	})
	if got != "helloworld" {
		t.Fatalf("blocks %q", got)
	}
}

func TestParsePiEvents(t *testing.T) {
	evs := ParseJSONL(map[string]any{
		"type": "session", "version": 3, "id": "sid-1", "cwd": "/tmp",
	})
	if len(evs) != 1 || evs[0].Type != "session" || evs[0].Session != "sid-1" {
		t.Fatalf("session %#v", evs)
	}
	evs = ParseJSONL(map[string]any{
		"type": "message_update",
		"assistantMessageEvent": map[string]any{
			"type":  "text_delta",
			"delta": "Hi",
		},
	})
	if len(evs) != 1 || evs[0].Type != "delta" || evs[0].Text != "Hi" {
		t.Fatalf("delta %#v", evs)
	}
	evs = ParseJSONL(map[string]any{"type": "agent_settled", "aborted": false})
	if len(evs) != 1 || !evs[0].Done {
		t.Fatalf("settled %#v", evs)
	}
}

func TestMatchSession(t *testing.T) {
	rows := []protocol.Session{
		{ID: "54b217af-e7ad-40f6-b27c-9a0d06097078", Engine: "cursor"},
		{ID: "54b217af-e7ad-40f6-b27c-9a0d06097079", Engine: "cursor"},
	}
	got, err := MatchSession(rows, "54b217af-e7ad-40f6-b27c-9a0d06097078")
	if err != nil || got.ID != rows[0].ID {
		t.Fatalf("exact %#v %v", got, err)
	}
	if _, err := MatchSession(rows, "54b217af"); err == nil {
		t.Fatal("prefix collision should fail")
	}
	got, err = MatchSession(rows, "54b217af-e7ad-40f6-b27c-9a0d06097079")
	if err != nil || got.ID != rows[1].ID {
		t.Fatalf("other %#v %v", got, err)
	}
	if _, err := MatchSession(rows, "missing"); err == nil {
		t.Fatal("missing should fail")
	}
}

func TestFormatAskAnswer(t *testing.T) {
	p := AskPrompt{
		Kind: "question",
		Questions: []AskQuestion{{
			ID:     "style",
			Prompt: "Which style?",
			Header: "style",
			Options: []AskOption{
				{ID: "rewrite", Label: "rewrite"},
			},
		}},
	}
	got := FormatAskAnswer(p, map[string][]string{"style": {"rewrite"}})
	if !strings.Contains(got, "style → rewrite") || !strings.Contains(got, `"answers"`) {
		t.Fatalf("%q", got)
	}
}

func TestTruncateDoesNotSplitChinese(t *testing.T) {
	title := "markdown 乱码了，没有使用markdown 显示？"
	got := Truncate(title, 48)
	if got != title {
		t.Fatalf("short title %q", got)
	}
	if !utf8.ValidString(got) || strings.ContainsRune(got, '\uFFFD') {
		t.Fatalf("invalid %q", got)
	}

	long := strings.Repeat("显示", 40)
	got = Truncate(long, 48)
	if !utf8.ValidString(got) || strings.ContainsRune(got, '\uFFFD') {
		t.Fatalf("long %q", got)
	}
	if !strings.HasSuffix(got, "…") {
		t.Fatalf("suffix %q", got)
	}
	if utf8.RuneCountInString(got) != 48 {
		t.Fatalf("runes %d %q", utf8.RuneCountInString(got), got)
	}
}

func TestCutBytesKeepsChineseRune(t *testing.T) {
	got := cutBytes("显示", 4)
	if got != "显" {
		t.Fatalf("cut %q", got)
	}
	if !utf8.ValidString(got) {
		t.Fatalf("invalid %q", got)
	}
}

func TestParseJSONLAssistantKeepsMarkdownTags(t *testing.T) {
	text := "wrap it in `<foo_bar>` and do not truncate."
	evs := ParseJSONL(map[string]any{
		"role": "assistant",
		"message": map[string]any{
			"content": []any{
				map[string]any{"type": "text", "text": text},
			},
		},
	})
	if len(evs) != 1 || evs[0].Type != "assistant" || evs[0].Text != text {
		t.Fatalf("markdown %#v", evs)
	}
}

func TestJSONLTitleKeepsChineseQuery(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/s.jsonl"
	raw := `{"role":"user","message":{"content":[{"type":"text","text":"<user_query>\nmarkdown 乱码了，没有使用markdown 显示？\n</user_query>"}]}}` + "\n"
	if err := os.WriteFile(path, []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}
	got := JSONLTitle(path)
	if got != "markdown 乱码了，没有使用markdown 显示？" {
		t.Fatalf("title %q", got)
	}
	if !utf8.ValidString(got) || strings.ContainsRune(got, '\uFFFD') {
		t.Fatalf("invalid %q", got)
	}
}

func TestParseJSONLAssistantKeepsMarkdownTable(t *testing.T) {
	text := "DingTalk notices now use a **Markdown table**.\n\n| pickup code | courier |\n| :---: | --- |\n| **08115060** | China Post |"
	evs := ParseJSONL(map[string]any{
		"role": "assistant",
		"message": map[string]any{
			"content": []any{
				map[string]any{"type": "text", "text": text},
			},
		},
	})
	if len(evs) != 1 || evs[0].Text != text {
		t.Fatalf("table %#v", evs)
	}
}

func TestAskFromPlanTool(t *testing.T) {
	ev, ok := AskFromTool("ExitPlanMode", map[string]any{"plan": "1. read the code\n2. fix the bug"}, "p1")
	if !ok || ev.Type != "ask" {
		t.Fatalf("event %#v ok=%v", ev, ok)
	}
	var p AskPrompt
	if err := json.Unmarshal([]byte(ev.Text), &p); err != nil {
		t.Fatal(err)
	}
	if p.Kind != "plan" || len(p.Questions) != 1 {
		t.Fatalf("prompt %#v", p)
	}
	q := p.Questions[0]
	if !strings.Contains(q.Detail, "fix the bug") {
		t.Fatalf("detail %q", q.Detail)
	}
	if len(q.Options) < 2 || !q.Options[0].Recommended {
		t.Fatalf("options %#v", q.Options)
	}
}
