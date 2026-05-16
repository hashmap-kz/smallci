package pipeline

import (
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hashmap-kz/smallci/internal/config"
)

func noopNotify() {}

//nolint:unparam
func waitDone(t *testing.T, p *Pipeline, timeout time.Duration) {
	t.Helper()
	select {
	case <-p.done:
	case <-time.After(timeout):
		t.Fatal("pipeline did not finish within timeout")
	}
}

type pollStatusInput struct {
	Get     func() Status
	Want    Status
	Timeout time.Duration
}

func pollStatus(t *testing.T, in pollStatusInput) {
	t.Helper()
	deadline := time.Now().Add(in.Timeout)
	for time.Now().Before(deadline) {
		if in.Get() == in.Want {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out after %v: want status %v, got %v", in.Timeout, in.Want, in.Get())
}

func getJobStatus(p *Pipeline, j *Job) Status {
	p.mu.Lock()
	defer p.mu.Unlock()
	return j.Status
}

func getStepStatus(p *Pipeline, s *Step) Status {
	p.mu.Lock()
	defer p.mu.Unlock()
	return s.Status
}

func TestStepDuration(t *testing.T) {
	t.Parallel()

	now := time.Now()
	cases := []struct {
		name      string
		startTime time.Time
		endTime   time.Time
		check     func(t *testing.T, d time.Duration)
	}{
		{
			name: "zero start returns zero",
			check: func(t *testing.T, d time.Duration) {
				if d != 0 {
					t.Errorf("want 0, got %v", d)
				}
			},
		},
		{
			name:      "both times set returns exact duration",
			startTime: now,
			endTime:   now.Add(3 * time.Second),
			check: func(t *testing.T, d time.Duration) {
				if d != 3*time.Second {
					t.Errorf("want 3s, got %v", d)
				}
			},
		},
		{
			name:      "start set end zero returns positive duration",
			startTime: now.Add(-100 * time.Millisecond),
			check: func(t *testing.T, d time.Duration) {
				if d <= 0 {
					t.Errorf("want positive duration, got %v", d)
				}
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			s := &Step{StartTime: tc.startTime, EndTime: tc.endTime}
			tc.check(t, s.Duration())
		})
	}
}

func TestStepAppendGetLogs(t *testing.T) {
	t.Parallel()

	s := &Step{}
	lines := []string{"line 1", "line 2", "line 3"}
	for _, l := range lines {
		s.AppendLog(l)
	}
	got := s.GetLogs()
	if len(got) != len(lines) {
		t.Fatalf("want %d logs, got %d", len(lines), len(got))
	}
	for i, want := range lines {
		if got[i] != want {
			t.Errorf("log[%d]: want %q, got %q", i, want, got[i])
		}
	}
}

func TestStepGetLogsReturnsCopy(t *testing.T) {
	t.Parallel()

	s := &Step{}
	s.AppendLog("original")
	got := s.GetLogs()
	got[0] = "modified"
	second := s.GetLogs()
	if second[0] != "original" {
		t.Errorf("GetLogs must return a copy, got %q", second[0])
	}
}

func TestStepAppendLogConcurrent(t *testing.T) {
	t.Parallel()

	const n = 100
	s := &Step{}
	var wg sync.WaitGroup
	for range n {
		wg.Go(func() {
			s.AppendLog("line")
		})
	}
	wg.Wait()
	if len(s.GetLogs()) != n {
		t.Errorf("want %d logs after concurrent appends, got %d", n, len(s.GetLogs()))
	}
}

func TestJobDuration(t *testing.T) {
	t.Parallel()

	now := time.Now()
	cases := []struct {
		name  string
		steps []*Step
		check func(t *testing.T, d time.Duration)
	}{
		{
			name:  "no steps returns zero",
			steps: nil,
			check: func(t *testing.T, d time.Duration) {
				if d != 0 {
					t.Errorf("want 0, got %v", d)
				}
			},
		},
		{
			name:  "steps with no start times return zero",
			steps: []*Step{{}, {}},
			check: func(t *testing.T, d time.Duration) {
				if d != 0 {
					t.Errorf("want 0, got %v", d)
				}
			},
		},
		{
			name: "single step with both times",
			steps: []*Step{
				{StartTime: now, EndTime: now.Add(2 * time.Second)},
			},
			check: func(t *testing.T, d time.Duration) {
				if d != 2*time.Second {
					t.Errorf("want 2s, got %v", d)
				}
			},
		},
		{
			name: "sequential steps span from first start to last end",
			steps: []*Step{
				{StartTime: now, EndTime: now.Add(1 * time.Second)},
				{StartTime: now.Add(1 * time.Second), EndTime: now.Add(3 * time.Second)},
			},
			check: func(t *testing.T, d time.Duration) {
				if d != 3*time.Second {
					t.Errorf("want 3s, got %v", d)
				}
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			j := &Job{Steps: tc.steps}
			tc.check(t, j.Duration())
		})
	}
}

func TestNewPipeline(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name      string
		cfg       *config.Config
		wantJobs  int
		checkJobs func(t *testing.T, p *Pipeline)
	}{
		{
			name: "step name preserved when given",
			cfg: &config.Config{
				Jobs: []config.JobConfig{
					{Name: "build", Steps: []config.StepConfig{{Name: "compile", Run: "go build ./..."}}},
				},
			},
			wantJobs: 1,
			checkJobs: func(t *testing.T, p *Pipeline) {
				if p.Jobs[0].Steps[0].Name != "compile" {
					t.Errorf("want step name 'compile', got %q", p.Jobs[0].Steps[0].Name)
				}
			},
		},
		{
			name: "step name derived from first command word when not given",
			cfg: &config.Config{
				Jobs: []config.JobConfig{
					{Name: "test", Steps: []config.StepConfig{{Run: "go test ./..."}}},
				},
			},
			wantJobs: 1,
			checkJobs: func(t *testing.T, p *Pipeline) {
				if p.Jobs[0].Steps[0].Name != "go" {
					t.Errorf("want step name 'go', got %q", p.Jobs[0].Steps[0].Name)
				}
			},
		},
		{
			name: "step name derived from single-word command",
			cfg: &config.Config{
				Jobs: []config.JobConfig{
					{Name: "run", Steps: []config.StepConfig{{Run: "make"}}},
				},
			},
			wantJobs: 1,
			checkJobs: func(t *testing.T, p *Pipeline) {
				if p.Jobs[0].Steps[0].Name != "make" {
					t.Errorf("want step name 'make', got %q", p.Jobs[0].Steps[0].Name)
				}
			},
		},
		{
			name: "step with empty run and no name gets empty name",
			cfg: &config.Config{
				Jobs: []config.JobConfig{
					{Name: "empty", Steps: []config.StepConfig{{Run: ""}}},
				},
			},
			wantJobs: 1,
			checkJobs: func(t *testing.T, p *Pipeline) {
				if p.Jobs[0].Steps[0].Name != "" {
					t.Errorf("want empty step name, got %q", p.Jobs[0].Steps[0].Name)
				}
			},
		},
		{
			name: "all steps and jobs start as StatusWaiting",
			cfg: &config.Config{
				Jobs: []config.JobConfig{
					{Name: "j", Steps: []config.StepConfig{{Run: "echo a"}, {Run: "echo b"}}},
				},
			},
			wantJobs: 1,
			checkJobs: func(t *testing.T, p *Pipeline) {
				if p.Jobs[0].Status != StatusWaiting {
					t.Errorf("want job StatusWaiting, got %v", p.Jobs[0].Status)
				}
				for _, s := range p.Jobs[0].Steps {
					if s.Status != StatusWaiting {
						t.Errorf("want step StatusWaiting, got %v", s.Status)
					}
				}
			},
		},
		{
			name: "multiple jobs created with correct names",
			cfg: &config.Config{
				Jobs: []config.JobConfig{
					{Name: "a", Steps: []config.StepConfig{{Run: "echo a"}}},
					{Name: "b", Steps: []config.StepConfig{{Run: "echo b"}}},
					{Name: "c", Steps: []config.StepConfig{{Run: "echo c"}}},
				},
			},
			wantJobs: 3,
			checkJobs: func(t *testing.T, p *Pipeline) {
				names := []string{"a", "b", "c"}
				for i, name := range names {
					if p.Jobs[i].Name != name {
						t.Errorf("job %d: want name %q, got %q", i, name, p.Jobs[i].Name)
					}
				}
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			p := NewPipeline(tc.cfg)
			if len(p.Jobs) != tc.wantJobs {
				t.Fatalf("want %d jobs, got %d", tc.wantJobs, len(p.Jobs))
			}
			if tc.checkJobs != nil {
				tc.checkJobs(t, p)
			}
		})
	}
}

func TestAllPassed(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		statuses []Status
		want     bool
	}{
		{name: "no jobs returns true", statuses: nil, want: true},
		{name: "all passed", statuses: []Status{StatusPassed, StatusPassed}, want: true},
		{name: "one failed", statuses: []Status{StatusPassed, StatusFailed}, want: false},
		{name: "one waiting", statuses: []Status{StatusPassed, StatusWaiting}, want: false},
		{name: "one running", statuses: []Status{StatusRunning}, want: false},
		{name: "one skipped", statuses: []Status{StatusSkipped}, want: false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			p := &Pipeline{done: make(chan struct{})}
			for _, st := range tc.statuses {
				p.Jobs = append(p.Jobs, &Job{Status: st})
			}
			if got := p.AllPassed(); got != tc.want {
				t.Errorf("want AllPassed=%v, got %v", tc.want, got)
			}
		})
	}
}

