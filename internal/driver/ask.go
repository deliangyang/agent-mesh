package driver

import (
	"encoding/json"
	"strings"

	"agent-mesh/internal/protocol"
)

// AskPrompt is a question or permission prompt the user must answer now.
type AskPrompt struct {
	ID        string        `json:"id,omitempty"`
	Kind      string        `json:"kind"`
	Title     string        `json:"title,omitempty"`
	Questions []AskQuestion `json:"questions"`
}

// AskQuestion is one question.
type AskQuestion struct {
	ID      string      `json:"id,omitempty"`
	Prompt  string      `json:"prompt"`
	Header  string      `json:"header,omitempty"`
	Detail  string      `json:"detail,omitempty"` // plan text or long prompt body
	Multi   bool        `json:"multi,omitempty"`
	Options []AskOption `json:"options"`
}

// AskOption is a choice.
type AskOption struct {
	ID          string `json:"id"`
	Label       string `json:"label"`
	Hint        string `json:"hint,omitempty"`
	Recommended bool   `json:"recommended,omitempty"`
}

func askToolName(name string) bool {
	n := strings.ToLower(name)
	n = strings.ReplaceAll(n, "_", "")
	n = strings.ReplaceAll(n, "-", "")
	n = strings.ReplaceAll(n, " ", "")
	switch n {
	case "askuserquestion", "askquestion", "askuser", "question", "requestuserinput":
		return true
	}
	return strings.Contains(n, "askuser") || strings.HasSuffix(n, "question")
}

// planToolName matches tools that submit a plan for approval.
func planToolName(name string) bool {
	n := strings.ToLower(name)
	n = strings.ReplaceAll(n, "_", "")
	n = strings.ReplaceAll(n, "-", "")
	n = strings.ReplaceAll(n, " ", "")
	switch n {
	case "exitplanmode", "presentplan", "submitplan", "planapproval", "plan":
		return true
	}
	return false
}

func permissionType(kind string) bool {
	k := strings.ToLower(kind)
	return k == "control_request" || k == "permission" || k == "permission_request" || k == "can_use_tool"
}

// AskFromTool turns tool arguments into an ask event.
func AskFromTool(name string, args any, id string) (protocol.Event, bool) {
	if planToolName(name) {
		return planAsk(name, args, id)
	}
	if !askToolName(name) {
		return protocol.Event{}, false
	}
	p := parseAskArgs(args)
	if len(p.Questions) == 0 {
		return protocol.Event{}, false
	}
	if p.Kind == "" {
		p.Kind = "question"
	}
	p.ID = id
	if p.Title == "" {
		p.Title = firstAskTitle(p)
	}
	raw, _ := json.Marshal(p)
	return protocol.Event{
		Type:    "ask",
		Role:    "assistant",
		Tool:    name,
		Text:    string(raw),
		Preview: Truncate(p.Title, 120),
	}, true
}

// planAsk turns an ExitPlanMode-style tool call into a plan approval prompt.
func planAsk(name string, args any, id string) (protocol.Event, bool) {
	m := argsMap(args)
	plan := strField(m, "plan", "text", "content", "summary", "description")
	if plan == "" {
		plan = payloadText(args)
	}
	plan = strings.TrimSpace(plan)
	if plan == "" {
		return protocol.Event{}, false
	}
	if id == "" {
		id = "plan"
	}
	p := AskPrompt{
		ID:    id,
		Kind:  "plan",
		Title: Truncate(plan, 120),
		Questions: []AskQuestion{{
			ID:     id,
			Prompt: "Approve this plan?",
			Header: "Plan",
			Detail: Truncate(plan, 8000),
			Options: []AskOption{
				{ID: "approve", Label: "Approve", Recommended: true},
				{ID: "keep-planning", Label: "Keep planning"},
			},
		}},
	}
	raw, _ := json.Marshal(p)
	return protocol.Event{
		Type:    "ask",
		Role:    "assistant",
		Tool:    name,
		Text:    string(raw),
		Preview: Truncate(p.Title, 120),
	}, true
}

func askFromControl(obj map[string]any) []protocol.Event {
	req, _ := obj["request"].(map[string]any)
	if req == nil {
		req = obj
	}
	sub := strField(req, "subtype", "type")
	if sub != "can_use_tool" && sub != "permission" && !permissionType(strField(obj, "type")) {
		if strField(req, "tool_name", "toolName") == "" {
			return nil
		}
	}
	id := strField(obj, "request_id", "requestId", "id")
	tool := strField(req, "tool_name", "toolName", "name")
	if tool == "" {
		tool = "tool"
	}
	if planToolName(tool) {
		if ev, ok := planAsk(tool, req["input"], id); ok {
			return []protocol.Event{ev}
		}
	}
	detail := previewArgs(req["input"])
	if detail == "" {
		detail = payloadText(req["input"])
	}
	prompt := "Allow " + tool + "?"
	if strings.TrimSpace(detail) != "" {
		prompt = tool + "\n" + Truncate(detail, 240)
	}
	p := AskPrompt{
		ID:    id,
		Kind:  "permission",
		Title: "Confirm: " + tool,
		Questions: []AskQuestion{{
			ID:     id,
			Prompt: prompt,
			Header: tool,
			Options: []AskOption{
				{ID: "allow", Label: "Allow"},
				{ID: "deny", Label: "Deny"},
			},
		}},
	}
	raw, _ := json.Marshal(p)
	return []protocol.Event{{
		Type:    "ask",
		Role:    "assistant",
		Tool:    tool,
		Text:    string(raw),
		Preview: Truncate(p.Title, 120),
	}}
}

