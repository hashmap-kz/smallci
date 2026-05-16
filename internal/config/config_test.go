package config

import (
	"os"
	"path/filepath"
	"testing"
)

func writeTemp(t *testing.T, content string) string {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "*.yaml")
	if err != nil {
		t.Fatalf("create temp file: %v", err)
	}
	if _, err := f.WriteString(content); err != nil {
		t.Fatalf("write temp file: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("close temp file: %v", err)
	}
	return f.Name()
}

func TestLoadConfig(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		yaml    string
		wantErr bool
		check   func(t *testing.T, cfg *Config)
	}{
		{
			name: "single job with named steps",
			yaml: `
jobs:
  - name: build
    steps:
      - name: compile
        run: go build ./...
      - name: vet
        run: go vet ./...
`,
			check: func(t *testing.T, cfg *Config) {
				if len(cfg.Jobs) != 1 {
					t.Fatalf("want 1 job, got %d", len(cfg.Jobs))
				}
				job := cfg.Jobs[0]
				if job.Name != "build" {
					t.Errorf("want job name 'build', got %q", job.Name)
				}
				if len(job.Steps) != 2 {
					t.Fatalf("want 2 steps, got %d", len(job.Steps))
				}
				if job.Steps[0].Name != "compile" || job.Steps[0].Run != "go build ./..." {
					t.Errorf("step 0: got name=%q run=%q", job.Steps[0].Name, job.Steps[0].Run)
				}
				if job.Steps[1].Name != "vet" || job.Steps[1].Run != "go vet ./..." {
					t.Errorf("step 1: got name=%q run=%q", job.Steps[1].Name, job.Steps[1].Run)
				}
			},
		},
		{
			name: "step without name",
			yaml: `
jobs:
  - name: test
    steps:
      - run: go test ./...
`,
			check: func(t *testing.T, cfg *Config) {
				if len(cfg.Jobs) != 1 {
					t.Fatalf("want 1 job, got %d", len(cfg.Jobs))
				}
				step := cfg.Jobs[0].Steps[0]
				if step.Name != "" {
					t.Errorf("want empty step name, got %q", step.Name)
				}
				if step.Run != "go test ./..." {
					t.Errorf("want run 'go test ./...', got %q", step.Run)
				}
			},
		},
		{
			name: "multiple jobs",
			yaml: `
jobs:
  - name: lint
    steps:
      - run: golangci-lint run
  - name: test
    steps:
      - run: go test ./...
  - name: build
    steps:
      - run: go build ./...
`,
			check: func(t *testing.T, cfg *Config) {
				if len(cfg.Jobs) != 3 {
					t.Fatalf("want 3 jobs, got %d", len(cfg.Jobs))
				}
				names := []string{"lint", "test", "build"}
				for i, want := range names {
					if cfg.Jobs[i].Name != want {
						t.Errorf("job %d: want name %q, got %q", i, want, cfg.Jobs[i].Name)
					}
				}
			},
		},
		{
			name: "empty jobs list",
			yaml: `jobs: []`,
			check: func(t *testing.T, cfg *Config) {
				if len(cfg.Jobs) != 0 {
					t.Errorf("want 0 jobs, got %d", len(cfg.Jobs))
				}
			},
		},
		{
			name: "empty file parses to zero-value config",
			yaml: ``,
			check: func(t *testing.T, cfg *Config) {
				if cfg.Jobs != nil {
					t.Errorf("want nil jobs, got %v", cfg.Jobs)
				}
			},
		},
		{
			name: "step env vars are parsed",
			yaml: `
jobs:
  - name: build
    steps:
      - name: compile
        run: go build ./...
        env:
          CGO_ENABLED: "0"
          GOOS: linux
`,
			check: func(t *testing.T, cfg *Config) {
				step := cfg.Jobs[0].Steps[0]
				if len(step.Env) != 2 {
					t.Fatalf("want 2 env vars, got %d", len(step.Env))
				}
				if step.Env["CGO_ENABLED"] != "0" {
					t.Errorf("CGO_ENABLED: want %q, got %q", "0", step.Env["CGO_ENABLED"])
				}
				if step.Env["GOOS"] != "linux" {
					t.Errorf("GOOS: want %q, got %q", "linux", step.Env["GOOS"])
				}
			},
		},
		{
			name: "job env vars are parsed",
			yaml: `
jobs:
  - name: build
    env:
      CGO_ENABLED: "0"
      GOFLAGS: -mod=vendor
    steps:
      - name: compile
        run: go build ./...
`,
			check: func(t *testing.T, cfg *Config) {
				job := cfg.Jobs[0]
				if len(job.Env) != 2 {
					t.Fatalf("want 2 job env vars, got %d", len(job.Env))
				}
				if job.Env["CGO_ENABLED"] != "0" {
					t.Errorf("CGO_ENABLED: want %q, got %q", "0", job.Env["CGO_ENABLED"])
				}
				if job.Env["GOFLAGS"] != "-mod=vendor" {
					t.Errorf("GOFLAGS: want %q, got %q", "-mod=vendor", job.Env["GOFLAGS"])
				}
			},
		},
		{
			name: "job without env has nil env map",
			yaml: `
jobs:
  - name: build
    steps:
      - run: go build ./...
`,
			check: func(t *testing.T, cfg *Config) {
				if cfg.Jobs[0].Env != nil {
					t.Errorf("want nil job env, got %v", cfg.Jobs[0].Env)
				}
			},
		},
		{
			name: "step without env has nil env map",
			yaml: `
jobs:
  - name: build
    steps:
      - name: compile
        run: go build ./...
`,
			check: func(t *testing.T, cfg *Config) {
				if cfg.Jobs[0].Steps[0].Env != nil {
					t.Errorf("want nil env, got %v", cfg.Jobs[0].Steps[0].Env)
				}
			},
		},
		{
			name:    "invalid yaml returns error",
			yaml:    `jobs: [unclosed`,
			wantErr: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			path := writeTemp(t, tc.yaml)
			cfg, err := LoadConfig(path)
			if tc.wantErr {
				if err == nil {
					t.Error("want error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tc.check != nil {
				tc.check(t, cfg)
			}
		})
	}
}

func TestLoadConfigFileNotFound(t *testing.T) {
	t.Parallel()

	_, err := LoadConfig(filepath.Join(t.TempDir(), "nonexistent.yaml"))
	if err == nil {
		t.Error("want error for missing file, got nil")
	}
	if !os.IsNotExist(err) {
		t.Errorf("want os.IsNotExist error, got %v", err)
	}
}
