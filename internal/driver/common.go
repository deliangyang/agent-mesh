package driver

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"syscall"
	"time"

	"agent-mesh/internal/protocol"
)

var userQueryRe = regexp.MustCompile(`(?s)<user_query>(.*?)</user_query>`)
var envelopeOpenRe = regexp.MustCompile(`(?i)<([a-z][a-z0-9_]*)\b[^>]*>`)
var leftoverEnvelopeTagRe = regexp.MustCompile(`(?i)</?(?:[a-z][a-z0-9]*_[a-z0-9_]*|timestamp|communication)\b[^>]*>`)

// PIDAlive 判断进程是否存在。
func PIDAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	return err == nil
}

// Truncate 压缩空白并截断。
func Truncate(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) <= n {
		return s
	}
	if n < 2 {
		return s
	}
	return s[:n-1] + "…"
}

const maxEventText = 512 * 1024

// FullText 保留换行，仅在超长时截断。
func FullText(s string) string {
	s = strings.TrimRight(s, "\n")
	if len(s) <= maxEventText {
		return s
	}
	return s[:maxEventText] + "\n…(已截断)"
}

// EventTime 从 jsonl 对象取时间戳。
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

// ExtractUserText 抽出用户可见文本。
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

// ContentToText 将内容块转为文本。
func ContentToText(content any) string {
	if content == nil {
		return ""
	}
	switch v := content.(type) {
	case string:
		return v
	case []any:
		var b strings.Builder
		for _, item := range v {
			b.WriteString(ContentToText(item))
		}
		return b.String()
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
		for _, k := range []string{"command", "cmd", "path", "file_path", "filePath", "target_file", "query", "pattern", "glob", "url", "description"} {
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
		text := ExtractUserText(ContentToText(content))
		if text == "" {
			text = ContentToText(content)
		}
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
			text := ExtractUserText(strField(m, "text"))
			if text == "" {
				text = strField(m, "text")
			}
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
		for _, k := range []string{"command", "cmd", "path", "file_path", "filePath", "target_file", "query", "pattern", "glob", "url", "uri"} {
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

// ParseJSONL 把一条 jsonl 解析为结构化事件（含工具调用/结果）。
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

// EventsFromJSONL 解析并补上引擎/会话。
func sessionIDOf(obj map[string]any) string {
	if obj == nil {
		return ""
	}
	if s := strField(obj, "session_id", "sessionId"); s != "" {
		return s
	}
	if msg, ok := obj["message"].(map[string]any); ok {
		if s := strField(msg, "session_id", "sessionId"); s != "" {
			return s
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

// JSONLTitle 从会话记录里取最新标题，没有则用首条用户消息。
func JSONLTitle(path string) string {
	if path == "" {
		return ""
	}
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return ""
	}
	const max = 64 << 10
	start := st.Size() - max
	if start < 0 {
		start = 0
	}
	if start > 0 {
		if _, err := f.Seek(start, 0); err != nil {
			return ""
		}
	}
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	title := ""
	user := ""
	skipPartial := start > 0
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
	if title != "" {
		return title
	}
	return user
}

// WithJSONLTitle 会话标题为空时从 jsonl 补上。
func WithJSONLTitle(s protocol.Session) protocol.Session {
	if s.Title == "" && s.JSONL != "" {
		s.Title = JSONLTitle(s.JSONL)
	}
	return s
}

// FormatJSONL 将会话 jsonl 对象格式化为进度行。
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
			b.WriteString("[用户] ")
		case "assistant", "delta":
			if !strings.HasPrefix(ev.Text, "[") {
				b.WriteString("[助手] ")
			}
		case "thinking", "reasoning":
			b.WriteString("[思考] ")
		case "tool_call":
			b.WriteString("[工具调用] ")
		case "tool_result":
			b.WriteString("[工具结果] ")
		}
		if ev.Preview != "" {
			b.WriteString(ev.Preview)
		} else {
			b.WriteString(Truncate(ev.Text, 240))
		}
	}
	return b.String()
}

// ReadJSONFile 读取 JSON 对象。
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

// Home 用户主目录。
func Home() string {
	h, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return h
}

// Epoch 将多种时间戳转为 Unix 秒。
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

// MtimeUnix 文件修改时间。
func MtimeUnix(path string) int64 {
	st, err := os.Stat(path)
	if err != nil {
		return 0
	}
	return st.ModTime().Unix()
}

// FilterSince 只保留 since 之后的事件；无时间戳的条目一律保留。
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

// LastTurns 只保留最近 n 轮用户发言及其后的事件。
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

// Recent 文件是否在 20 秒内更新。
func Recent(path string) bool {
	st, err := os.Stat(path)
	if err != nil {
		return false
	}
	return time.Since(st.ModTime()) <= 20*time.Second
}

// FileExists 路径是否存在。
func FileExists(path string) bool {
	if path == "" {
		return false
	}
	_, err := os.Stat(path)
	return err == nil
}

// TitleOf 引擎显示名。
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
	default:
		return name
	}
}

// RunOutput 带超时执行命令，返回合并后的输出。
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

// BinVersion 尝试读取命令版本。
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
