// Package web 提供“化石带议庭”本地 HTTP 服务与操作页面。
package web

import (
	"encoding/json"
	"fmt"
	"net/http"

	"fossilcourt/internal/engine"
	"fossilcourt/internal/exchange"
	"fossilcourt/internal/store"
)

type Server struct {
	db  *store.DB
	svc *engine.Service
	mux *http.ServeMux
}

func NewServer(db *store.DB, svc *engine.Service) *Server {
	s := &Server{db: db, svc: svc, mux: http.NewServeMux()}
	s.routes()
	return s
}

func (s *Server) Handler() http.Handler { return s.mux }

func (s *Server) routes() {
	s.mux.HandleFunc("GET /", s.index)
	s.mux.HandleFunc("GET /api/state", s.handleState)
	s.mux.HandleFunc("POST /api/syn/merge", s.handleMerge)
	s.mux.HandleFunc("POST /api/syn/split", s.handleSplit)
	s.mux.HandleFunc("POST /api/rework", s.handleRework)
	s.mux.HandleFunc("POST /api/ties", s.handleAddTie)
	s.mux.HandleFunc("POST /api/ties/{id}/deactivate", s.handleDeactivateTie)
	s.mux.HandleFunc("GET /api/export", s.handleExport)
	s.mux.HandleFunc("POST /api/import", s.handleImport)
	s.mux.HandleFunc("POST /api/reset", s.handleReset)
	s.mux.HandleFunc("GET /svg/sections", s.handleSVG)
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, code int, err error) {
	writeJSON(w, code, map[string]any{"error": err.Error()})
}

func (s *Server) recordRun(kind string, payload any) {
	b, _ := json.Marshal(payload)
	_, _ = s.db.Sql.Exec(`INSERT INTO runs(kind,payload) VALUES(?,?)`, kind, string(b))
}

func (s *Server) handleState(w http.ResponseWriter, r *http.Request) {
	st, err := s.svc.State()
	if err != nil {
		writeErr(w, 500, err)
		return
	}
	writeJSON(w, 200, st)
}

type mergeReq struct {
	GroupID     string   `json:"groupId"`
	DisplayName string   `json:"displayName"`
	Members     []string `json:"members"`
	Note        string   `json:"note"`
}

func (s *Server) handleMerge(w http.ResponseWriter, r *http.Request) {
	var req mergeReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, 400, err)
		return
	}
	if err := s.svc.MergeTaxa(req.GroupID, req.DisplayName, req.Members, req.Note); err != nil {
		writeErr(w, 400, err)
		return
	}
	s.recordRun("syn_merge", req)
	s.respondState(w)
}

type splitReq struct {
	GroupID string `json:"groupId"`
	Note    string `json:"note"`
}

func (s *Server) handleSplit(w http.ResponseWriter, r *http.Request) {
	var req splitReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, 400, err)
		return
	}
	if err := s.svc.SplitTaxa(req.GroupID, req.Note); err != nil {
		writeErr(w, 400, err)
		return
	}
	s.recordRun("syn_split", req)
	s.respondState(w)
}

type reworkReq struct {
	OccurrenceID string `json:"occurrenceId"`
	Marked       bool   `json:"marked"`
	Note         string `json:"note"`
}

func (s *Server) handleRework(w http.ResponseWriter, r *http.Request) {
	var req reworkReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, 400, err)
		return
	}
	if err := s.svc.SetRework(req.OccurrenceID, req.Marked, req.Note); err != nil {
		writeErr(w, 400, err)
		return
	}
	s.recordRun("rework", req)
	s.respondState(w)
}

type tieReq struct {
	AGroup   string `json:"aGroup"`
	ASection string `json:"aSection"`
	AKind    string `json:"aKind"`
	BGroup   string `json:"bGroup"`
	BSection string `json:"bSection"`
	BKind    string `json:"bKind"`
	Note     string `json:"note"`
}

func (s *Server) handleAddTie(w http.ResponseWriter, r *http.Request) {
	var req tieReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, 400, err)
		return
	}
	id, chain, ids, err := s.svc.AddTie(engine.TieInput{
		AGroup: req.AGroup, ASection: req.ASection, AKind: req.AKind,
		BGroup: req.BGroup, BSection: req.BSection, BKind: req.BKind, Note: req.Note,
	})
	if err != nil {
		writeErr(w, 400, err)
		return
	}
	s.recordRun("add_tie", req)
	if len(chain) > 0 {
		writeJSON(w, 409, map[string]any{
			"conflict":      true,
			"tieId":         id,
			"conflictChain": chain,
			"tieIds":        ids,
			"message":       fmt.Sprintf("锦标已保留但成环；冲突锦标 %v，系统不会自动删除", ids),
		})
		return
	}
	s.respondState(w)
}

func (s *Server) handleDeactivateTie(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := s.svc.DeactivateTie(id); err != nil {
		writeErr(w, 400, err)
		return
	}
	s.recordRun("deactivate_tie", map[string]string{"id": id})
	s.respondState(w)
}

func (s *Server) handleExport(w http.ResponseWriter, r *http.Request) {
	b, err := exchange.Export(s.db)
	if err != nil {
		writeErr(w, 500, err)
		return
	}
	s.recordRun("export", map[string]string{"format": "json"})
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="fossil-court-run.json"`)
	_ = json.NewEncoder(w).Encode(b)
}

func (s *Server) handleImport(w http.ResponseWriter, r *http.Request) {
	var b exchange.Bundle
	if err := json.NewDecoder(r.Body).Decode(&b); err != nil {
		writeErr(w, 400, err)
		return
	}
	if err := exchange.Import(s.db, &b); err != nil {
		writeErr(w, 400, err)
		return
	}
	s.recordRun("import", map[string]int{"formatVersion": b.FormatVersion})
	s.respondState(w)
}

func (s *Server) handleReset(w http.ResponseWriter, r *http.Request) {
	if err := s.svc.Seed(); err != nil {
		writeErr(w, 500, err)
		return
	}
	s.recordRun("reset", map[string]string{"to": "fixture"})
	s.respondState(w)
}

func (s *Server) respondState(w http.ResponseWriter) {
	st, err := s.svc.State()
	if err != nil {
		writeErr(w, 500, err)
		return
	}
	writeJSON(w, 200, st)
}
