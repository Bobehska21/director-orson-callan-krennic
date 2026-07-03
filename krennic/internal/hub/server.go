package hub

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/acme/krennic/internal/audit"
)

// Server is the central hub HTTP service.
type Server struct {
	store *Store
	token string // shared bearer token; empty = auth disabled (dev only)
	log   *slog.Logger
}

// NewServer builds a hub server. token is the expected bearer token.
func NewServer(store *Store, token string, log *slog.Logger) *Server {
	return &Server{store: store, token: token, log: log}
}

// Handler returns the HTTP handler (dashboard + API).
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/report", s.handleReport)
	mux.HandleFunc("/api/feed", s.handleFeed)
	mux.HandleFunc("/api/verify", s.handleVerify)
	mux.HandleFunc("/api/stats", s.handleStats)
	mux.HandleFunc("/", s.handleDashboard)
	return mux
}

// authed checks the bearer token for write/read API calls.
func (s *Server) authed(r *http.Request) bool {
	if s.token == "" {
		return true // auth disabled
	}
	h := r.Header.Get("Authorization")
	return strings.TrimPrefix(h, "Bearer ") == s.token
}

func (s *Server) handleReport(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return
	}
	if !s.authed(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	var rep audit.Report
	if err := json.NewDecoder(r.Body).Decode(&rep); err != nil {
		http.Error(w, "bad json: "+err.Error(), http.StatusBadRequest)
		return
	}
	if rep.ReportID == "" || rep.ChangeID == "" {
		http.Error(w, "missing report_id/change_id", http.StatusBadRequest)
		return
	}
	seq, hash, err := s.store.Append(rep)
	if err != nil {
		s.log.Warn("append failed", "err", err)
		http.Error(w, "store error", http.StatusInternalServerError)
		return
	}
	s.log.Info("report", "seq", seq, "user", rep.Developer.UserSlug, "repo", rep.Repo,
		"branch", rep.Branch, "files", len(rep.Files), "verdict", rep.Verdict)
	writeJSON(w, map[string]any{"seq": seq, "entry_hash": hash})
}

func (s *Server) handleFeed(w http.ResponseWriter, r *http.Request) {
	if !s.authed(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	limit := 100
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			limit = n
		}
	}
	entries, err := s.store.Feed(limit, r.URL.Query().Get("user"), r.URL.Query().Get("repo"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, entries)
}

func (s *Server) handleVerify(w http.ResponseWriter, r *http.Request) {
	if !s.authed(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	res, err := s.store.Verify()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, res)
}

func (s *Server) handleStats(w http.ResponseWriter, r *http.Request) {
	st, _ := s.store.Stats()
	writeJSON(w, st)
}

func (s *Server) handleDashboard(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("content-type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(dashboardHTML))
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("content-type", "application/json")
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(v)
}

// ListenAndServe starts the hub server with sensible timeouts.
func (s *Server) ListenAndServe(addr string) error {
	srv := &http.Server{
		Addr:              addr,
		Handler:           s.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}
	return srv.ListenAndServe()
}
