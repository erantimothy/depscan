// Package analysis derives compact, deterministic dependency intelligence from a scan.
package analysis

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/erantimothy/depscan/internal/domain"
)

// Enrich adds stable identifiers, capability tags, and all derived scan views.
// It uses only the parsed go.mod data, so it performs no network I/O.
func Enrich(scan *domain.ScanResult) {
	for mi := range scan.Modules {
		mod := &scan.Modules[mi]
		mod.ID = stableID("module", mod.Path)
		for di := range mod.Dependencies {
			dep := &mod.Dependencies[di]
			dep.ID = stableID("dependency", mod.Path, dep.Path)
			dep.Category, dep.Reason, dep.Tags = classify(dep.Path)
		}
		sort.Slice(mod.Dependencies, func(i, j int) bool { return mod.Dependencies[i].Path < mod.Dependencies[j].Path })
	}
	sort.Slice(scan.Modules, func(i, j int) bool { return scan.Modules[i].Path < scan.Modules[j].Path })

	scan.Statistics = statistics(scan.Modules)
	scan.ModuleSummaries = moduleSummaries(scan.Modules)
	scan.SharedDependencies, scan.VersionConflicts = sharedAndConflicts(scan.Modules)
	scan.Ecosystems = ecosystems(scan.Modules)
	scan.Graph = graph(scan.Modules)
}

func stableID(parts ...string) string {
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return hex.EncodeToString(sum[:8])
}

func statistics(modules []domain.Module) domain.Statistics {
	s := domain.Statistics{ModuleCount: len(modules)}
	unique := make(map[string]struct{})
	for _, mod := range modules {
		for _, dep := range mod.Dependencies {
			unique[dep.Path] = struct{}{}
			if dep.Indirect {
				s.IndirectDependencies++
			} else {
				s.DirectDependencies++
			}
		}
	}
	s.UniqueDependencies = len(unique)
	return s
}

func moduleSummaries(modules []domain.Module) []domain.ModuleSummary {
	out := make([]domain.ModuleSummary, 0, len(modules))
	for _, mod := range modules {
		s := domain.ModuleSummary{ModuleID: mod.ID, Module: mod.Path, GoVersion: mod.GoVersion, Purpose: mod.Purpose}
		frameworks, systems := make(map[string]struct{}), make(map[string]struct{})
		for _, dep := range mod.Dependencies {
			if dep.Indirect {
				s.IndirectCount++
			} else {
				s.DirectCount++
			}
			if framework := frameworkFor(dep.Path); framework != "" {
				frameworks[framework] = struct{}{}
			}
			if system := systemFor(dep.Path); system != "" {
				systems[system] = struct{}{}
			}
		}
		s.Frameworks = sortedKeys(frameworks)
		s.ExternalSystems = sortedKeys(systems)
		out = append(out, s)
	}
	return out
}

func sharedAndConflicts(modules []domain.Module) ([]domain.SharedDependency, []domain.VersionConflict) {
	byPath := make(map[string][]domain.DependencyUse)
	for _, mod := range modules {
		for _, dep := range mod.Dependencies {
			byPath[dep.Path] = append(byPath[dep.Path], domain.DependencyUse{ModuleID: mod.ID, Module: mod.Path, Version: dep.Version, Indirect: dep.Indirect})
		}
	}
	paths := sortedKeys(byPath)
	shared := make([]domain.SharedDependency, 0)
	conflicts := make([]domain.VersionConflict, 0)
	for _, path := range paths {
		uses := byPath[path]
		sort.Slice(uses, func(i, j int) bool { return uses[i].Module < uses[j].Module })
		if len(uses) > 1 {
			shared = append(shared, domain.SharedDependency{Path: path, Uses: uses})
		}
		versions := make(map[string]struct{})
		for _, use := range uses {
			versions[use.Version] = struct{}{}
		}
		if len(versions) > 1 {
			conflicts = append(conflicts, domain.VersionConflict{Path: path, Versions: uses})
		}
	}
	return shared, conflicts
}

