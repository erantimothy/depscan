// Package scanner walks a directory tree, finds every go.mod file, and parses them concurrently using a bounded worker pool.
package scanner

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/erantimothy/depscan/internal/analysis"
	"github.com/erantimothy/depscan/internal/domain"
)

type Scanner struct {
	parser     domain.Parser
	maxWorkers int
}

type Option func(*Scanner)

func WithMaxWorkers(n int) Option {
	return func(s *Scanner) {
		if n > 0 {
			s.maxWorkers = n
		}
	}
}

func New(parser domain.Parser, opts ...Option) *Scanner {
	s := &Scanner{parser: parser, maxWorkers: 8}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

type job struct {
	path string
}

type result struct {
	module domain.Module
	err    error
}

func (s *Scanner) Scan(ctx context.Context, rootPath string) (domain.ScanResult, error) {
	if rootPath == "" {
		return domain.ScanResult{}, domain.ErrEmptyPath
	}
	start := time.Now()

	res := domain.ScanResult{
		ID:        newScanID(),
		RootPath:  rootPath,
		StartedAt: start,
	}
	var paths []string
	err := filepath.WalkDir(rootPath, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if d.IsDir() && (d.Name() == "vendor" || d.Name() == ".git") {
			return filepath.SkipDir
		}
		if !d.IsDir() && d.Name() == "go.mod" {
			paths = append(paths, path)
		}
		return nil
	})
	if err != nil {
		return res, fmt.Errorf("walking %s: %w", rootPath, err)
	}
	if len(paths) == 0 {
		return res, domain.ErrNoModules
	}
	sort.Strings(paths)

	jobs := make(chan job)
	results := make(chan result, len(paths))
	var workers sync.WaitGroup
	for i := 0; i < s.maxWorkers; i++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for j := range jobs {
				results <- s.parseOne(rootPath, j)
			}
		}()
	}
	go func() {
		defer close(jobs)
		for _, path := range paths {
			select {
			case jobs <- job{path: path}:
			case <-ctx.Done():
				return
			}
		}
	}()
	workers.Wait()
	close(results)

	for r := range results {
		if r.err != nil {
			return res, r.err
		}
		res.Modules = append(res.Modules, r.module)
	}

	if err := ctx.Err(); err != nil {
		return res, fmt.Errorf("Scan cancelled: %w", err)
	}

	res.Duration = time.Since(start)
	analysis.Enrich(&res)
	return res, nil
}

func (s *Scanner) parseOne(rootPath string, j job) result {
	content, err := os.ReadFile(j.path)
	if err != nil {
		return result{err: fmt.Errorf("reading %s: %w", j.path, err)}
	}
	mod, err := s.parser.Parse(content)
	if err != nil {
		return result{err: fmt.Errorf("parsing %s: %w", j.path, err)}
	}
	dir, err := filepath.Rel(rootPath, filepath.Dir(j.path))
	if err != nil {
		return result{err: fmt.Errorf("getting module directory for %s: %w", j.path, err)}
	}
	if dir == "." {
		dir = ""
	}
	mod.Directory = dir
	return result{module: mod}
}

func newScanID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
