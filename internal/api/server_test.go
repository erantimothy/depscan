package api

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/erantimothy/depscan/internal/analysis"
	"github.com/erantimothy/depscan/internal/domain"
	"github.com/erantimothy/depscan/internal/store"
)

func TestSummaryAndChangesEndpoints(t *testing.T) {
	repository := store.NewMemoryStore()
	base := domain.ScanResult{ID: "base", Modules: []domain.Module{{Path: "example.com/app", Dependencies: []domain.Dependency{{Path: "example.com/lib", Version: "v1.9.0"}}}}}
	current := domain.ScanResult{ID: "current", Modules: []domain.Module{{Path: "example.com/app", Dependencies: []domain.Dependency{{Path: "example.com/lib", Version: "v1.10.0"}}}}}
	analysis.Enrich(&base)
	analysis.Enrich(&current)
	if err := repository.Save(context.Background(), base); err != nil {
		t.Fatal(err)
	}
	if err := repository.Save(context.Background(), current); err != nil {
		t.Fatal(err)
	}

	server := NewServer(nil, repository, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))

	for _, request := range []struct {
		path   string
		status int
	}{
		{"/scans/current/summary", http.StatusOK},
		{"/scans/current/modules", http.StatusOK},
		{"/scans/current/changes?base=base", http.StatusOK},
		{"/scans/current/changes", http.StatusBadRequest},
	} {
		recorder := httptest.NewRecorder()
		server.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, request.path, nil))
		if recorder.Code != request.status {
			t.Errorf("GET %s = %d, want %d", request.path, recorder.Code, request.status)
		}
	}
}