func ecosystems(modules []domain.Module) []domain.EcosystemSummary {
	type aggregate struct{ dependencies, modules map[string]struct{} }
	groups := make(map[string]aggregate)
	for _, mod := range modules {
		for _, dep := range mod.Dependencies {
			name := ecosystemFor(dep.Path)
			if name == "" {
				continue
			}
			a := groups[name]
			if a.dependencies == nil {
				a = aggregate{map[string]struct{}{}, map[string]struct{}{}}
			}
			a.dependencies[dep.Path], a.modules[mod.Path] = struct{}{}, struct{}{}
			groups[name] = a
		}
	}
	out := make([]domain.EcosystemSummary, 0, len(groups))
	for _, name := range sortedKeys(groups) {
		a := groups[name]
		out = append(out, domain.EcosystemSummary{Name: name, Dependencies: sortedKeys(a.dependencies), Modules: sortedKeys(a.modules)})
	}
	return out
}

func graph(modules []domain.Module) domain.DependencyGraph {
	g := domain.DependencyGraph{}
	for _, mod := range modules {
		g.Nodes = append(g.Nodes, domain.GraphNode{ID: mod.ID, Type: "module", Label: mod.Path})
		for _, dep := range mod.Dependencies {
			g.Nodes = append(g.Nodes, domain.GraphNode{ID: dep.ID, Type: "dependency", Label: dep.Path, Version: dep.Version})
			relation := "direct"
			if dep.Indirect {
				relation = "indirect"
			}
			g.Edges = append(g.Edges, domain.GraphEdge{From: mod.ID, To: dep.ID, Relation: relation, Reason: dep.Reason})
		}
	}
	return g
}

// Compare reports dependency changes between two scans, matched by module path.
func Compare(base, current domain.ScanResult) domain.ChangeSummary {
	result := domain.ChangeSummary{BaseScanID: base.ID, ScanID: current.ID, Added: []domain.DependencyChange{}, Removed: []domain.DependencyChange{}, Upgraded: []domain.DependencyChange{}, Downgraded: []domain.DependencyChange{}}
	baseDeps, currentDeps := dependencyIndex(base), dependencyIndex(current)
	keys := make(map[string]struct{}, len(baseDeps)+len(currentDeps))
	for key := range baseDeps {
		keys[key] = struct{}{}
	}
	for key := range currentDeps {
		keys[key] = struct{}{}
	}
	for _, key := range sortedKeys(keys) {
		before, hadBefore := baseDeps[key]
		after, hasAfter := currentDeps[key]
		switch {
		case !hadBefore:
			result.Added = append(result.Added, after)
		case !hasAfter:
			result.Removed = append(result.Removed, before)
		case before.To != after.To:
			change := after
			change.From = before.To
			if versionCompare(after.To, before.To) > 0 {
				result.Upgraded = append(result.Upgraded, change)
			} else {
				result.Downgraded = append(result.Downgraded, change)
			}
		}
	}
	return result
}

func dependencyIndex(scan domain.ScanResult) map[string]domain.DependencyChange {
	out := make(map[string]domain.DependencyChange)
	for _, mod := range scan.Modules {
		for _, dep := range mod.Dependencies {
			out[mod.Path+"\x00"+dep.Path] = domain.DependencyChange{ModuleID: mod.ID, Module: mod.Path, Path: dep.Path, To: dep.Version}
		}
	}
	return out
}

// versionCompare compares Go module versions sufficiently to classify normal
// semantic-version upgrades. It deliberately falls back to a deterministic
// lexical ordering for malformed versions rather than failing a comparison.
func versionCompare(a, b string) int {
	pa, oka := versionParts(a)
	pb, okb := versionParts(b)
	if oka && okb {
		for i := range pa {
			if pa[i] < pb[i] {
				return -1
			}
			if pa[i] > pb[i] {
				return 1
			}
		}
		aPrerelease := strings.Contains(strings.TrimPrefix(a, "v"), "-")
		bPrerelease := strings.Contains(strings.TrimPrefix(b, "v"), "-")
		if aPrerelease && !bPrerelease {
			return -1
		}
		if !aPrerelease && bPrerelease {
			return 1
		}
	}
	return strings.Compare(strings.TrimPrefix(a, "v"), strings.TrimPrefix(b, "v"))
}

