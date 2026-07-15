package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/erantimothy/depscan/internal/analysis"
	"github.com/erantimothy/depscan/internal/domain"
)

type Server struct {
	scanner domain.Scanner
	store   domain.Store
	fetcher domain.RepoFetcher
	logger  *slog.Logger
	mux     *http.ServeMux
}

func NewServer(scanner domain.Scanner, store domain.Store, fetcher domain.RepoFetcher, logger *slog.Logger) *Server {
	s := &Server{scanner: scanner, store: store, fetcher: fetcher, logger: logger, mux: http.NewServeMux()}
	s.routes()
	return s
}

func (s *Server) routes() {
	s.mux.HandleFunc("GET /healthz", s.handleHealth)
	s.mux.HandleFunc("POST /scans", s.handleCreateScan)
	s.mux.HandleFunc("POST /scans/remote", s.handleCreateRemoteScan)
	s.mux.HandleFunc("GET /scans", s.handleListScans)
	s.mux.HandleFunc("GET /scans/{id}", s.handleGetScan)
	s.mux.HandleFunc("GET /scans/{id}/summary", s.handleGetSummary)
	s.mux.HandleFunc("GET /scans/{id}/modules", s.handleGetModules)
	s.mux.HandleFunc("GET /scans/{id}/modules/{moduleID}", s.handleGetModule)
	s.mux.HandleFunc("GET /scans/{id}/conflicts", s.handleGetConflicts)
	s.mux.HandleFunc("GET /scans/{id}/duplicates", s.handleGetDuplicates)
	s.mux.HandleFunc("GET /scans/{id}/graph", s.handleGetGraph)
	s.mux.HandleFunc("GET /scans/{id}/changes", s.handleGetChanges)
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mux.ServeHTTP(w, r)
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

type createScanRequest struct {
	RootPath string `json:"rootPath"`
}

func (s *Server) handleCreateScan(w http.ResponseWriter, r *http.Request) {
	var req createScanRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if req.RootPath == "" {
		writeError(w, http.StatusBadRequest, "rootPath is required")
		return
	}

	// Bound how long a single scan request can run — an enterprise API
	// should never let one client's request tie up server resources
	// indefinitely. r.Context() already carries client-disconnect
	// cancellation; WithTimeout layers a server-side ceiling on top.
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	result, err := s.scanner.Scan(ctx, req.RootPath)
	if err != nil {
		if errors.Is(err, domain.ErrEmptyPath) {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		s.logger.Error("scan_failed", "error", err, "rootPath", req.RootPath)
		writeError(w, http.StatusInternalServerError, "scan failed")
		return
	}

	if err := s.store.Save(ctx, result); err != nil {
		s.logger.Error("save_failed", "error", err, "scanId", result.ID)
		writeError(w, http.StatusInternalServerError, "failed to save scan result")
		return
	}

	writeJSON(w, http.StatusCreated, result)
}

type createRemoteScanRequest struct {
	Owner string `json:"owner"`
	Repo  string `json:"repo"`
	Ref   string `json:"ref"` // optional: branch, tag, or commit SHA
}

// handleCreateRemoteScan fetches a remote GitHub repo, scans the local
// copy, saves the result, and — critically — always cleans up the
// temporary directory the fetcher created, whether the scan succeeded,
// failed, or the request was cancelled midway.
func (s *Server) handleCreateRemoteScan(w http.ResponseWriter, r *http.Request) {
	var req createRemoteScanRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if req.Owner == "" || req.Repo == "" {
		writeError(w, http.StatusBadRequest, "owner and repo are required")
		return
	}

	// A remote fetch + scan needs more headroom than a local scan: a
	// large repo's tarball download alone can take a while. This is a
	// separate, longer budget from handleCreateScan's local-only timeout.
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Minute)
	defer cancel()

	localPath, cleanup, err := s.fetcher.Fetch(ctx, domain.RepoSource{
		Owner: req.Owner,
		Repo:  req.Repo,
		Ref:   req.Ref,
	})
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			writeError(w, http.StatusNotFound, "repository or ref not found")
			return
		}
		s.logger.Error("fetch_failed", "error", err, "owner", req.Owner, "repo", req.Repo)
		writeError(w, http.StatusBadGateway, "failed to fetch repository")
		return
	}
	// defer, not a plain call at the end of the function: this must run
	// on every exit path below (early return on scan error included),
	// or every failed scan would leak a temp directory forever.
	defer cleanup()

	result, err := s.scanner.Scan(ctx, localPath)
	if err != nil {
		s.logger.Error("scan_failed", "error", err, "owner", req.Owner, "repo", req.Repo)
		writeError(w, http.StatusInternalServerError, "scan failed")
		return
	}
	// Overwrite RootPath: the caller asked about a GitHub repo, not our
	// internal temp directory — the response should reflect what they
	// actually requested, not an implementation detail they never gave us.
	result.RootPath = fmt.Sprintf("github.com/%s/%s@%s", req.Owner, req.Repo, orDefault(req.Ref, "HEAD"))

	if err := s.store.Save(ctx, result); err != nil {
		s.logger.Error("save_failed", "error", err, "scanId", result.ID)
		writeError(w, http.StatusInternalServerError, "failed to save scan result")
		return
	}

	writeJSON(w, http.StatusCreated, result)
}