func TestLineWriter(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		writes   []string
		wantLogs []string
	}{
		{
			name:     "single line with newline",
			writes:   []string{"hello\n"},
			wantLogs: []string{"hello"},
		},
		{
			name:     "multiple lines in one write",
			writes:   []string{"a\nb\nc\n"},
			wantLogs: []string{"a", "b", "c"},
		},
		{
			name:     "partial line without newline is buffered",
			writes:   []string{"no newline"},
			wantLogs: []string{},
		},
		{
			name:     "partial write followed by rest flushes combined line",
			writes:   []string{"part", "ial\n"},
			wantLogs: []string{"partial"},
		},
		{
			name:     "empty write produces no logs",
			writes:   []string{""},
			wantLogs: []string{},
		},
		{
			name:     "sequential writes each with newline",
			writes:   []string{"line1\n", "line2\n"},
			wantLogs: []string{"line1", "line2"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			s := &Step{}
			lw := &lineWriter{step: s, notify: noopNotify}
			for _, w := range tc.writes {
				n, err := lw.Write([]byte(w))
				if err != nil {
					t.Fatalf("Write(%q): unexpected error: %v", w, err)
				}
				if n != len(w) {
					t.Fatalf("Write(%q): want n=%d, got n=%d", w, len(w), n)
				}
			}
			logs := s.GetLogs()
			if len(logs) != len(tc.wantLogs) {
				t.Fatalf("want %d logs %v, got %d %v", len(tc.wantLogs), tc.wantLogs, len(logs), logs)
			}
			for i, want := range tc.wantLogs {
				if logs[i] != want {
					t.Errorf("log[%d]: want %q, got %q", i, want, logs[i])
				}
			}
		})
	}
}

