// Package domain holds the core types and interfaces for depscan
package domain

import (
	"errors"
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
