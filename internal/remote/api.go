package remote

import (
	"net/http"
	"strconv"
	"strings"
)

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request, _ session) {
	status, err := s.host.Status(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, status)
}

func (s *Server) handleGroups(w http.ResponseWriter, r *http.Request, _ session) {
	groups, err := s.host.Groups(r.Context())
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"groups": groups})
}

func (s *Server) handleSelect(w http.ResponseWriter, r *http.Request, _ session) {
	var req struct {
		Group  string `json:"group"`
		Member string `json:"member"`
	}
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	req.Group, req.Member = strings.TrimSpace(req.Group), strings.TrimSpace(req.Member)
	if req.Group == "" || req.Member == "" || len(req.Group) > 256 || len(req.Member) > 256 {
		writeError(w, http.StatusBadRequest, "group and member are required")
		return
	}
	if err := s.host.Select(r.Context(), req.Group, req.Member); err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	s.audit(r, "select "+req.Group+" -> "+req.Member)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleMode(w http.ResponseWriter, r *http.Request, _ session) {
	var req struct {
		Mode string `json:"mode"`
	}
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	req.Mode = strings.ToLower(strings.TrimSpace(req.Mode))
	if !validMode(req.Mode) {
		writeError(w, http.StatusBadRequest, "invalid mode")
		return
	}
	if err := s.host.SetMode(r.Context(), req.Mode); err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	s.audit(r, "mode "+req.Mode)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleSystemProxy(w http.ResponseWriter, r *http.Request, _ session) {
	var req struct {
		On *bool `json:"on"`
	}
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.On == nil {
		writeError(w, http.StatusBadRequest, "on is required")
		return
	}
	if err := s.host.SetSystemProxy(r.Context(), *req.On); err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	s.audit(r, "system proxy "+strconv.FormatBool(*req.On))
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleTun(w http.ResponseWriter, r *http.Request, _ session) {
	var req struct {
		On *bool `json:"on"`
	}
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.On == nil {
		writeError(w, http.StatusBadRequest, "on is required")
		return
	}
	if err := s.host.SetTun(r.Context(), *req.On); err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	s.audit(r, "tun "+strconv.FormatBool(*req.On))
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleConnections(w http.ResponseWriter, r *http.Request, _ session) {
	conns, err := s.host.Connections(r.Context())
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	limit := 500
	if raw := r.URL.Query().Get("limit"); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 && n < limit {
			limit = n
		}
	}
	total := len(conns)
	if len(conns) > limit {
		conns = conns[:limit]
	}
	writeJSON(w, http.StatusOK, map[string]any{"connections": conns, "total": total})
}

func (s *Server) handleCloseConnection(w http.ResponseWriter, r *http.Request, _ session) {
	id := strings.TrimSpace(r.PathValue("id"))
	if id == "" || len(id) > 512 {
		writeError(w, http.StatusBadRequest, "connection id is required")
		return
	}
	if err := s.host.CloseConnection(r.Context(), id); err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	s.audit(r, "close connection "+id)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleCloseAll(w http.ResponseWriter, r *http.Request, _ session) {
	if err := s.host.CloseAllConnections(r.Context()); err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	s.audit(r, "close all connections")
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleLogs(w http.ResponseWriter, r *http.Request, _ session) {
	logs, err := s.host.Logs(r.Context())
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	level := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("level")))
	min := 0
	switch level {
	case "info":
		min = 1
	case "warning":
		min = 2
	case "error":
		min = 3
	case "", "all":
	default:
		writeError(w, http.StatusBadRequest, "invalid level")
		return
	}
	filtered := make([]LogEntry, 0, len(logs))
	for _, entry := range logs {
		if logRank(entry.Level) >= min {
			filtered = append(filtered, entry)
		}
	}
	limit := 300
	if raw := r.URL.Query().Get("limit"); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 && n < limit {
			limit = n
		}
	}
	if len(filtered) > limit {
		filtered = filtered[len(filtered)-limit:]
	}
	writeJSON(w, http.StatusOK, map[string]any{"logs": filtered})
}

func (s *Server) handleClearLogs(w http.ResponseWriter, r *http.Request, _ session) {
	if err := s.host.ClearLogs(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.audit(r, "clear logs")
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleDelay(w http.ResponseWriter, r *http.Request, _ session) {
	var req struct {
		Group string   `json:"group"`
		Names []string `json:"names"`
	}
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	req.Group = strings.TrimSpace(req.Group)
	if len(req.Names) > 100 {
		writeError(w, http.StatusBadRequest, "too many nodes")
		return
	}
	for i := range req.Names {
		req.Names[i] = strings.TrimSpace(req.Names[i])
		if req.Names[i] == "" || len(req.Names[i]) > 256 {
			writeError(w, http.StatusBadRequest, "invalid node name")
			return
		}
	}
	if req.Group == "" && len(req.Names) == 0 {
		writeError(w, http.StatusBadRequest, "group or names is required")
		return
	}
	results, err := s.host.Delay(r.Context(), req.Group, req.Names)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	s.audit(r, "delay test")
	writeJSON(w, http.StatusOK, map[string]any{"results": results})
}

func logRank(level string) int {
	switch strings.ToLower(level) {
	case "error":
		return 3
	case "warning", "warn":
		return 2
	case "info":
		return 1
	default:
		return 0
	}
}