func TestPipelineRunSingleJobPasses(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{
		Jobs: []config.JobConfig{
			{Name: "greet", Steps: []config.StepConfig{{Name: "say", Run: "echo hello"}}},
		},
	}
	p := NewPipeline(cfg)
	p.Run(noopNotify)
	waitDone(t, p, 5*time.Second)

	if !p.AllPassed() {
		t.Error("want AllPassed=true")
	}
	if getStepStatus(p, p.Jobs[0].Steps[0]) != StatusPassed {
		t.Errorf("want step StatusPassed, got %v", getStepStatus(p, p.Jobs[0].Steps[0]))
	}
	if len(p.Jobs[0].Steps[0].GetLogs()) == 0 {
		t.Error("want at least one log line")
	}
}

func TestPipelineRunEmptyCommandFails(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{
		Jobs: []config.JobConfig{
			{Name: "bad", Steps: []config.StepConfig{{Name: "empty", Run: "   "}}},
		},
	}
	p := NewPipeline(cfg)
	p.Run(noopNotify)
	waitDone(t, p, 5*time.Second)

	if p.AllPassed() {
		t.Error("want AllPassed=false for empty command")
	}
	if getStepStatus(p, p.Jobs[0].Steps[0]) != StatusFailed {
		t.Errorf("want step StatusFailed, got %v", getStepStatus(p, p.Jobs[0].Steps[0]))
	}
}

