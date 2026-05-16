package pipeline

import (
	"testing"
	"time"

	"github.com/hashmap-kz/smallci/internal/config"
)

func TestJobDurationWithRunningStep(t *testing.T) {
	t.Parallel()

	now := time.Now()
	cases := []struct {
		name  string
		steps []*Step
		check func(t *testing.T, d time.Duration)
	}{
		{
			name: "running step with no end time contributes to duration",
			steps: []*Step{
				{Status: StatusRunning, StartTime: now.Add(-500 * time.Millisecond)},
			},
			check: func(t *testing.T, d time.Duration) {
				if d <= 0 {
					t.Errorf("want positive duration for running step, got %v", d)
				}
			},
		},
		{
			name: "non-running step with no end time does not contribute",
			steps: []*Step{
				{Status: StatusPassed, StartTime: now.Add(-1 * time.Second)},
				{Status: StatusWaiting}, // no start time, contributes nothing
			},
			check: func(t *testing.T, d time.Duration) {
				if d <= 0 {
					t.Errorf("want positive duration (first step has times), got %v", d)
				}
			},
		},
		{
			name: "skipped step with zero end time does not extend job duration",
			steps: []*Step{
				{Status: StatusPassed, StartTime: now, EndTime: now.Add(1 * time.Second)},
				{Status: StatusSkipped}, // zero times, should not extend beyond first step
			},
			check: func(t *testing.T, d time.Duration) {
				if d != 1*time.Second {
					t.Errorf("want 1s (skipped step ignored), got %v", d)
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

func TestRerunJobClearsLogsAndStatus(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{
		Jobs: []config.JobConfig{
			{Name: "j", Steps: []config.StepConfig{{Name: "step", Run: "echo hello"}}},
		},
	}
	p := NewPipeline(cfg)
	p.Run(noopNotify)
	waitDone(t, p, 5*time.Second)

	step := p.Jobs[0].Steps[0]
	if len(step.GetLogs()) == 0 {
		t.Fatal("want logs after first run")
	}

	p.RerunJob(0)

	// Logs must be cleared immediately after reset, before the step re-runs.
	p.mu.Lock()
	logsAfterReset := len(step.GetLogs())
	p.mu.Unlock()
	if logsAfterReset != 0 {
		t.Errorf("want 0 logs immediately after RerunJob reset, got %d", logsAfterReset)
	}

	pollStatus(t, pollStatusInput{
		Get:     func() Status { return getJobStatus(p, p.Jobs[0]) },
		Want:    StatusPassed,
		Timeout: 5 * time.Second,
	})
}

func TestRerunStepPassesAndUpdatesJobStatus(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{
		Jobs: []config.JobConfig{
			{Name: "j", Steps: []config.StepConfig{
				{Name: "a", Run: "echo a"},
				{Name: "b", Run: "echo b"},
			}},
		},
	}
	p := NewPipeline(cfg)
	p.Run(noopNotify)
	waitDone(t, p, 5*time.Second)

	if !p.AllPassed() {
		t.Fatal("first run: want AllPassed=true")
	}

	p.RerunStep(0, 1)

	pollStatus(t, pollStatusInput{
		Get:     func() Status { return getJobStatus(p, p.Jobs[0]) },
		Want:    StatusPassed,
		Timeout: 5 * time.Second,
	})

	if getStepStatus(p, p.Jobs[0].Steps[1]) != StatusPassed {
		t.Errorf("want step[1] StatusPassed after rerun, got %v", getStepStatus(p, p.Jobs[0].Steps[1]))
	}
}

func TestRerunStepClearsLogsAndTimes(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{
		Jobs: []config.JobConfig{
			{Name: "j", Steps: []config.StepConfig{{Name: "s", Run: "echo x"}}},
		},
	}
	p := NewPipeline(cfg)
	p.Run(noopNotify)
	waitDone(t, p, 5*time.Second)

	step := p.Jobs[0].Steps[0]
	if len(step.GetLogs()) == 0 {
		t.Fatal("want logs after first run")
	}
	if step.StartTime.IsZero() || step.EndTime.IsZero() {
		t.Fatal("want non-zero times after first run")
	}

	p.RerunStep(0, 0)

	// Check reset happened synchronously before step re-runs.
	p.mu.Lock()
	startAfterReset := step.StartTime
	endAfterReset := step.EndTime
	logsAfterReset := len(step.GetLogs())
	p.mu.Unlock()

	if !startAfterReset.IsZero() {
		t.Errorf("want zero StartTime after reset, got %v", startAfterReset)
	}
	if !endAfterReset.IsZero() {
		t.Errorf("want zero EndTime after reset, got %v", endAfterReset)
	}
	if logsAfterReset != 0 {
		t.Errorf("want 0 logs after reset, got %d", logsAfterReset)
	}

	pollStatus(t, pollStatusInput{
		Get:     func() Status { return getStepStatus(p, step) },
		Want:    StatusPassed,
		Timeout: 5 * time.Second,
	})
}

func TestRerunStepFailedThenJobFails(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{
		Jobs: []config.JobConfig{
			{Name: "j", Steps: []config.StepConfig{
				{Name: "ok", Run: "echo ok"},
				{Name: "bad", Run: "exit 1"},
			}},
		},
	}
	p := NewPipeline(cfg)
	p.Run(noopNotify)
	waitDone(t, p, 5*time.Second)

	if getJobStatus(p, p.Jobs[0]) != StatusFailed {
		t.Fatal("want job StatusFailed after first run")
	}

	// Rerun the failing step — it will fail again.
	p.RerunStep(0, 1)

	pollStatus(t, pollStatusInput{
		Get:     func() Status { return getJobStatus(p, p.Jobs[0]) },
		Want:    StatusFailed,
		Timeout: 5 * time.Second,
	})
}

func TestRerunStepInvalidIndex(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{
		Jobs: []config.JobConfig{
			{Name: "j", Steps: []config.StepConfig{{Run: "echo x"}}},
		},
	}
	p := NewPipeline(cfg)

	// Must not panic on any out-of-range combination.
	p.RerunStep(-1, 0)
	p.RerunStep(0, -1)
	p.RerunStep(0, len(p.Jobs[0].Steps))
	p.RerunStep(len(p.Jobs), 0)
}

func TestRerunAll(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{
		Jobs: []config.JobConfig{
			{Name: "a", Steps: []config.StepConfig{{Run: "echo a"}}},
			{Name: "b", Steps: []config.StepConfig{{Run: "echo b"}}},
		},
	}
	p := NewPipeline(cfg)
	p.Run(noopNotify)
	waitDone(t, p, 5*time.Second)

	if !p.AllPassed() {
		t.Fatal("first run: want AllPassed=true")
	}

	// Capture log counts from first run.
	firstRunLogs := make([]int, len(p.Jobs[0].Steps))
	for i, s := range p.Jobs[0].Steps {
		firstRunLogs[i] = len(s.GetLogs())
	}

	p.RerunAll()

	waitDone(t, p, 5*time.Second)

	if !p.AllPassed() {
		t.Error("second run: want AllPassed=true")
	}
	for _, j := range p.Jobs {
		for _, s := range j.Steps {
			if s.Status != StatusPassed {
				t.Errorf("job %s step %s: want StatusPassed, got %v", j.Name, s.Name, s.Status)
			}
		}
	}
}

func TestRerunAllClearsLogsAndTimes(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{
		Jobs: []config.JobConfig{
			{Name: "j", Steps: []config.StepConfig{{Name: "s", Run: "echo x"}}},
		},
	}
	p := NewPipeline(cfg)
	p.Run(noopNotify)
	waitDone(t, p, 5*time.Second)

	step := p.Jobs[0].Steps[0]
	if len(step.GetLogs()) == 0 {
		t.Fatal("want logs after first run")
	}

	p.RerunAll()

	// Reset must be synchronous: check before the goroutines can run the step.
	p.mu.Lock()
	statusAfterReset := step.Status
	logsAfterReset := len(step.GetLogs())
	startAfterReset := step.StartTime
	p.mu.Unlock()

	if statusAfterReset != StatusWaiting {
		t.Errorf("want StatusWaiting immediately after RerunAll reset, got %v", statusAfterReset)
	}
	if logsAfterReset != 0 {
		t.Errorf("want 0 logs after reset, got %d", logsAfterReset)
	}
	if !startAfterReset.IsZero() {
		t.Errorf("want zero StartTime after reset, got %v", startAfterReset)
	}

	waitDone(t, p, 5*time.Second)
}
