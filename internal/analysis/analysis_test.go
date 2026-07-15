package analysis

import (
	"testing"

	"github.com/erantimothy/depscan/internal/domain"
)

func TestEnrichBuildsDeterministicViews(t *testing.T) {
	scan := domain.ScanResult{Modules: []domain.Module{
		{Path: "example.com/service", GoVersion: "1.26", Dependencies: []domain.Dependency{
			{Path: "gorm.io/gorm", Version: "v1.25.0"},
			{Path: "golang.org/x/oauth2", Version: "v0.35.0", Indirect: true},
		}},
		{Path: "example.com/cli", Dependencies: []domain.Dependency{
			{Path: "gorm.io/gorm", Version: "v1.24.0"},
			{Path: "golang.org/x/oauth2", Version: "v0.35.0"},
		}},
	}}

	Enrich(&scan)

	if scan.Statistics != (domain.Statistics{ModuleCount: 2, DirectDependencies: 3, IndirectDependencies: 1, UniqueDependencies: 2}) {
		t.Fatalf("Statistics = %+v", scan.Statistics)
	}
	if len(scan.SharedDependencies) != 2 || len(scan.VersionConflicts) != 1 {
		t.Fatalf("shared=%d conflicts=%d, want 2 and 1", len(scan.SharedDependencies), len(scan.VersionConflicts))
	}
	var gorm domain.Dependency
	for _, dependency := range scan.Modules[1].Dependencies {
		if dependency.Path == "gorm.io/gorm" {
			gorm = dependency
			break
		}
	}
	if gorm.Category != "database" || gorm.Reason != "ORM" {
		t.Fatalf("GORM classification = %+v", gorm)
	}
	if len(scan.Graph.Nodes) != 6 || len(scan.Graph.Edges) != 4 {
		t.Fatalf("graph nodes=%d edges=%d", len(scan.Graph.Nodes), len(scan.Graph.Edges))
	}
	if summary := Summary(scan); len(summary.ModuleSummaries) != 2 || summary.ID != scan.ID {
		t.Fatalf("summary = %+v", summary)
	}
}

func TestCompareUsesNumericVersions(t *testing.T) {
	base := domain.ScanResult{ID: "base", Modules: []domain.Module{{Path: "example.com/app", Dependencies: []domain.Dependency{{Path: "example.com/lib", Version: "v1.9.0"}}}}}
	current := domain.ScanResult{ID: "current", Modules: []domain.Module{{Path: "example.com/app", Dependencies: []domain.Dependency{{Path: "example.com/lib", Version: "v1.10.0"}}}}}

	changes := Compare(base, current)
	if len(changes.Upgraded) != 1 || len(changes.Downgraded) != 0 {
		t.Fatalf("changes = %+v", changes)
	}
}
