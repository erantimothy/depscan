// Package domain holds the core types and interfaces for depscan
package domain

import (
	"context"
	"errors"
	"time"
)

var (
	ErrNotFound      = errors.New("depscan: resource not found")
	ErrInvalidModule = errors.New("depscan: invalid go.mod content")
	ErrEmptyPath     = errors.New("depscan: path must not be empty")
)

type Dependency struct {
	Path     string `json:"path"`
	Version  string `json:"version"`
	Indirect bool   `json:"indirect"`
}

type Module struct {
	Path         string       `json:"path"`
	GoVersion    string       `json:"goVersion"`
	Dependencies []Dependency `json:"dependencies"`
}

type ScanResult struct {
	ID        string
	RootPath  string
	Modules   []Module
	StartedAt time.Time
	Duration  time.Duration
}

type Parser interface {
	Parse(content []byte) (Module, error)
}

type Scanner interface {
	Scan(ctx context.Context, rootPath string) (ScanResult, error)
}

type Store interface {
	Save(ctx context.Context, result ScanResult) error
	Get(ctx context.Context, id string) (ScanResult, error)
	List(ctx context.Context) ([]ScanResult, error)
}
