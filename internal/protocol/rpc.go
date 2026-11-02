// Package protocol defines RPC and streaming events between client, relay, and node.
//
// Trust boundary:
//   - the relay only routes Envelope From/To and cannot decrypt Ciphertext
//   - clients may talk to a node directly or through the relay; application encryption is the same
package protocol

import "encoding/json"

const (
	MethodList      = "list"
	MethodStatus    = "status"
	MethodHistory   = "history"
	MethodWatch     = "watch"
	MethodReply     = "reply"
	MethodSend      = "send"
	MethodHealth    = "health"
	MethodInfo      = "info"
	MethodEngines   = "engines"
	MethodModels    = "models"
	MethodCommands  = "commands"
	MethodFollowups = "followups"
	MethodChoice    = "choice"
)

// Request is an RPC request before encryption.
type Request struct {
	ID     string          `json:"id"`
	Method string          `json:"method"`
	Stream bool            `json:"stream,omitempty"`
	Params json.RawMessage `json:"params,omitempty"`
}

// Response is an RPC response before encryption.
type Response struct {
	ID    string          `json:"id"`
	OK    bool            `json:"ok"`
	Error string          `json:"error,omitempty"`
	Body  json.RawMessage `json:"body,omitempty"`
}

// Event is streaming progress.
type Event struct {
	Type    string `json:"type"`
	Role    string `json:"role,omitempty"`
	Tool    string `json:"tool,omitempty"`
	Engine  string `json:"engine,omitempty"`
	Session string `json:"session,omitempty"`
	Text    string `json:"text,omitempty"`
	Preview string `json:"preview,omitempty"`
	Unix    int64  `json:"unix,omitempty"`
	Done    bool   `json:"done,omitempty"`
}

// WorkDir is a workspace an engine has used.
type WorkDir struct {
	Path   string `json:"path"`
	Engine string `json:"engine,omitempty"`
	Count  int    `json:"count,omitempty"`
}

// Session is the shared session view.
type Session struct {
	Engine    string `json:"engine"`
	ID        string `json:"id"`
	Title     string `json:"title,omitempty"`
	CWD       string `json:"cwd,omitempty"`
	Status    string `json:"status,omitempty"`
	Kind      string `json:"kind,omitempty"`
	PID       int    `json:"pid,omitempty"`
	Live      bool   `json:"live"`
	Busy      bool   `json:"busy"`
	UpdatedAt int64  `json:"updated_at,omitempty"`
	JSONL     string `json:"jsonl,omitempty"`
	Extra     string `json:"extra,omitempty"`
	NodeID    string `json:"node_id,omitempty"`
	NodeName  string `json:"node_name,omitempty"`
}

// EngineInfo is the status of one local coding-agent engine.
type EngineInfo struct {
	Name      string `json:"name"`
	Title     string `json:"title,omitempty"`
	Enabled   bool   `json:"enabled"`
	Available bool   `json:"available"`
	Binary    string `json:"binary,omitempty"`
	Version   string `json:"version,omitempty"`
	Detail    string `json:"detail,omitempty"`
}

// Model is a model the engine can use.
type Model struct {
	ID      string `json:"id"`
	Engine  string `json:"engine"`
	Name    string `json:"name"`
	Title   string `json:"title,omitempty"`
	Default bool   `json:"default,omitempty"`
}

// SlashItem is a slash command or skill the engine can invoke.
type SlashItem struct {
	Name         string `json:"name"`
	Kind         string `json:"kind"`
	Description  string `json:"description,omitempty"`
	ArgumentHint string `json:"argument_hint,omitempty"`
	Source       string `json:"source,omitempty"`
}

// SlashList is a slash catalog.
type SlashList struct {
	Items []SlashItem `json:"items"`
}

// ModelsParams lists models.
type ModelsParams struct {
	Engine string `json:"engine,omitempty"`
}

// EngineToggleParams toggles an engine.
type EngineToggleParams struct {
	Engine  string `json:"engine"`
	Enabled bool   `json:"enabled"`
}

// ListParams lists tasks.
type ListParams struct {
	Engine string `json:"engine,omitempty"`
	All    bool   `json:"all,omitempty"`
}

