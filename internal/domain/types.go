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
	ErrNoModules     = errors.New("depscan: no go.mod files found")
)

type Dependency struct {
	ID       string   `json:"id"`
	Path     string   `json:"path"`
	Version  string   `json:"version"`
	Indirect bool     `json:"indirect"`
	Tags     []string `json:"tags,omitempty"`
	Category string   `json:"category,omitempty"`
	Reason   string   `json:"reason,omitempty"`
}

type Module struct {
	ID           string       `json:"id"`
	Path         string       `json:"path"`
	GoVersion    string       `json:"goVersion"`
	Directory    string       `json:"directory,omitempty"`
	Purpose      string       `json:"purpose,omitempty"`
	Dependencies []Dependency `json:"dependencies"`
}

type ScanResult struct {
	ID                 string             `json:"id"`
	RootPath           string             `json:"rootPath"`
	Modules            []Module           `json:"modules"`
	StartedAt          time.Time          `json:"startedAt"`
	Duration           time.Duration      `json:"duration"`
	Statistics         Statistics         `json:"statistics"`
	ModuleSummaries    []ModuleSummary    `json:"moduleSummaries"`
	SharedDependencies []SharedDependency `json:"sharedDependencies"`
	VersionConflicts   []VersionConflict  `json:"versionConflicts"`
	Ecosystems         []EcosystemSummary `json:"ecosystems"`
	Graph              DependencyGraph    `json:"graph"`
}

// ScanSummary is the compact, LLM-friendly view of a scan. The full scan
// remains available when callers need the raw per-module dependency lists.
type ScanSummary struct {
	ID                 string             `json:"id"`
	RootPath           string             `json:"rootPath"`
	StartedAt          time.Time          `json:"startedAt"`
	Duration           time.Duration      `json:"duration"`
	Statistics         Statistics         `json:"statistics"`
	ModuleSummaries    []ModuleSummary    `json:"moduleSummaries"`
	SharedDependencies []SharedDependency `json:"sharedDependencies"`
	VersionConflicts   []VersionConflict  `json:"versionConflicts"`
	Ecosystems         []EcosystemSummary `json:"ecosystems"`
}

type Statistics struct {
	ModuleCount          int `json:"moduleCount"`
	DirectDependencies   int `json:"directDependencies"`
	IndirectDependencies int `json:"indirectDependencies"`
	UniqueDependencies   int `json:"uniqueDependencies"`
}

type ModuleSummary struct {
	ModuleID        string   `json:"moduleId"`
	Module          string   `json:"module"`
	GoVersion       string   `json:"goVersion"`
	Purpose         string   `json:"purpose,omitempty"`
	DirectCount     int      `json:"directCount"`
	IndirectCount   int      `json:"indirectCount"`
	Frameworks      []string `json:"frameworks,omitempty"`
	ExternalSystems []string `json:"externalSystems,omitempty"`
}

type DependencyUse struct {
	ModuleID string `json:"moduleId"`
	Module   string `json:"module"`
	Version  string `json:"version"`
	Indirect bool   `json:"indirect"`
}

type SharedDependency struct {
	Path string          `json:"path"`
	Uses []DependencyUse `json:"uses"`
}

type VersionConflict struct {
	Path     string          `json:"path"`
	Versions []DependencyUse `json:"versions"`
}

type EcosystemSummary struct {
	Name         string   `json:"name"`
	Dependencies []string `json:"dependencies"`
	Modules      []string `json:"modules"`
}

type DependencyGraph struct {
	Nodes []GraphNode `json:"nodes"`
	Edges []GraphEdge `json:"edges"`
}

type GraphNode struct {
	ID      string `json:"id"`
	Type    string `json:"type"`
	Label   string `json:"label"`
	Version string `json:"version,omitempty"`
}

type GraphEdge struct {
	From     string `json:"from"`
	To       string `json:"to"`
	Relation string `json:"relation"`
	Reason   string `json:"reason,omitempty"`
}

type ChangeSummary struct {
	BaseScanID string             `json:"baseScanId"`
	ScanID     string             `json:"scanId"`
	Added      []DependencyChange `json:"added"`
	Removed    []DependencyChange `json:"removed"`
	Upgraded   []DependencyChange `json:"upgraded"`
	Downgraded []DependencyChange `json:"downgraded"`
}

type DependencyChange struct {
	ModuleID string `json:"moduleId"`
	Module   string `json:"module"`
	Path     string `json:"path"`
	From     string `json:"from,omitempty"`
	To       string `json:"to,omitempty"`
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

type RepoSource struct {
	Owner string
	Repo  string
	Ref   string
}

type RepoFetcher interface {
	Fetch(ctx context.Context, source RepoSource) (localPath string, cleanup func(), err error)
}