func orDefault(v, fallback string) string {
	if v == "" {
		return fallback
	}
	return v
}

func (s *Server) handleGetScan(w http.ResponseWriter, r *http.Request) {
	result, ok := s.getScan(w, r)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) handleGetSummary(w http.ResponseWriter, r *http.Request) {
	result, ok := s.getScan(w, r)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, analysis.Summary(result))
}

func (s *Server) handleGetModules(w http.ResponseWriter, r *http.Request) {
	result, ok := s.getScan(w, r)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, result.Modules)
}

func (s *Server) handleGetModule(w http.ResponseWriter, r *http.Request) {
	result, ok := s.getScan(w, r)
	if !ok {
		return
	}
	moduleID := r.PathValue("moduleID")
	for _, module := range result.Modules {
		if module.ID == moduleID {
			writeJSON(w, http.StatusOK, module)
			return
		}
	}
	writeError(w, http.StatusNotFound, "module not found")
}

func (s *Server) handleGetConflicts(w http.ResponseWriter, r *http.Request) {
	result, ok := s.getScan(w, r)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, result.VersionConflicts)
}

func (s *Server) handleGetDuplicates(w http.ResponseWriter, r *http.Request) {
	result, ok := s.getScan(w, r)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, result.SharedDependencies)
}

func (s *Server) handleGetGraph(w http.ResponseWriter, r *http.Request) {
	result, ok := s.getScan(w, r)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, result.Graph)
}

func (s *Server) handleGetChanges(w http.ResponseWriter, r *http.Request) {
	result, ok := s.getScan(w, r)
	if !ok {
		return
	}
	baseID := r.URL.Query().Get("base")
	if baseID == "" {
		writeError(w, http.StatusBadRequest, "base query parameter is required")
		return
	}
	base, err := s.store.Get(r.Context(), baseID)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			writeError(w, http.StatusNotFound, "base scan not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to fetch base scan")
		return
	}
	writeJSON(w, http.StatusOK, analysis.Compare(base, result))
}

func (s *Server) getScan(w http.ResponseWriter, r *http.Request) (domain.ScanResult, bool) {
	result, err := s.store.Get(r.Context(), r.PathValue("id"))
	if err == nil {
		return result, true
	}
	if errors.Is(err, domain.ErrNotFound) {
		writeError(w, http.StatusNotFound, "scan not found")
	} else {
		writeError(w, http.StatusInternalServerError, "failed to fetch scan")
	}
	return domain.ScanResult{}, false
}

func (s *Server) handleListScans(w http.ResponseWriter, r *http.Request) {
	results, err := s.store.List(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list scans")
		return
	}
	writeJSON(w, http.StatusOK, results)
}

// writeJSON and writeError are the two shared response paths every
// handler funnels through. Centralizing this means the JSON content-type
// header and error shape are consistent across the whole API rather than
// hand-rolled slightly differently in each handler.
func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

type errorResponse struct {
	Error string `json:"error"`
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, errorResponse{Error: msg})
}
