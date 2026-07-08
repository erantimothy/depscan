// Package scanner walks a directory tree, finds every go.mod file, and parses them cocurrently using a bounded worker pool.
package scanner

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"

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

	jobs := make(chan job)
	results := make(chan result)

	go func() {
		defer close(jobs)
		_ = filepath.WalkDir(rootPath, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() && d.Name() == "vendor" {
				return filepath.SkipDir
			}
			if !d.IsDir() && d.Name() == "go.mod" {
				select {
				case jobs <- job{path: path}:
				case <-ctx.Done():
					return ctx.Err()
				}
			}
			return nil
		})
	}()

	var pending = make(chan struct{}, s.maxWorkers)
	go func() {
		defer close(results)
		done := make(chan struct{})
		activeWorkers := 0

		for j := range jobs {
			pending <- struct{}{}
			activeWorkers++
			go func(j job) {
				defer func() {
					<-pending
					done <- struct{}{}
				}()
				results <- s.parseOne(j)
			}(j)
		}
		for i := 0; i < activeWorkers; i++ {
			<-done
		}
	}()

	res := domain.ScanResult{
		ID:        newScanID(),
		RootPath:  rootPath,
		StartedAt: start,
	}

	for r := range results {
		if r.err != nil {
			continue
		}
		res.Modules = append(res.Modules, r.module)
	}

	if err := ctx.Err(); err != nil {
		return res, fmt.Errorf("Scan cancelled: %w", err)
	}

	res.Duration = time.Since(start)
	return res, nil
}

func (s *Scanner) parseOne(j job) result {
	content, err := os.ReadFile(j.path)
	if err != nil {
		return result{err: fmt.Errorf("reading %s: %w", j.path, err)}
	}
	mod, err := s.parser.Parse(content)
	if err != nil {
		return result{err: fmt.Errorf("parsing %s: %w", j.path, err)}
	}
	return result{module: mod}
}

func newScanID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
