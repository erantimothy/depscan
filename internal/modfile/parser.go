// Package modfile parses go.mod files using only the standard library.
package modfile

import (
	"bufio"
	"bytes"
	"fmt"
	"strings"

	"github.com/erantimothy/depscan/internal/domain"
)

type Parser struct{}

// Constructor for Parser
func New() *Parser {
	return &Parser{}
}

func (p *Parser) Parse(content []byte) (domain.Module, error) {
	if len(bytes.TrimSpace(content)) == 0 {
		return domain.Module{}, fmt.Errorf("parsing go.mod: %w", domain.ErrInvalidModule)
	}

	mod := domain.Module{}
	scanner := bufio.NewScanner(bytes.NewReader(content))
	inRequireBlock := false

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "//") {
			continue
		}

		switch {
		case strings.HasPrefix(line, "module "):
			mod.Path = strings.TrimSpace(strings.TrimPrefix(line, "module "))

		case strings.HasPrefix(line, "go "):
			mod.GoVersion = strings.TrimSpace(strings.TrimPrefix(line, "go "))

		case line == "require (":
			inRequireBlock = true

		case inRequireBlock && line == ")":
			inRequireBlock = false

		case inRequireBlock:
			if dep, ok := parseRequireLine(line); ok {
				mod.Dependencies = append(mod.Dependencies, dep)
			}

		case strings.HasPrefix(line, "require "):
			rest := strings.TrimSpace(strings.TrimPrefix(line, "require "))
			if dep, ok := parseRequireLine(rest); ok {
				mod.Dependencies = append(mod.Dependencies, dep)
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return domain.Module{}, fmt.Errorf("scanning go.mod: %w", err)
	}
	if mod.Path == "" {
		return domain.Module{}, fmt.Errorf("parsing go.mod: missing module directive")
	}

	return mod, nil
}

func parseRequireLine(line string) (domain.Dependency, bool) {
	indirect := false
	if idx := strings.Index(line, "//"); idx != -1 {
		comment := strings.TrimSpace(line[idx+2:])
		indirect = comment == "indirect"
		line = strings.TrimSpace(line[:idx])
	}

	fields := strings.Fields(line)
	if len(fields) < 2 {
		return domain.Dependency{}, false
	}

	return domain.Dependency{
		Path:     fields[0],
		Version:  fields[1],
		Indirect: indirect,
	}, true
}