func TestPipelineRunFailedStepSkipsRemainingSteps(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{
		Jobs: []config.JobConfig{
			{
				Name: "job",
				Steps: []config.StepConfig{
					{Name: "fail", Run: "exit 1"},
					{Name: "skip1", Run: "echo skipped"},
					{Name: "skip2", Run: "echo skipped2"},
				},
			},
		},
	}
	p := NewPipeline(cfg)
	p.Run(noopNotify)
	waitDone(t, p, 5*time.Second)

	job := p.Jobs[0]
	if getJobStatus(p, job) != StatusFailed {
		t.Errorf("want job StatusFailed, got %v", getJobStatus(p, job))
	}
	if getStepStatus(p, job.Steps[0]) != StatusFailed {
		t.Errorf("want steps[0] StatusFailed, got %v", getStepStatus(p, job.Steps[0]))
	}
	if getStepStatus(p, job.Steps[1]) != StatusSkipped {
		t.Errorf("want steps[1] StatusSkipped, got %v", getStepStatus(p, job.Steps[1]))
	}
	if getStepStatus(p, job.Steps[2]) != StatusSkipped {
		t.Errorf("want steps[2] StatusSkipped, got %v", getStepStatus(p, job.Steps[2]))
	}
}

func TestPipelineRunMultipleJobsAllPass(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{
		Jobs: []config.JobConfig{
			{Name: "a", Steps: []config.StepConfig{{Run: "echo a"}}},
			{Name: "b", Steps: []config.StepConfig{{Run: "echo b"}}},
			{Name: "c", Steps: []config.StepConfig{{Run: "echo c"}}},
		},
	}
	p := NewPipeline(cfg)
	p.Run(noopNotify)
	waitDone(t, p, 5*time.Second)

	if !p.AllPassed() {
		for i, j := range p.Jobs {
			t.Logf("job %d (%s): status=%v", i, j.Name, getJobStatus(p, j))
		}
		t.Error("want AllPassed=true")
	}
}

func TestPipelineRerunJob(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{
		Jobs: []config.JobConfig{
			{Name: "rerun", Steps: []config.StepConfig{{Name: "step", Run: "echo rerun"}}},
		},
	}
	p := NewPipeline(cfg)
	p.Run(noopNotify)
	waitDone(t, p, 5*time.Second)

	if !p.AllPassed() {
		t.Fatal("first run: want AllPassed=true")
	}

	p.RerunJob(0)

	pollStatus(t, pollStatusInput{
		Get:     func() Status { return getJobStatus(p, p.Jobs[0]) },
		Want:    StatusPassed,
		Timeout: 5 * time.Second,
	})

	if getStepStatus(p, p.Jobs[0].Steps[0]) != StatusPassed {
		t.Errorf("want step StatusPassed after rerun, got %v", getStepStatus(p, p.Jobs[0].Steps[0]))
	}
}

func TestStepEnvVarPassedToCommand(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{
		Jobs: []config.JobConfig{
			{Name: "env", Steps: []config.StepConfig{
				{Name: "print", Run: "echo $SMALLCI_TEST_VAR", Env: map[string]string{
					"SMALLCI_TEST_VAR": "hello_from_env",
				}},
			}},
		},
	}
	p := NewPipeline(cfg)
	p.Run(noopNotify)
	waitDone(t, p, 5*time.Second)

	if !p.AllPassed() {
		t.Fatal("want AllPassed=true")
	}
	logs := p.Jobs[0].Steps[0].GetLogs()
	found := false
	for _, line := range logs {
		if strings.Contains(line, "hello_from_env") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("want log line containing %q, got %v", "hello_from_env", logs)
	}
}

