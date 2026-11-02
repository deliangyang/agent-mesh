package driver

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode/utf8"

	"agent-mesh/internal/protocol"
)

var userQueryRe = regexp.MustCompile(`(?s)<user_query>(.*?)</user_query>`)
var envelopeOpenRe = regexp.MustCompile(`(?i)<([a-z][a-z0-9_]*)\b[^>]*>`)
var leftoverEnvelopeTagRe = regexp.MustCompile(`(?i)</?(?:[a-z][a-z0-9]*_[a-z0-9_]*|timestamp|communication)\b[^>]*>`)

// PIDAlive reports whether a process is still running.
func PIDAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	return err == nil
}

// Truncate collapses whitespace and truncates on rune boundaries so CJK is not split.
func Truncate(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	if n <= 0 {
		return ""
	}
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	if n == 1 {
		return "…"
	}
	return string(runes[:n-1]) + "…"
}

const maxEventText = 512 * 1024

func cutBytes(s string, n int) string {
	if n <= 0 {
		return ""
	}
	if len(s) <= n {
		return s
	}
	s = s[:n]
	for len(s) > 0 && !utf8.ValidString(s) {
		s = s[:len(s)-1]
	}
	return s
}

func cutByteSlice(b []byte, n int) []byte {
	if n <= 0 {
		return []byte{}
	}
	if len(b) <= n {
		return b
	}
	b = b[:n]
	for len(b) > 0 && !utf8.Valid(b) {
		b = b[:len(b)-1]
	}
	return b
}

// FullText keeps newlines and truncates on full characters when too long.
func FullText(s string) string {
	s = strings.TrimRight(s, "\n")
	if len(s) <= maxEventText {
		return s
	}
	return cutBytes(s, maxEventText) + "\n…(truncated)"
}

// EventTime reads a timestamp from a jsonl object.
func EventTime(obj map[string]any) int64 {
	if obj == nil {
		return 0
	}
	for _, k := range []string{"timestamp", "createdAt", "created_at", "time", "ts"} {
		if n := Epoch(obj[k]); n > 0 {
			return n
		}
	}
	return 0
}

// ExtractUserText extracts user-visible text.
func ExtractUserText(text string) string {
	if text == "" {
		return ""
	}
	text = stripMeshAttach(text)
	var parts []string
	for _, m := range userQueryRe.FindAllStringSubmatch(text, -1) {
		if len(m) == 2 {
			s := strings.TrimSpace(m[1])
			if s != "" {
				parts = append(parts, s)
			}
		}
	}
	if len(parts) > 0 {
		return dropInjectedUserPrefix(strings.Join(parts, "\n"))
	}
	return dropInjectedUserPrefix(stripEnvelopeTags(text))
}