func parseAskArgs(args any) AskPrompt {
	m := argsMap(args)
	if m == nil {
		return AskPrompt{}
	}
	p := AskPrompt{Kind: "question", Title: strField(m, "title", "header")}
	if q := parseQuestionList(m["questions"]); len(q) > 0 {
		p.Questions = q
		return p
	}
	if q, ok := parseOneQuestion(m); ok {
		p.Questions = []AskQuestion{q}
	}
	return p
}

func parseQuestionList(v any) []AskQuestion {
	switch t := v.(type) {
	case []any:
		var out []AskQuestion
		for _, item := range t {
			if q, ok := parseOneQuestion(item); ok {
				out = append(out, q)
			}
		}
		return out
	case map[string]any:
		if q, ok := parseOneQuestion(t); ok {
			return []AskQuestion{q}
		}
	}
	return nil
}

func parseOneQuestion(v any) (AskQuestion, bool) {
	m, ok := v.(map[string]any)
	if !ok {
		return AskQuestion{}, false
	}
	prompt := strField(m, "question", "prompt", "text", "title", "message")
	opts := parseOptions(m["options"])
	if prompt == "" && len(opts) == 0 {
		return AskQuestion{}, false
	}
	if prompt == "" {
		prompt = strField(m, "header")
	}
	if prompt == "" {
		prompt = "Please choose"
	}
	multi, _ := m["multiSelect"].(bool)
	if !multi {
		multi, _ = m["multi"].(bool)
	}
	return AskQuestion{
		ID:      strField(m, "id", "header"),
		Prompt:  prompt,
		Header:  strField(m, "header"),
		Multi:   multi,
		Options: opts,
	}, true
}

func parseOptions(v any) []AskOption {
	arr, ok := v.([]any)
	if !ok {
		return nil
	}
	var out []AskOption
	for i, item := range arr {
		switch t := item.(type) {
		case string:
			s := strings.TrimSpace(t)
			if s == "" {
				continue
			}
			out = append(out, AskOption{ID: s, Label: s})
		case map[string]any:
			label := strField(t, "label", "title", "text", "name", "value")
			id := strField(t, "id", "value", "label")
			if label == "" {
				label = id
			}
			if label == "" {
				continue
			}
			if id == "" {
				id = label
			}
			rec, _ := t["recommended"].(bool)
			if !rec {
				low := strings.ToLower(label)
				rec = strings.Contains(low, "(recommended)") || strings.Contains(low, "（推荐）")
			}
			out = append(out, AskOption{
				ID:          id,
				Label:       label,
				Hint:        strField(t, "description", "hint", "detail"),
				Recommended: rec,
			})
		default:
			_ = i
		}
	}
	return out
}

func firstAskTitle(p AskPrompt) string {
	if p.Title != "" {
		return p.Title
	}
	if len(p.Questions) == 0 {
		return "Need you to choose"
	}
	q := p.Questions[0]
	if q.Header != "" {
		return q.Header
	}
	return Truncate(q.Prompt, 80)
}

// FormatAskAnswer formats the user's choice into a reply the engine can read.
func FormatAskAnswer(prompt AskPrompt, picks map[string][]string) string {
	if prompt.Kind == "permission" {
		for _, q := range prompt.Questions {
			sel := picks[q.ID]
			if len(sel) == 0 {
				sel = picks[q.Prompt]
			}
			if len(sel) == 0 {
				continue
			}
			v := strings.ToLower(sel[0])
			if v == "allow" || v == "允许" {
				return "允许"
			}
			if v == "deny" || v == "拒绝" {
				return "拒绝"
			}
			return sel[0]
		}
		return ""
	}
	answers := map[string]string{}
	var lines []string
	for _, q := range prompt.Questions {
		key := q.ID
		if key == "" {
			key = q.Prompt
		}
		sel := picks[key]
		if len(sel) == 0 {
			sel = picks[q.Prompt]
		}
		if len(sel) == 0 {
			continue
		}
		joined := strings.Join(sel, ", ")
		answers[q.Prompt] = joined
		title := q.Header
		if title == "" {
			title = q.Prompt
		}
		lines = append(lines, title+" → "+joined)
	}
	raw, _ := json.Marshal(map[string]any{"answers": answers})
	if len(lines) == 0 {
		return string(raw)
	}
	return strings.Join(lines, "\n") + "\n" + string(raw)
}
