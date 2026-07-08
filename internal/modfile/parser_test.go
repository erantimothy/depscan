package modfile

import (
	"errors"
	"testing"

	"github.com/erantimothy/depscan/internal/domain"
)

func TestParses_Parse(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    domain.Module
		wantErr error
	}{
		{
			name: "single-line requires",
			input: `module github.com/example/app

go 1.22

require github.com/pkg/errors v0.9.1
require golang.org/x/sync v0.5.0 // indirect
`,
			want: domain.Module{
				Path:      "github.com/example/app",
				GoVersion: "1.22",
				Dependencies: []domain.Dependency{
					{Path: "github.com/pkg/errors", Version: "v0.9.1"},
					{Path: "golang.org/x/sync", Version: "v0.5.0", Indirect: true},
				},
			},
		},
		{
			name:    "empty content is an error",
			input:   "",
			wantErr: domain.ErrInvalidModule,
		},
	}
	parser := New()

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := parser.Parse([]byte(tt.input))

			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("Parse() error = %v, want wrapping %v", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("Parse() unexpected error: %v", err)
			}

			if got.Path != tt.want.Path {
				t.Errorf("Path = %q, want %q", got.Path, tt.want.Path)
			}
			// ... same pattern for GoVersion, Dependencies
		})
	}
}