var cursorTaskPrefixRe = regexp.MustCompile(`(?s)^\s*Briefly inform the user about the task result[^\n]*(?:\n+|$)`)
var sessionIDTextRe = regexp.MustCompile(`(?i)^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

func dropInjectedUserPrefix(text string) string {
	t := strings.TrimSpace(cursorTaskPrefixRe.ReplaceAllString(text, ""))
	t = stripMeshAttach(t)
	if looksLikeSessionMeta(t) {
		return ""
	}
	return t
}

func stripMeshAttach(text string) string {
	const open, close = "<<MESH_ATTACH>>", "<</MESH_ATTACH>>"
	s := text
	for {
		i := strings.Index(s, open)
		if i < 0 {
			break
		}
		j := strings.Index(s[i:], close)
		if j < 0 {
			s = strings.TrimSpace(s[:i])
			break
		}
		s = strings.TrimSpace(s[:i] + s[i+j+len(close):])
	}
	return s
}

func looksLikeSessionMeta(text string) bool {
	s := strings.TrimSpace(text)
	if s == "" {
		return true
	}
	compact := strings.ToLower(strings.ReplaceAll(s, " ", ""))
	switch compact {
	case "当前session", "当前会话", "session", "currentsession":
		return true
	}
	return sessionIDTextRe.MatchString(s)
}

func isEnvelopeTag(name string) bool {
	n := strings.ToLower(strings.TrimSpace(name))
	if n == "" || n == "user_query" {
		return false
	}
	if n == "timestamp" || n == "communication" {
		return true
	}
	return strings.Contains(n, "_")
}

func stripEnvelopeTags(text string) string {
	t := text
	start := 0
	for i := 0; i < 64; i++ {
		loc := envelopeOpenRe.FindStringSubmatchIndex(t[start:])
		if loc == nil {
			break
		}
		abs0 := start + loc[0]
		abs1 := start + loc[1]
		name := strings.ToLower(t[start+loc[2] : start+loc[3]])
		if !isEnvelopeTag(name) {
			start = abs1
			continue
		}
		close := "</" + name + ">"
		rest := t[abs1:]
		idx := strings.Index(strings.ToLower(rest), close)
		if idx < 0 {
			t = t[:abs0] + t[abs1:]
			continue
		}
		t = t[:abs0] + "\n" + rest[idx+len(close):]
		start = abs0
	}
	t = leftoverEnvelopeTagRe.ReplaceAllString(t, "")
	return strings.TrimSpace(t)
}

// ContentToText turns content blocks into text.
func ContentToText(content any) string {
	if content == nil {
		return ""
	}
	switch v := content.(type) {
	case string:
		return v
	case []any:
		return joinContentParts(v)
	case map[string]any:
		if t, ok := v["text"].(string); ok {
			return t
		}
		if t, ok := v["result"].(string); ok {
			return t
		}
		if t, ok := v["thinking"].(string); ok {
			return t
		}
	}
	return ""
}

func joinContentParts(items []any) string {
	parts := make([]string, 0, len(items))
	allStr := true
	for _, item := range items {
		if _, ok := item.(string); !ok {
			allStr = false
		}
		s := ContentToText(item)
		if s != "" {
			parts = append(parts, s)
		}
	}
	if len(parts) == 0 {
		return ""
	}
	if allStr && len(parts) > 1 {
		atomic := true
		for _, p := range parts {
			if strings.ContainsAny(p, " \t\n\r") {
				atomic = false
				break
			}
		}
		if atomic {
			return strings.Join(parts, "\n")
		}
	}
	var b strings.Builder
	for _, p := range parts {
		b.WriteString(p)
	}
	return b.String()
}

func strField(obj map[string]any, keys ...string) string {
	for _, k := range keys {
		if s, ok := obj[k].(string); ok && s != "" {
			return s
		}
	}
	return ""
}

func previewArgs(v any) string {
	switch t := v.(type) {
	case string:
		if m := argsMap(t); m != nil {
			if s := previewArgs(m); s != "" && s != t {
				return s
			}
		}
		return t
	case map[string]any:
		for _, k := range []string{"command", "cmd", "path", "file_path", "filePath", "target_file", "glob_pattern", "globPattern", "query", "pattern", "glob", "url", "description", "target_directory"} {
			if s, ok := t[k].(string); ok && s != "" {
				return s
			}
		}
		raw, _ := json.Marshal(t)
		return string(raw)
	default:
		if v == nil {
			return ""
		}
		return fmt.Sprintf("%v", v)
	}
}

func parseContentBlocks(content any) []protocol.Event {
	if m, ok := content.(map[string]any); ok {
		kind := strField(m, "type")
		if kind == "thinking" || kind == "reasoning" || kind == "redacted_thinking" || kind == "thought" || strField(m, "thinking") != "" {
			return parseContentBlocks([]any{m})
		}
	}
	arr, ok := content.([]any)
	if !ok {
		text := ContentToText(content)
		if strings.TrimSpace(text) == "" {
			return nil
		}
		return []protocol.Event{{Type: "assistant", Role: "assistant", Text: FullText(text)}}
	}
	var out []protocol.Event
	for _, item := range arr {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		kind := strField(m, "type")
		if kind == "" && strField(m, "thinking") != "" {
			kind = "thinking"
		}
		switch kind {
		case "tool_use", "tool-use":
			name := strField(m, "name", "toolName", "tool_name")
			if name == "" {
				name = "tool"
			}
			args := m["input"]
			if args == nil {
				args = m["arguments"]
			}
			out = append(out, toolEvent("tool_call", name, args))
			if ask, ok := AskFromTool(name, args, strField(m, "id", "tool_use_id", "toolUseId")); ok {
				out = append(out, ask)
			}
		case "tool_result", "tool-result":
			text := ContentToText(m)
			out = append(out, toolEvent("tool_result", strField(m, "name", "toolName"), text))
		case "thinking", "reasoning", "redacted_thinking", "thought", "reasoning_text":
			text := strField(m, "thinking", "text", "content")
			if text == "" {
				text = ContentToText(m)
			}
			if text != "" {
				out = append(out, protocol.Event{Type: "thinking", Role: "assistant", Text: FullText(text)})
			}
		case "text", "input_text", "output_text":
			text := strField(m, "text")
			if text != "" {
				out = append(out, protocol.Event{Type: "assistant", Role: "assistant", Text: FullText(text)})
			}
		}
	}
	return out
}

func argsMap(args any) map[string]any {
	switch t := args.(type) {
	case map[string]any:
		return t
	case string:
		s := strings.TrimSpace(t)
		if s == "" {
			return nil
		}
		var m map[string]any
		if json.Unmarshal([]byte(s), &m) == nil {
			return m
		}
	}
	return nil
}

func compactToolArgs(args any, display string) any {
	m := argsMap(args)
	if m != nil {
		out := map[string]any{}
		for _, k := range []string{"command", "cmd", "path", "file_path", "filePath", "target_file", "glob_pattern", "globPattern", "query", "pattern", "glob", "url", "uri", "target_directory", "targetDirectory"} {
			if s, ok := m[k].(string); ok && s != "" {
				out[k] = s
			}
		}
		if len(out) > 0 {
			return out
		}
	}
	if display != "" {
		return display
	}
	if s, ok := args.(string); ok && len(s) > 400 {
		return Truncate(s, 400)
	}
	return args
}

func workbuddyFunctionCall(obj map[string]any) protocol.Event {
	name := strField(obj, "name", "title")
	display := ""
	if pd, ok := obj["providerData"].(map[string]any); ok {
		display = strField(pd, "argumentsDisplayText")
	}
	ev := toolEvent("tool_call", name, compactToolArgs(obj["arguments"], display))
	if display != "" {
		ev.Preview = Truncate(name+" "+display, 160)
	}
	return ev
}

func workbuddyFunctionResult(obj map[string]any) protocol.Event {
	name := strField(obj, "name")
	text := ContentToText(obj["output"])
	if text == "" {
		text = ContentToText(obj["content"])
	}
	if text == "" {
		text = strField(obj, "status")
	}
	return toolEvent("tool_result", name, text)
}

func cursorStreamToolEvents(obj map[string]any) []protocol.Event {
	inner, _ := obj["tool_call"].(map[string]any)
	if inner == nil {
		return nil
	}
	name, body := cursorToolPart(inner)
	if name == "" || body == nil {
		return nil
	}
	args := body["args"]
	if args == nil {
		args = body["arguments"]
	}
	if args == nil {
		args = map[string]any{}
	}
	out := []protocol.Event{toolEvent("tool_call", name, args)}
	if strField(obj, "subtype") != "completed" {
		if ask, ok := AskFromTool(name, args, strField(obj, "call_id", "id")); ok {
			out = append(out, ask)
		}
		return out
	}
	if text := cursorToolResultText(body["result"]); text != "" {
		out = append(out, toolEvent("tool_result", name, text))
	}
	return out
}

func cursorToolPart(inner map[string]any) (string, map[string]any) {
	for k, v := range inner {
		body, ok := v.(map[string]any)
		if !ok {
			continue
		}
		if strings.HasSuffix(k, "ToolCall") {
			name := strings.TrimSuffix(k, "ToolCall")
			if name == "" {
				continue
			}
			return strings.ToUpper(name[:1]) + name[1:], body
		}
	}
	fn, _ := inner["function"].(map[string]any)
	if fn == nil {
		return "", nil
	}
	name := strField(fn, "name")
	if name == "" {
		name = "tool"
	}
	return name, fn
}

func cursorToolResultText(result any) string {
	if result == nil {
		return ""
	}
	if s, ok := result.(string); ok {
		return s
	}
	m, ok := result.(map[string]any)
	if !ok {
		return ContentToText(result)
	}
	if success, ok := m["success"].(map[string]any); ok {
		if t := cursorSuccessText(success); t != "" {
			return t
		}
	}
	if t := cursorSuccessText(m); t != "" {
		return t
	}
	for _, key := range []string{"error", "failure"} {
		fail := m[key]
		if fail == nil {
			continue
		}
		if s := ContentToText(fail); s != "" {
			return s
		}
		if fm, ok := fail.(map[string]any); ok {
			if s := strField(fm, "message", "stderr", "error"); s != "" {
				return s
			}
		}
	}
	return strings.TrimSpace(ContentToText(m))
}

func cursorSuccessText(success map[string]any) string {
	parts := make([]string, 0, 2)
	for _, k := range []string{"stdout", "content", "output", "text", "diff", "markdown"} {
		if s, ok := success[k].(string); ok && s != "" {
			parts = append(parts, s)
		}
	}
	if s, ok := success["stderr"].(string); ok && s != "" {
		parts = append(parts, s)
	}
	if len(parts) > 0 {
		return strings.Join(parts, "\n")
	}
	for _, k := range []string{"files", "entries", "names"} {
		if t := joinListItems(success[k]); t != "" {
			return t
		}
	}
	return ""
}

func joinListItems(v any) string {
	if v == nil {
		return ""
	}
	arr, ok := v.([]any)
	if !ok {
		return ""
	}
	parts := make([]string, 0, len(arr))
	for _, item := range arr {
		switch t := item.(type) {
		case string:
			if t != "" {
				parts = append(parts, t)
			}
		case map[string]any:
			name := strField(t, "name", "path", "file", "filename")
			if name != "" {
				parts = append(parts, name)
			}
		}
	}
	return strings.Join(parts, "\n")
}

func toolCallEvent(obj map[string]any) protocol.Event {
	name := strField(obj, "title", "toolName", "tool_name", "name", "kind")
	inner, _ := obj["tool_call"].(map[string]any)
	if inner != nil {
		if name == "" {
			name = strField(inner, "name", "toolName", "case", "type")
		}
		if tool, ok := inner["tool"].(map[string]any); ok {
			if name == "" {
				name = strField(tool, "case", "type", "name")
			}
			if val := tool["value"]; val != nil {
				inner = map[string]any{"value": val}
				if vm, ok := val.(map[string]any); ok {
					inner = vm
				}
			}
		}
	}
	if name == "" {
		name = "tool"
	}
	args := obj["input"]
	if args == nil && inner != nil {
		args = inner
	}
	if args == nil {
		args = obj
	}
	return toolEvent("tool_call", name, args)
}

func toolEvent(kind, name string, payload any) protocol.Event {
	if name == "" {
		name = "tool"
	}
	full := FullText(payloadText(payload))
	prev := Truncate(name+" "+previewArgs(payload), 160)
	role := "assistant"
	if kind == "tool_result" {
		role = "tool"
	}
	return protocol.Event{Type: kind, Role: role, Tool: name, Text: full, Preview: prev}
}

func payloadText(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case []byte:
		return string(t)
	default:
		if t == nil {
			return ""
		}
		raw, err := json.MarshalIndent(t, "", "  ")
		if err != nil {
			return fmt.Sprintf("%v", t)
		}
		return string(raw)
	}
}

func parseVendorJSONL(obj map[string]any, kind string) []protocol.Event {
	switch kind {
	case "step_start":
		if sid := sessionIDOf(obj); sid != "" {
			return []protocol.Event{{Type: "session", Session: sid}}
		}
		return []protocol.Event{}
	case "step_finish":
		part, _ := obj["part"].(map[string]any)
		if strField(part, "reason") == "stop" {
			return []protocol.Event{{Type: "log", Done: true}}
		}
		return []protocol.Event{}
	case "tool_use":
		return openCodeToolEvents(obj)
	case "thinking":
		if strField(obj, "subtype") == "completed" && strField(obj, "text", "thinking") == "" && ContentToText(obj["content"]) == "" {
			return []protocol.Event{}
		}
		return nil
	case "tool_call", "tool_call_update", "tool-call":
		if evs := cursorStreamToolEvents(obj); evs != nil {
			return evs
		}
		return nil
	case "error":
		if text := nestedErrorText(obj); text != "" {
			return []protocol.Event{{Type: "log", Text: text}}
		}
		return nil
	case "session":
		sid := strField(obj, "id", "sessionID", "sessionId")
		name := strField(obj, "name", "title")
		var evs []protocol.Event
		if sid != "" {
			evs = append(evs, protocol.Event{Type: "session", Session: sid})
		}
		if name != "" {
			evs = append(evs, protocol.Event{Type: "title", Text: Truncate(name, 80)})
		}
		if len(evs) == 0 {
			return []protocol.Event{}
		}
		return evs
	case "session_info_changed":
		name := strField(obj, "name", "title")
		if name == "" {
			return []protocol.Event{}
		}
		return []protocol.Event{{Type: "title", Text: Truncate(name, 80)}}
	case "message_update":
		return piMessageUpdate(obj)
	case "tool_execution_start":
		name := strField(obj, "toolName", "tool")
		return []protocol.Event{toolEvent("tool_call", name, payloadText(obj["args"]))}
	case "tool_execution_end":
		name := strField(obj, "toolName", "tool")
		text := payloadText(obj["result"])
		if text == "" {
			text = payloadText(obj["partialResult"])
		}
		return []protocol.Event{toolEvent("tool_result", name, text)}
	case "agent_settled":
		return []protocol.Event{{Type: "log", Done: true}}
	case "auto_retry_start":
		text := strField(obj, "errorMessage")
		if text == "" {
			text = "Retrying"
		}
		return []protocol.Event{{Type: "log", Text: Truncate(text, 160)}}
	case "event_msg":
		return parseCodexEventMsg(payloadOf(obj))
	case "response_item":
		return parseCodexResponseItem(payloadOf(obj))
	case "session_meta", "session-meta":
		return parseCodexSessionMeta(obj)
	}
	if kind == "text" {
		part, _ := obj["part"].(map[string]any)
		text := strField(part, "text")
		if text == "" {
			text = strField(obj, "text")
		}
		if text == "" {
			return []protocol.Event{}
		}
		return []protocol.Event{{Type: "assistant", Role: "assistant", Text: FullText(text)}}
	}
	if kind == "message" {
		msg, _ := obj["message"].(map[string]any)
		if msg == nil {
			return nil
		}
		return ParseJSONL(msg)
	}
	return nil
}

func openCodeToolEvents(obj map[string]any) []protocol.Event {
	part, _ := obj["part"].(map[string]any)
	if part == nil {
		part = obj
	}
	name := strField(part, "tool", "toolName", "name")
	state, _ := part["state"].(map[string]any)
	input := any(nil)
	output := ""
	if state != nil {
		input = state["input"]
		output = strField(state, "output", "title")
		if output == "" {
			output = payloadText(state["output"])
		}
	}
	if input == nil {
		input = part["input"]
	}
	ev := toolEvent("tool_call", name, payloadText(input))
	if output == "" || output == "null" {
		return []protocol.Event{ev}
	}
	return []protocol.Event{ev, toolEvent("tool_result", name, output)}
}

func payloadOf(obj map[string]any) map[string]any {
	part, _ := obj["payload"].(map[string]any)
	if part != nil {
		return part
	}
	return obj
}

func parseCodexSessionMeta(obj map[string]any) []protocol.Event {
	part := payloadOf(obj)
	sid := strField(part, "id", "session_id", "sessionId")
	if sid == "" {
		sid = strField(obj, "id", "session_id", "sessionId")
	}
	title := strField(part, "title", "aiTitle", "customTitle")
	if title == "" {
		title = strField(obj, "title", "aiTitle", "customTitle")
	}
	var evs []protocol.Event
	if sid != "" {
		evs = append(evs, protocol.Event{Type: "session", Session: sid})
	}
	if title != "" {
		evs = append(evs, protocol.Event{Type: "title", Text: Truncate(title, 80)})
	}
	if len(evs) == 0 {
		return []protocol.Event{}
	}
	return evs
}

func parseCodexEventMsg(part map[string]any) []protocol.Event {
	switch strField(part, "type") {
	case "user_message":
		text := ExtractUserText(strField(part, "message", "text"))
		if text == "" {
			text = ExtractUserText(ContentToText(part["content"]))
		}
		if text == "" || codexInjectedUser(text) {
			return []protocol.Event{}
		}
		return []protocol.Event{{Type: "user", Role: "user", Text: FullText(text)}}
	case "agent_message":
		text := strField(part, "message", "text")
		if text == "" {
			text = ContentToText(part["content"])
		}
		if text == "" {
			return []protocol.Event{}
		}
		return []protocol.Event{{Type: "assistant", Role: "assistant", Text: FullText(text)}}
	case "agent_reasoning":
		text := strField(part, "text", "message")
		if text == "" {
			text = ContentToText(part["summary"])
		}
		if text == "" {
			return []protocol.Event{}
		}
		return []protocol.Event{{Type: "thinking", Role: "assistant", Text: FullText(text)}}
	default:
		return []protocol.Event{}
	}
}

func parseCodexResponseItem(part map[string]any) []protocol.Event {
	switch strField(part, "type") {
	case "message":
		role := strField(part, "role")
		if role == "developer" || role == "system" {
			return []protocol.Event{}
		}
		if role == "user" {
			text := ExtractUserText(ContentToText(part["content"]))
			if text == "" || codexInjectedUser(text) {
				return []protocol.Event{}
			}
			return []protocol.Event{{Type: "user", Role: "user", Text: FullText(text)}}
		}
		if role == "assistant" {
			text := ContentToText(part["content"])
			if text == "" {
				return []protocol.Event{}
			}
			return []protocol.Event{{Type: "assistant", Role: "assistant", Text: FullText(text)}}
		}
		return []protocol.Event{}
	case "function_call", "custom_tool_call":
		name := strField(part, "name")
		args := part["arguments"]
		if args == nil {
			args = part["input"]
		}
		return []protocol.Event{toolEvent("tool_call", name, args)}
	case "function_call_output", "custom_tool_call_output":
		text := strField(part, "output")
		if text == "" {
			text = ContentToText(part["output"])
		}
		return []protocol.Event{toolEvent("tool_result", strField(part, "name"), text)}
	case "reasoning":
		text := ContentToText(part["summary"])
		if text == "" {
			text = ContentToText(part["content"])
		}
		if text == "" {
			text = strField(part, "text")
		}
		if text == "" {
			return []protocol.Event{}
		}
		return []protocol.Event{{Type: "thinking", Role: "assistant", Text: FullText(text)}}
	default:
		return []protocol.Event{}
	}
}

var codexInjectedPrefixes = []string{
	"<environment_context>",
	"# agents.md",
	"<instructions>",
	"<permissions",
	"<skill>",
	"<turn_aborted>",
	"<collaboration_mode>",
	"<multi_agent",
	"<skills_instructions>",
}

func codexInjectedUser(text string) bool {
	s := strings.TrimSpace(text)
	if s == "" {
		return true
	}
	low := strings.ToLower(s)
	for _, p := range codexInjectedPrefixes {
		if strings.HasPrefix(low, p) {
			return true
		}
	}
	return false
}

func nestedErrorText(obj map[string]any) string {
	if s := strField(obj, "message", "error"); s != "" && !strings.HasPrefix(s, "{") {
		return s
	}
	raw, _ := obj["error"].(map[string]any)
	if raw == nil {
		return ""
	}
	if s := strField(raw, "message"); s != "" {
		return s
	}
	data, _ := raw["data"].(map[string]any)
	return strField(data, "message")
}

func piMessageUpdate(obj map[string]any) []protocol.Event {
	asst, _ := obj["assistantMessageEvent"].(map[string]any)
	if asst == nil {
		return []protocol.Event{}
	}
	switch strField(asst, "type") {
	case "text_delta":
		t := strField(asst, "delta")
		if t == "" {
			return []protocol.Event{}
		}
		return []protocol.Event{{Type: "delta", Role: "assistant", Text: t}}
	case "thinking_delta":
		t := strField(asst, "delta")
		if t == "" {
			return []protocol.Event{}
		}
		return []protocol.Event{{Type: "thinking", Role: "assistant", Text: FullText(t)}}
	case "toolcall_start":
		return []protocol.Event{toolEvent("tool_call", strField(asst, "toolName"), "")}
	}
	return []protocol.Event{}
}

// ParseJSONL parses one jsonl object into structured events, including tool calls and results.
func ParseJSONL(obj map[string]any) []protocol.Event {
	if obj == nil {
		return nil
	}
	kind := strField(obj, "type")
	role := strField(obj, "role")
	if role == "" {
		role = kind
	}
	if permissionType(kind) || (strField(obj, "request_id", "requestId") != "" && obj["request"] != nil) {
		if evs := askFromControl(obj); len(evs) > 0 {
			return evs
		}
	}
	if evs := parseVendorJSONL(obj, kind); evs != nil {
		return evs
	}
	switch kind {
	case "file-history-snapshot", "mode", "last-prompt", "queue-operation", "available_commands_update":
		return nil
	case "ai-title":
		t := strField(obj, "aiTitle", "title", "text")
		if t == "" {
			return nil
		}
		return []protocol.Event{{Type: "title", Text: Truncate(t, 80)}}
	case "custom-title":
		t := strField(obj, "customTitle", "title", "text")
		if t == "" {
			return nil
		}
		return []protocol.Event{{Type: "title", Text: Truncate(t, 80)}}
	case "session-meta":
		t := strField(obj, "title", "aiTitle", "customTitle")
		if t == "" {
			return nil
		}
		return []protocol.Event{{Type: "title", Text: Truncate(t, 80)}}
	case "system":
		sid := sessionIDOf(obj)
		if sid == "" {
			return nil
		}
		return []protocol.Event{{Type: "session", Session: sid}}
	}
	if b, ok := obj["isMeta"].(bool); ok && b {
		return nil
	}
	msg, _ := obj["message"].(map[string]any)
	var content any
	if msg != nil {
		content = msg["content"]
	} else {
		content = obj["content"]
	}

	if kind == "thinking" || kind == "reasoning" || kind == "thought" || kind == "thinking_delta" {
		text := ContentToText(content)
		if text == "" {
			text = ContentToText(obj["rawContent"])
		}
		if text == "" {
			text = strField(obj, "thinking", "text")
		}
		if text == "" {
			return nil
		}
		return []protocol.Event{{Type: "thinking", Role: "assistant", Text: FullText(text)}}
	}
	if kind == "function_call" {
		ev := workbuddyFunctionCall(obj)
		out := []protocol.Event{ev}
		if ask, ok := AskFromTool(ev.Tool, obj["arguments"], strField(obj, "id")); ok {
			out = append(out, ask)
		}
		return out
	}
	if kind == "function_call_result" {
		return []protocol.Event{workbuddyFunctionResult(obj)}
	}
	if kind == "tool_call" || kind == "tool_call_update" || kind == "tool-call" {
		ev := toolCallEvent(obj)
		out := []protocol.Event{ev}
		if ask, ok := AskFromTool(ev.Tool, obj["input"], strField(obj, "id")); ok {
			out = append(out, ask)
		}
		return out
	}
	if method, _ := obj["method"].(string); method == "session/update" {
		params, _ := obj["params"].(map[string]any)
		update, _ := params["update"].(map[string]any)
		if update == nil {
			update = params
		}
		ukind := strField(update, "sessionUpdate", "type")
		if ukind == "tool_call" || ukind == "tool_call_update" {
			ev := toolCallEvent(update)
			status := strField(update, "status")
			if status != "" {
				ev.Preview = Truncate(ev.Tool+" "+status+" "+ev.Preview, 160)
			}
			return []protocol.Event{ev}
		}
		if ukind == "agent_thought_chunk" {
			text := ContentToText(update["content"])
			if text == "" {
				text = strField(update, "text")
			}
			if text == "" {
				return nil
			}
			return []protocol.Event{{Type: "thinking", Role: "assistant", Text: FullText(text)}}
		}
		if ukind == "agent_message_chunk" {
			text := ContentToText(update["content"])
			if text == "" {
				text = strField(update, "text")
			}
			if text == "" {
				return nil
			}
			return []protocol.Event{{Type: "delta", Role: "assistant", Text: text}}
		}
	}

	if role == "user" || kind == "user" {
		if arr, ok := content.([]any); ok && len(arr) > 0 {
			if m, ok := arr[0].(map[string]any); ok {
				if strField(m, "type") == "tool_result" || strField(m, "type") == "tool-result" {
					return parseContentBlocks(content)
				}
			}
		}
		text := ExtractUserText(ContentToText(content))
		if text == "" {
			return nil
		}
		return []protocol.Event{{Type: "user", Role: "user", Text: FullText(text)}}
	}
	if role == "assistant" || kind == "assistant" {
		if _, ok := content.([]any); ok {
			return parseContentBlocks(content)
		}
		text := ContentToText(content)
		if text == "" {
			return nil
		}
		return []protocol.Event{{Type: "assistant", Role: "assistant", Text: FullText(text)}}
	}
	if role == "tool" || kind == "tool_result" || kind == "tool-result" {
		return []protocol.Event{toolEvent("tool_result", strField(obj, "name", "toolName"), ContentToText(content))}
	}
	if kind == "result" {
		res := strField(obj, "result")
		if res == "" {
			res = ContentToText(obj["result"])
		}
		return []protocol.Event{{Type: "assistant", Role: "assistant", Text: FullText(res), Done: true}}
	}
	return nil
}

// EventsFromJSONL parses events and fills engine/session.
func sessionIDOf(obj map[string]any) string {
	if obj == nil {
		return ""
	}
	if s := strField(obj, "session_id", "sessionId", "sessionID"); s != "" {
		return s
	}
	if msg, ok := obj["message"].(map[string]any); ok {
		if s := strField(msg, "session_id", "sessionId"); s != "" {
			return s
		}
	}
	kind := strField(obj, "type")
	if kind == "session_meta" || kind == "session-meta" {
		if payload, ok := obj["payload"].(map[string]any); ok {
			if s := strField(payload, "session_id", "sessionId", "id"); s != "" {
				return s
			}
		}
	}
	return ""
}

func EventsFromJSONL(obj map[string]any, engine, session string) []protocol.Event {
	rows := ParseJSONL(obj)
	unix := EventTime(obj)
	if session == "" {
		session = sessionIDOf(obj)
	}
	for i := range rows {
		if rows[i].Engine == "" {
			rows[i].Engine = engine
		}
		if rows[i].Session == "" {
			rows[i].Session = session
		}
		if rows[i].Unix == 0 {
			rows[i].Unix = unix
		}
	}
	return rows
}

type jsonlTitleHit struct {
	mtime int64
	title string
}

var jsonlTitleCache sync.Map

// JSONLTitle reads the latest title from the session log, or the first user message.
func JSONLTitle(path string) string {
	if path == "" {
		return ""
	}
	st, err := os.Stat(path)
	if err != nil {
		return ""
	}
	mtime := st.ModTime().UnixNano()
	if hit, ok := jsonlTitleCache.Load(path); ok {
		cached := hit.(jsonlTitleHit)
		if cached.mtime == mtime {
			return cached.title
		}
	}
	title := readJSONLTitle(path, st.Size())
	jsonlTitleCache.Store(path, jsonlTitleHit{mtime: mtime, title: title})
	return title
}

func readJSONLTitle(path string, size int64) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	const win = 64 << 10
	title, user := scanJSONLTitle(f, 0, win, false)
	if size > win {
		later, _ := scanJSONLTitle(f, size-win, win, true)
		if later != "" {
			title = later
		}
	}
	if title != "" {
		return title
	}
	return user
}

func scanJSONLTitle(f *os.File, start, limit int64, skipPartial bool) (title, user string) {
	if _, err := f.Seek(start, 0); err != nil {
		return "", ""
	}
	r := io.LimitReader(f, limit)
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		if skipPartial {
			skipPartial = false
			continue
		}
		var obj map[string]any
		if json.Unmarshal(sc.Bytes(), &obj) != nil {
			continue
		}
		for _, ev := range ParseJSONL(obj) {
			if ev.Type == "title" && ev.Text != "" {
				title = ev.Text
			}
			if ev.Type == "user" && user == "" && ev.Text != "" {
				user = Truncate(ev.Text, 48)
			}
		}
	}
	return title, user
}

// MatchSession matches a unique session by full id, then by prefix.
func MatchSession(rows []protocol.Session, id string) (protocol.Session, error) {
	if id == "" {
		return protocol.Session{}, fmt.Errorf("session %s not unique or missing", id)
	}
	exact := []protocol.Session{}
	prefix := []protocol.Session{}
	for _, s := range rows {
		if s.ID == id {
			exact = append(exact, s)
			continue
		}
		if strings.HasPrefix(s.ID, id) {
			prefix = append(prefix, s)
		}
	}
	if len(exact) == 1 {
		return exact[0], nil
	}
	if len(exact) == 0 && len(prefix) == 1 {
		return prefix[0], nil
	}
	return protocol.Session{}, fmt.Errorf("session %s not unique or missing", id)
}

// WithJSONLTitle fills an empty session title from jsonl.
func WithJSONLTitle(s protocol.Session) protocol.Session {
	if s.Title == "" && s.JSONL != "" {
		s.Title = JSONLTitle(s.JSONL)
	}
	return s
}

// FormatJSONL formats a session jsonl object as a progress line.
func FormatJSONL(obj map[string]any) string {
	var b strings.Builder
	for _, ev := range ParseJSONL(obj) {
		if ev.Text == "" {
			continue
		}
		if b.Len() > 0 {
			b.WriteByte('\n')
		}
		switch ev.Type {
		case "user":
			b.WriteString("[user] ")
		case "assistant", "delta":
			if !strings.HasPrefix(ev.Text, "[") {
				b.WriteString("[assistant] ")
			}
		case "thinking", "reasoning":
			b.WriteString("[thinking] ")
		case "tool_call":
			b.WriteString("[tool call] ")
		case "tool_result":
			b.WriteString("[tool result] ")
		}
		if ev.Preview != "" {
			b.WriteString(ev.Preview)
		} else {
			b.WriteString(Truncate(ev.Text, 240))
		}
	}
	return b.String()
}

// ReadJSONFile reads a JSON object.
func ReadJSONFile(path string) (map[string]any, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var obj map[string]any
	if err := json.Unmarshal(raw, &obj); err != nil {
		return nil, err
	}
	return obj, nil
}

// Home user home directory.
func Home() string {
	h, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return h
}

// Epoch converts several timestamp shapes to Unix seconds.
func Epoch(v any) int64 {
	switch n := v.(type) {
	case float64:
		if n > 1e12 {
			return int64(n / 1000)
		}
		return int64(n)
	case int64:
		if n > 1e12 {
			return n / 1000
		}
		return n
	case json.Number:
		f, _ := n.Float64()
		return Epoch(f)
	case string:
		s := strings.TrimSpace(n)
		if s == "" {
			return 0
		}
		if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
			return t.Unix()
		}
		if t, err := time.Parse(time.RFC3339, s); err == nil {
			return t.Unix()
		}
		return 0
	default:
		return 0
	}
}

// MtimeUnix file mtime.
func MtimeUnix(path string) int64 {
	st, err := os.Stat(path)
	if err != nil {
		return 0
	}
	return st.ModTime().Unix()
}

// FilterSince keeps events after since; entries without a timestamp are always kept.
func FilterSince(evs []protocol.Event, since int64) []protocol.Event {
	if since <= 0 || len(evs) == 0 {
		return evs
	}
	out := make([]protocol.Event, 0, len(evs))
	for _, ev := range evs {
		if ev.Unix == 0 || ev.Unix >= since {
			out = append(out, ev)
		}
	}
	return out
}

// LastTurns keeps the last n user turns and the events after them.
func LastTurns(evs []protocol.Event, n int) []protocol.Event {
	if n <= 0 || len(evs) == 0 {
		if evs == nil {
			return []protocol.Event{}
		}
		return evs
	}
	starts := make([]int, 0, 32)
	for i, ev := range evs {
		if ev.Type == "user" || ev.Role == "user" {
			starts = append(starts, i)
		}
	}
	if len(starts) <= n {
		return evs
	}
	return evs[starts[len(starts)-n]:]
}

// Recent whether the file changed in the last 20 seconds.
func Recent(path string) bool {
	st, err := os.Stat(path)
	if err != nil {
		return false
	}
	return time.Since(st.ModTime()) <= 20*time.Second
}

// FileExists whether the path exists.
func FileExists(path string) bool {
	if path == "" {
		return false
	}
	_, err := os.Stat(path)
	return err == nil
}

// TitleOf engine display name.
func TitleOf(name string) string {
	switch name {
	case "workbuddy":
		return "WorkBuddy"
	case "claude":
		return "Claude Code"
	case "cursor":
		return "Cursor Agent"
	case "codex":
		return "Codex"
	case "opencode":
		return "OpenCode"
	case "pi":
		return "Pi"
	case "qoder-cn", "qodercn":
		return "Qoder CN"
	default:
		return name
	}
}

// DataHome user data directory.
func DataHome() string {
	if x := os.Getenv("XDG_DATA_HOME"); x != "" {
		return x
	}
	return filepath.Join(Home(), ".local", "share")
}

// StreamJSONL turns JSONL on stdout into events.
func StreamJSONL(r io.Reader, engine, session string, emit func(protocol.Event)) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	sid := session
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var obj map[string]any
		if json.Unmarshal([]byte(line), &obj) != nil {
			emit(protocol.Event{Type: "log", Engine: engine, Session: sid, Text: line})
			continue
		}
		for _, ev := range EventsFromJSONL(obj, engine, sid) {
			if ev.Session != "" {
				sid = ev.Session
			}
			emit(ev)
		}
	}
}

// RunOutput runs a command with a timeout and returns combined output.
func RunOutput(ctx context.Context, timeout time.Duration, bin string, args ...string) (string, error) {
	if bin == "" {
		return "", os.ErrNotExist
	}
	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}
	cmd := exec.CommandContext(ctx, bin, args...)
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	err := cmd.Run()
	return strings.TrimSpace(buf.String()), err
}

// BinVersion tries to read a command version.
func BinVersion(ctx context.Context, bin string) string {
	for _, args := range [][]string{{"--version"}, {"-v"}, {"version"}} {
		out, err := RunOutput(ctx, 2*time.Second, bin, args...)
		if err != nil || out == "" {
			continue
		}
		line := strings.Split(out, "\n")[0]
		return Truncate(strings.TrimSpace(line), 80)
	}
	return ""
}
