// Package protocol 定义客户端、中继、节点之间的 RPC 与流式事件。
//
// 信任边界：
//   - 中继只路由 Envelope 的 From/To，不能解密 Ciphertext
//   - 客户端可直连节点，也可经中继转发，应用层加密相同
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

// Request 加密前的 RPC 请求。
type Request struct {
	ID     string          `json:"id"`
	Method string          `json:"method"`
	Stream bool            `json:"stream,omitempty"`
	Params json.RawMessage `json:"params,omitempty"`
}

// Response 加密前的 RPC 响应。
type Response struct {
	ID    string          `json:"id"`
	OK    bool            `json:"ok"`
	Error string          `json:"error,omitempty"`
	Body  json.RawMessage `json:"body,omitempty"`
}

// Event 流式进度。
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

// WorkDir 引擎用过的开发目录。
type WorkDir struct {
	Path   string `json:"path"`
	Engine string `json:"engine,omitempty"`
	Count  int    `json:"count,omitempty"`
}

// Session 统一会话视图。
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

// EngineInfo 本机一种 coding agent 引擎的状态。
type EngineInfo struct {
	Name      string `json:"name"`
	Title     string `json:"title,omitempty"`
	Enabled   bool   `json:"enabled"`
	Available bool   `json:"available"`
	Binary    string `json:"binary,omitempty"`
	Version   string `json:"version,omitempty"`
	Detail    string `json:"detail,omitempty"`
}

// Model 引擎可用模型。
type Model struct {
	ID      string `json:"id"`
	Engine  string `json:"engine"`
	Name    string `json:"name"`
	Title   string `json:"title,omitempty"`
	Default bool   `json:"default,omitempty"`
}

// SlashItem 引擎可调用的斜杠命令或技能。
type SlashItem struct {
	Name         string `json:"name"`
	Kind         string `json:"kind"`
	Description  string `json:"description,omitempty"`
	ArgumentHint string `json:"argument_hint,omitempty"`
	Source       string `json:"source,omitempty"`
}

// SlashList 斜杠目录。
type SlashList struct {
	Items []SlashItem `json:"items"`
}

// ModelsParams 查询模型列表。
type ModelsParams struct {
	Engine string `json:"engine,omitempty"`
}

// EngineToggleParams 开关引擎。
type EngineToggleParams struct {
	Engine  string `json:"engine"`
	Enabled bool   `json:"enabled"`
}

// ListParams 列出任务。
type ListParams struct {
	Engine string `json:"engine,omitempty"`
	All    bool   `json:"all,omitempty"`
}

// HistoryParams 历史摘要。
type HistoryParams struct {
	Engine    string `json:"engine"`
	SessionID string `json:"session_id"`
	Tail      int    `json:"tail,omitempty"`
}

// ReplyParams 追加对话。
type ReplyParams struct {
	Engine    string `json:"engine"`
	SessionID string `json:"session_id"`
	Text      string `json:"text"`
	Watch     bool   `json:"watch,omitempty"`
}

// SendParams 新开一轮。
type SendParams struct {
	Engine string `json:"engine"`
	CWD    string `json:"cwd,omitempty"`
	Text   string `json:"text"`
	Resume string `json:"resume,omitempty"`
}

// WatchParams 跟踪进度。
type WatchParams struct {
	Engine    string `json:"engine"`
	SessionID string `json:"session_id"`
}

// CommandsParams 查询斜杠命令。
type CommandsParams struct {
	Engine string `json:"engine,omitempty"`
	CWD    string `json:"cwd,omitempty"`
}

// FollowupsParams 追加队列。
type FollowupsParams struct {
	Action  string `json:"action,omitempty"`
	Engine  string `json:"engine,omitempty"`
	Session string `json:"session,omitempty"`
	CWD     string `json:"cwd,omitempty"`
	ID      string `json:"id,omitempty"`
	Text    string `json:"text,omitempty"`
}

// ChoiceParams 回答 Ask / 权限卡。
type ChoiceParams struct {
	Engine  string `json:"engine"`
	Session string `json:"session,omitempty"`
	CWD     string `json:"cwd,omitempty"`
	Text    string `json:"text"`
}

// NodeInfo 节点公开信息（不含私钥）。
type NodeInfo struct {
	ID       string   `json:"id"`
	Name     string   `json:"name,omitempty"`
	Listen   string   `json:"listen,omitempty"`
	Engines  []string `json:"engines"`
	SignPub  []byte   `json:"sign_pub"`
	X25519   []byte   `json:"x25519_pub"`
	LastSeen int64    `json:"last_seen,omitempty"`
}

// RegisterRequest 节点向中继登记。签名覆盖 ID+Listen+Unix。
type RegisterRequest struct {
	Info      NodeInfo     `json:"info"`
	Unix      int64        `json:"unix"`
	Signature []byte       `json:"signature"`
	Snapshot  NodeSnapshot `json:"snapshot,omitempty"`
}

// FollowItem 一条等待处理的追加消息。
type FollowItem struct {
	ID      string `json:"id"`
	Text    string `json:"text"`
	Created int64  `json:"created"`
}

// FollowQueue 一个会话的追加队列快照。
type FollowQueue struct {
	Engine  string       `json:"engine"`
	Session string       `json:"session"`
	CWD     string       `json:"cwd,omitempty"`
	Items   []FollowItem `json:"items"`
	NodeID  string       `json:"node_id,omitempty"`
}

// NodeSnapshot 节点推给中继的只读快照，供公网中继在无法回连时展示。
type NodeSnapshot struct {
	Models    []Model       `json:"models"`
	Engines   []EngineInfo  `json:"engines"`
	Sessions  []Session     `json:"sessions"`
	Dirs      []WorkDir     `json:"directories"`
	Followups []FollowQueue `json:"followups"`
}

// TunnelFrame 节点反向隧道上的请求/响应帧。
type TunnelFrame struct {
	ID     string            `json:"id,omitempty"`
	Method string            `json:"method,omitempty"`
	Path   string            `json:"path,omitempty"`
	Body   json.RawMessage   `json:"body,omitempty"`
	Status int               `json:"status,omitempty"`
	Header map[string]string `json:"header,omitempty"`
	Chunk  []byte            `json:"chunk,omitempty"`
	EOF    bool              `json:"eof,omitempty"`
	Error  string            `json:"error,omitempty"`
}

// MustJSON 序列化，失败时返回空对象。
func MustJSON(v any) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		return json.RawMessage(`{}`)
	}
	return b
}
