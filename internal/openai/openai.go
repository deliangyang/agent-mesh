package openai

import (
	"crypto/rand"
	"encoding/hex"
	"strings"
	"time"

	"agent-mesh/internal/protocol"
)

// Chunk matches OpenAI chat.completion.chunk.
type Chunk struct {
	ID      string   `json:"id"`
	Object  string   `json:"object"`
	Created int64    `json:"created"`
	Model   string   `json:"model"`
	Choices []Choice `json:"choices"`
	Mesh    Mesh     `json:"agent_mesh,omitempty"`
}

// Choice is a streaming choice.
type Choice struct {
	Index        int     `json:"index"`
	Delta        Delta   `json:"delta"`
	FinishReason *string `json:"finish_reason"`
}

// Delta is a delta.
type Delta struct {
	Role    string `json:"role,omitempty"`
	Content string `json:"content,omitempty"`
}

// Mesh is mesh metadata clients may ignore.
type Mesh struct {
	Engine   string `json:"engine,omitempty"`
	Session  string `json:"session,omitempty"`
	NodeID   string `json:"node_id,omitempty"`
	NodeName string `json:"node_name,omitempty"`
	Type     string `json:"type,omitempty"`
	Role     string `json:"role,omitempty"`
	Tool     string `json:"tool,omitempty"`
	Preview  string `json:"preview,omitempty"`
}

// ModelList is an OpenAI /v1/models response.
type ModelList struct {
	Object string      `json:"object"`
	Data   []ModelCard `json:"data"`
}

// ModelCard is an OpenAI model card.
type ModelCard struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	Created int64  `json:"created"`
	OwnedBy string `json:"owned_by"`
}

// Completion is a non-streaming OpenAI chat.completion.
type Completion struct {
	ID      string             `json:"id"`
	Object  string             `json:"object"`
	Created int64              `json:"created"`
	Model   string             `json:"model"`
	Choices []CompletionChoice `json:"choices"`
	Mesh    Mesh               `json:"agent_mesh,omitempty"`
}

// CompletionChoice is a non-streaming choice.
type CompletionChoice struct {
	Index        int    `json:"index"`
	Message      Delta  `json:"message"`
	FinishReason string `json:"finish_reason"`
}

// ChatRequest is an OpenAI chat.completions request plus mesh fields.
type ChatRequest struct {
	Model    string        `json:"model"`
	Stream   bool          `json:"stream"`
	Messages []ChatMessage `json:"messages"`
	Session  string        `json:"session,omitempty"`
	CWD      string        `json:"cwd,omitempty"`
	Engine   string        `json:"engine,omitempty"`
	Files    []ChatFile    `json:"files,omitempty"`
}

// ChatMessage is a chat message.
type ChatMessage struct {
	Role    string `json:"role"`
	Content any    `json:"content"`
}

// NewID creates a chatcmpl id.
func NewID() string {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return "chatcmpl-" + hex.EncodeToString(b[:])
}

// FromModels turns engine models into an OpenAI list.
func FromModels(rows []protocol.Model) ModelList {
	out := ModelList{Object: "list", Data: []ModelCard{}}
	now := time.Now().Unix()
	for _, m := range rows {
		out.Data = append(out.Data, ModelCard{
			ID:      m.ID,
			Object:  "model",
			Created: now,
			OwnedBy: m.Engine,
		})
	}
	return out
}

// FromEvent turns engine progress into an OpenAI chunk. Done events include finish_reason=stop.
func FromEvent(id string, model string, nodeID, nodeName string, ev protocol.Event) Chunk {
	if id == "" {
		id = NewID()
	}
	if model == "" && ev.Engine != "" {
		model = ev.Engine
	}
	chunk := Chunk{
		ID:      id,
		Object:  "chat.completion.chunk",
		Created: time.Now().Unix(),
		Model:   model,
		Mesh: Mesh{
			Engine:   ev.Engine,
			Session:  ev.Session,
			NodeID:   nodeID,
			NodeName: nodeName,
			Type:     ev.Type,
			Role:     ev.Role,
			Tool:     ev.Tool,
			Preview:  ev.Preview,
		},
	}
	text := strings.TrimSpace(ev.Text)
	delta := Delta{}
	if text != "" {
		if ev.Type == "delta" {
			delta.Content = ev.Text
		} else {
			delta.Content = ev.Text + "\n"
		}
	}
	var reason *string
	if ev.Done {
		stop := "stop"
		reason = &stop
	}
	chunk.Choices = []Choice{{Index: 0, Delta: delta, FinishReason: reason}}
	return chunk
}

// LastUserText returns the last user text.
func LastUserText(msgs []ChatMessage) string {
	for i := len(msgs) - 1; i >= 0; i-- {
		if strings.EqualFold(msgs[i].Role, "user") {
			return contentText(msgs[i].Content)
		}
	}
	if len(msgs) == 0 {
		return ""
	}
	return contentText(msgs[len(msgs)-1].Content)
}

func contentText(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case []any:
		var b strings.Builder
		for _, item := range t {
			b.WriteString(contentText(item))
		}
		return b.String()
	case map[string]any:
		if s, ok := t["text"].(string); ok {
			return s
		}
	}
	return ""
}

// SplitModel parses engine/name; engine must be in known.
func SplitModel(id string, known []string) (engine, name string) {
	id = strings.TrimSpace(id)
	if id == "" {
		return "", ""
	}
	for _, k := range known {
		if id == k {
			return k, ""
		}
		prefix := k + "/"
		if strings.HasPrefix(id, prefix) {
			return k, strings.TrimPrefix(id, prefix)
		}
	}
	engine, name, _ = strings.Cut(id, "/")
	return engine, name
}

func knownEngine(name string, known []string) bool {
	for _, k := range known {
		if k == name {
			return true
		}
	}
	return false
}

// EngineModel splits engine and model from a chat request. The engine field wins; a mismatched prefix drops the model name.
func EngineModel(model, engineField string, known []string) (engine, name string) {
	engine, name = SplitModel(model, known)
	if engineField == "" {
		return engine, name
	}
	if engine == engineField {
		return engine, name
	}
	if name == "" && engine != "" && !knownEngine(engine, known) {
		return engineField, engine
	}
	return engineField, ""
}

// DoneChunk is the done chunk.
func DoneChunk(id, model string) Chunk {
	stop := "stop"
	return Chunk{
		ID:      id,
		Object:  "chat.completion.chunk",
		Created: time.Now().Unix(),
		Model:   model,
		Choices: []Choice{{Index: 0, FinishReason: &stop}},
	}
}