func TestStepEnvVarOverridesParentEnv(t *testing.T) {
	t.Setenv("SMALLCI_OVERRIDE_TEST", "parent_value")

	cfg := &config.Config{
		Jobs: []config.JobConfig{
			{Name: "env", Steps: []config.StepConfig{
				{Name: "override", Run: "echo $SMALLCI_OVERRIDE_TEST", Env: map[string]string{
					"SMALLCI_OVERRIDE_TEST": "step_value",
				}},
			}},
		},
	}
	p := NewPipeline(cfg)
	p.Run(noopNotify)
	waitDone(t, p, 5*time.Second)

	logs := p.Jobs[0].Steps[0].GetLogs()
	for _, line := range logs {
		if strings.Contains(line, "parent_value") {
			t.Errorf("step env should override parent; found parent_value in log: %v", logs)
		}
	}
	found := false
	for _, line := range logs {
		if strings.Contains(line, "step_value") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("want log line containing %q, got %v", "step_value", logs)
	}
}

func TestJobEnvVarInheritedByStep(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{
		Jobs: []config.JobConfig{
			{
				Name: "env", Env: map[string]string{"SMALLCI_JOB_VAR": "from_job"},
				Steps: []config.StepConfig{
					{Name: "print", Run: "echo $SMALLCI_JOB_VAR"},
				},
			},
		},
	}
	p := NewPipeline(cfg)
	p.Run(noopNotify)
	waitDone(t, p, 5*time.Second)

	if !p.AllPassed() {
		t.Fatal("want AllPassed=true")
	}
	logs := p.Jobs[0].Steps[0].GetLogs()
	found := false
	for _, line := range logs {
		if strings.Contains(line, "from_job") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("want log line containing %q, got %v", "from_job", logs)
	}
}

func TestStepEnvOverridesJobEnv(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{
		Jobs: []config.JobConfig{
			{
				Name: "env", Env: map[string]string{"SMALLCI_OVERRIDE_JOB": "job_value"},
				Steps: []config.StepConfig{
					{Name: "override", Run: "echo $SMALLCI_OVERRIDE_JOB", Env: map[string]string{
						"SMALLCI_OVERRIDE_JOB": "step_value",
					}},
				},
			},
		},
	}
	p := NewPipeline(cfg)
	p.Run(noopNotify)
	waitDone(t, p, 5*time.Second)

	logs := p.Jobs[0].Steps[0].GetLogs()
	for _, line := range logs {
		if strings.Contains(line, "job_value") {
			t.Errorf("step env should override job env; found job_value in log: %v", logs)
		}
	}
	found := false
	for _, line := range logs {
		if strings.Contains(line, "step_value") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("want log line containing %q, got %v", "step_value", logs)
	}
}

func TestStepWithNoEnvInheritsParentEnv(t *testing.T) {
	t.Setenv("SMALLCI_INHERIT_TEST", "inherited_value")

	cfg := &config.Config{
		Jobs: []config.JobConfig{
			{Name: "env", Steps: []config.StepConfig{
				{Name: "inherit", Run: "echo $SMALLCI_INHERIT_TEST"},
			}},
		},
	}
	p := NewPipeline(cfg)
	p.Run(noopNotify)
	waitDone(t, p, 5*time.Second)

	logs := p.Jobs[0].Steps[0].GetLogs()
	found := false
	for _, line := range logs {
		if strings.Contains(line, "inherited_value") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("want step to inherit parent env; log %v", logs)
	}
}

func TestPipelineRerunJobInvalidIndex(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{
		Jobs: []config.JobConfig{
			{Name: "j", Steps: []config.StepConfig{{Run: "echo x"}}},
		},
	}
	p := NewPipeline(cfg)
	p.Run(noopNotify)
	waitDone(t, p, 5*time.Second)

	// must not panic on out-of-range indices
	p.RerunJob(-1)
	p.RerunJob(len(p.Jobs))
}
