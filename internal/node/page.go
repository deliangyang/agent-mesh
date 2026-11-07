package node

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"strconv"

	"agent-mesh/internal/protocol"
)

func (s *Server) listSessions(ctx context.Context, p protocol.ListParams) (any, error) {
	rows, err := s.collectSessions(ctx, p)
	if err != nil {
		return nil, fmt.Errorf("list: %w", err)
	}
	return pageSessions(rows, p), nil
}

func (s *Server) collectSessions(ctx context.Context, p protocol.ListParams) ([]protocol.Session, error) {
	if snap := s.peekSnapshot(); len(snap.Sessions) > 0 {
		_, _, _, _, sessions, _ := s.webCatalog()
		return sessions, nil
	}
	rows, err := s.Drivers.ListAll(ctx, p.Engine, p.All || p.Limit > 0)
	if err != nil {
		return nil, err
	}
	if rows == nil {
		rows = []protocol.Session{}
	}
	for i := range rows {
		if rows[i].NodeID == "" {
			rows[i].NodeID = s.Ident.ID
			rows[i].NodeName = s.Name
		}
	}
	return rows, nil
}

// pageSessions returns []Session when Limit is 0, and a SessionPage otherwise.
func pageSessions(rows []protocol.Session, p protocol.ListParams) any {
	filtered := make([]protocol.Session, 0, len(rows))
	for _, sess := range rows {
		if p.Engine != "" && sess.Engine != p.Engine {
			continue
		}
		if p.Limit <= 0 && !p.All && !sess.Live && !sess.Busy {
			continue
		}
		filtered = append(filtered, sess)
	}
	if p.Limit <= 0 {
		if filtered == nil {
			filtered = []protocol.Session{}
		}
		return filtered
	}
	sort.SliceStable(filtered, func(i, j int) bool {
		if filtered[i].UpdatedAt != filtered[j].UpdatedAt {
			return filtered[i].UpdatedAt > filtered[j].UpdatedAt
		}
		if filtered[i].ID != filtered[j].ID {
			return filtered[i].ID > filtered[j].ID
		}
		return filtered[i].Engine > filtered[j].Engine
	})
	start := 0
	if p.Before != 0 || p.BeforeID != "" {
		start = len(filtered)
		for i, sess := range filtered {
			if sessionOlder(sess, p.Before, p.BeforeID) {
				start = i
				break
			}
		}
	}
	end := start + p.Limit
	more := end < len(filtered)
	if end > len(filtered) {
		end = len(filtered)
	}
	page := append([]protocol.Session(nil), filtered[start:end]...)
	if page == nil {
		page = []protocol.Session{}
	}
	return protocol.SessionPage{Sessions: page, More: more}
}

// sessionOlder reports whether s sorts strictly after the exclusive newest-first cursor.
func sessionOlder(s protocol.Session, before int64, beforeID string) bool {
	if s.UpdatedAt != before {
		return s.UpdatedAt < before
	}
	return s.ID < beforeID
}

func (s *Server) handleSessions(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	limit, _ := strconv.Atoi(q.Get("limit"))
	before, _ := strconv.ParseInt(q.Get("before"), 10, 64)
	body, err := s.listSessions(r.Context(), protocol.ListParams{
		Engine:   q.Get("engine"),
		All:      q.Get("all") == "1" || q.Get("all") == "true",
		Limit:    limit,
		Before:   before,
		BeforeID: q.Get("before_id"),
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	writeJSON(w, http.StatusOK, body)
}