// HistoryParams is a history summary.
type HistoryParams struct {
	Engine    string `json:"engine"`
	SessionID string `json:"session_id"`
	Tail      int    `json:"tail,omitempty"`
}

// ReplyParams appends to a session.
type ReplyParams struct {
	Engine    string `json:"engine"`
	SessionID string `json:"session_id"`
	Text      string `json:"text"`
	Model     string `json:"model,omitempty"`
	Watch     bool   `json:"watch,omitempty"`
}

// SendParams starts a new turn.
type SendParams struct {
	Engine string `json:"engine"`
	CWD    string `json:"cwd,omitempty"`
	Text   string `json:"text"`
	Resume string `json:"resume,omitempty"`
	Model  string `json:"model,omitempty"`
}

// WatchParams follows progress.
type WatchParams struct {
	Engine    string `json:"engine"`
	SessionID string `json:"session_id"`
}

// CommandsParams lists slash commands.
type CommandsParams struct {
	Engine string `json:"engine,omitempty"`
	CWD    string `json:"cwd,omitempty"`
}

// FollowupsParams is the follow-up queue.
type FollowupsParams struct {
	Action  string `json:"action,omitempty"`
	Engine  string `json:"engine,omitempty"`
	Session string `json:"session,omitempty"`
	CWD     string `json:"cwd,omitempty"`
	ID      string `json:"id,omitempty"`
	Text    string `json:"text,omitempty"`
}

// ChoiceParams answers an Ask / permission card.
type ChoiceParams struct {
	Engine  string `json:"engine"`
	Session string `json:"session,omitempty"`
	CWD     string `json:"cwd,omitempty"`
	Text    string `json:"text"`
}

// NodeInfo is public node info (no private key).
type NodeInfo struct {
	ID       string   `json:"id"`
	Name     string   `json:"name,omitempty"`
	Listen   string   `json:"listen,omitempty"`
	Engines  []string `json:"engines"`
	SignPub  []byte   `json:"sign_pub"`
	X25519   []byte   `json:"x25519_pub"`
	LastSeen int64    `json:"last_seen,omitempty"`
}

// RegisterRequest is a node registration. The signature covers ID+Listen+Unix.
type RegisterRequest struct {
	Info      NodeInfo     `json:"info"`
	Unix      int64        `json:"unix"`
	Signature []byte       `json:"signature"`
	Snapshot  NodeSnapshot `json:"snapshot,omitempty"`
}

// FollowItem is a queued follow-up.
type FollowItem struct {
	ID      string `json:"id"`
	Text    string `json:"text"`
	Created int64  `json:"created"`
}

// FollowQueue is a snapshot of one session's follow-up queue.
type FollowQueue struct {
	Engine  string       `json:"engine"`
	Session string       `json:"session"`
	CWD     string       `json:"cwd,omitempty"`
	Items   []FollowItem `json:"items"`
	NodeID  string       `json:"node_id,omitempty"`
}

// NodeSnapshot is a read-only snapshot a node pushes to the relay, for public relays that cannot call back.
type NodeSnapshot struct {
	Models    []Model       `json:"models"`
	Engines   []EngineInfo  `json:"engines"`
	Sessions  []Session     `json:"sessions"`
	Dirs      []WorkDir     `json:"directories"`
	Followups []FollowQueue `json:"followups"`
}

// TunnelFrame is a request/response frame on the node reverse tunnel.
type TunnelFrame struct {
	ID     string            `json:"id,omitempty"`
	Method string            `json:"method,omitempty"`
	Path   string            `json:"path,omitempty"`
	Body   json.RawMessage   `json:"body,omitempty"`
	Raw    []byte            `json:"raw,omitempty"` // opaque binary request body (e2e envelopes)
	Status int               `json:"status,omitempty"`
	Header map[string]string `json:"header,omitempty"`
	Chunk  []byte            `json:"chunk,omitempty"`
	EOF    bool              `json:"eof,omitempty"`
	Error  string            `json:"error,omitempty"`
}

// MustJSON marshals JSON, or an empty object on failure.
func MustJSON(v any) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		return json.RawMessage(`{}`)
	}
	return b
}
