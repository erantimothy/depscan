package scanner

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/erantimothy/depscan/internal/domain"
)

type fakeParser struct {
	moduleFor func(content []byte) domain.Module
	err       error
}

func (f *fakeParser) Parse(content []byte) (domain.Module, error) {
	if f.err != nil {
		return domain.Module{}, f.err
	}
	return f.moduleFor(content), nil
}

func writeTestTree(t *testing.T) string {
	t.Helper()

	root := t.TempDir()

	paths := []string{
		"go.mod",
		"pkg/sub/go.mod",
		"vendor/somelib/go.mod",
	}
	for _, p := range paths {
		full := filepath.Join(root, p)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("MkdirAll: %v", err)
		}
		content := "module example.com/" + filepath.Dir(p) + "\ngo 1.22\n"
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}
	}
	return root
}

func TestScanner_Scan_FindsModulesAndSkipsVendor(t *testing.T) {
	root := writeTestTree(t)

	parser := &fakeParser{
		moduleFor: func(content []byte) domain.Module {
			return domain.Module{Path: "found"}
		},
	}
	s := New(parser, WithMaxWorkers(2))

	result, err := s.Scan(context.Background(), root)
	if err != nil {
		t.Fatalf("Scan() unexpected error: %v", err)
	}

	if len(result.Modules) != 2 {
		t.Fatalf("got %d modules, want 2 (vendor should be skipped): %+v", len(result.Modules), result.Modules)
	}
	if result.RootPath != root {
		t.Errorf("RootPath = %q, want %q", result.RootPath, root)
	}
	if result.ID == "" {
		t.Error("expected a non-empty scan ID")
	}
}

func TestScanner_Scan_EmptyPathIsError(t *testing.T) {
	s := New(&fakeParser{})
	_, err := s.Scan(context.Background(), "")
	if err != domain.ErrEmptyPath {
		t.Fatalf("got error %v, want %v", err, domain.ErrEmptyPath)
	}
}

func TestScanner_Scan_RespectsCancellation(t *testing.T) {
	root := writeTestTree(t)
	parser := &fakeParser{moduleFor: func([]byte) domain.Module { return domain.Module{} }}
	s := New(parser, WithMaxWorkers(1))

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := s.Scan(ctx, root)
	if err == nil {
		t.Fatal("expected an error from an already-cancelled context, got nil")
	}
}

func TestScanner_Scan_NoRaceUnderConcurrency(t *testing.T) {
	root := writeTestTree(t)
	parser := &fakeParser{moduleFor: func([]byte) domain.Module { return domain.Module{Path: "x"} }}
	s := New(parser, WithMaxWorkers(4))

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if _, err := s.Scan(ctx, root); err != nil {
		t.Fatalf("Scan() error: %v", err)
	}
}