func versionParts(version string) ([3]int, bool) {
	var parts [3]int
	version = strings.TrimPrefix(version, "v")
	version = strings.SplitN(version, "-", 2)[0]
	segments := strings.Split(version, ".")
	if len(segments) > 3 {
		return parts, false
	}
	for i, segment := range segments {
		if segment == "" {
			return parts, false
		}
		for _, r := range segment {
			if r < '0' || r > '9' {
				return parts, false
			}
		}
		for _, r := range segment {
			parts[i] = parts[i]*10 + int(r-'0')
		}
	}
	return parts, true
}

// Summary returns the compact scan representation intended for overview and
// AI use cases, without repeating the raw dependencies for every module.
func Summary(scan domain.ScanResult) domain.ScanSummary {
	return domain.ScanSummary{
		ID: scan.ID, RootPath: scan.RootPath, StartedAt: scan.StartedAt, Duration: scan.Duration,
		Statistics: scan.Statistics, ModuleSummaries: scan.ModuleSummaries,
		SharedDependencies: scan.SharedDependencies, VersionConflicts: scan.VersionConflicts,
		Ecosystems: scan.Ecosystems,
	}
}

func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for key := range m {
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}

func classify(path string) (string, string, []string) {
	rules, err := loadRules()
	if err != nil {
		return "", "", nil
	}
	for _, rule := range rules {
		if strings.HasPrefix(path, rule.prefix) {
			return rule.category, rule.reason, []string{rule.category}
		}
	}
	return "", "", nil
}

type rule struct{ prefix, category, reason string }
type RuleSet struct {
	Version int    `json:"version"`
	Rules   []rule `json:"rules"`
}

// var rules = []rule{
// 	{"gorm.io/", "database", "ORM"}, {"github.com/jackc/", "database", "PostgreSQL driver"}, {"github.com/lib/pq", "database", "PostgreSQL driver"}, {"go.mongodb.org/", "database", "MongoDB client"},
// 	{"k8s.io/", "kubernetes", "Kubernetes client"}, {"github.com/hashicorp/vault", "security", "Vault client"}, {"golang.org/x/oauth2", "auth", "OAuth 2.0"},
// 	{"github.com/gorilla/websocket", "websocket", "WebSocket support"}, {"google.golang.org/grpc", "messaging", "gRPC"}, {"go.opentelemetry.io/", "telemetry", "OpenTelemetry"},
// 	{"github.com/stretchr/testify", "testing", "Test assertions"}, {"github.com/spf13/viper", "configuration", "Configuration"}, {"cloud.google.com/go", "cloud", "Google Cloud"}, {"github.com/aws/aws-sdk-go", "cloud", "AWS SDK"},
// }

func loadRules() ([]rule, error) {
	targetPath := filepath.Join("..", "..", "config", "dependency_rules.json")
	file, err := os.ReadFile(targetPath)
	if err != nil {
		return nil, err
	}

	var rules RuleSet
	err = json.Unmarshal(file, &rules)
	if err != nil {
		return nil, err
	}

	return rules.Rules, nil
}

func frameworkFor(path string) string {
	for _, pair := range []struct{ prefix, name string }{{"gorm.io/", "GORM"}, {"k8s.io/", "Kubernetes"}, {"github.com/hashicorp/vault", "Vault"}, {"github.com/modelcontextprotocol/", "MCP SDK"}} {
		if strings.HasPrefix(path, pair.prefix) {
			return pair.name
		}
	}
	return ""
}
func systemFor(path string) string {
	for _, pair := range []struct{ prefix, name string }{{"gorm.io/", "Database"}, {"github.com/jackc/", "PostgreSQL"}, {"github.com/lib/pq", "PostgreSQL"}, {"go.mongodb.org/", "MongoDB"}, {"k8s.io/", "Kubernetes"}, {"github.com/hashicorp/vault", "Vault"}, {"golang.org/x/oauth2", "OAuth"}} {
		if strings.HasPrefix(path, pair.prefix) {
			return pair.name
		}
	}
	return ""
}
func ecosystemFor(path string) string {
	for _, pair := range []struct{ prefix, name string }{{"k8s.io/", "Kubernetes"}, {"github.com/hashicorp/", "HashiCorp"}, {"gorm.io/", "GORM"}, {"go.opentelemetry.io/", "OpenTelemetry"}, {"github.com/aws/", "AWS"}, {"cloud.google.com/go", "Google Cloud"}} {
		if strings.HasPrefix(path, pair.prefix) {
			return pair.name
		}
	}
	return ""
}
