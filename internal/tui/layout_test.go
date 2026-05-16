package tui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/hashmap-kz/smallci/internal/config"
	"github.com/hashmap-kz/smallci/internal/pipeline"
)

// newTestModel builds a Model from cfg without starting the pipeline.
func newTestModel(cfg *config.Config) *Model {
	return NewModel(pipeline.NewPipeline(cfg))
}

// jobCfg creates a single-job config with the given step count.
func jobCfg(jobName string, nSteps int) *config.Config {
	steps := make([]config.StepConfig, nSteps)
	for i := range steps {
		steps[i] = config.StepConfig{Name: fmt.Sprintf("step%d", i+1), Run: "echo ok"}
	}
	return &config.Config{
		Jobs: []config.JobConfig{{Name: jobName, Steps: steps}},
	}
}

// checkCardWidths asserts that every line of a rendered job card is exactly
// leftWidth visual columns wide. This is the invariant that prevents the
// badge-below-step and floating-timing bugs: if any content line drifts
// narrower or wider, lipgloss word-wraps or mis-aligns the badge column.
func checkCardWidths(t *testing.T, card string) {
	t.Helper()
	for i, line := range strings.Split(card, "\n") {
		if w := lipgloss.Width(line); w != leftWidth {
			t.Errorf("line %d: visual width = %d, want %d  %q", i, w, leftWidth, line)
		}
	}
}

// TestStatusBadgeWidth verifies that statusBadge always produces exactly
// badgeWidth visual columns regardless of status or elapsed duration.
func TestStatusBadgeWidth(t *testing.T) {
	t.Parallel()

	now := time.Now()

	type tc struct {
		name   string
		status pipeline.Status
		start  time.Time
		end    time.Time
	}

	cases := []tc{
		{name: "waiting"},
		{name: "skipped", status: pipeline.StatusSkipped},
		{name: "running_no_dur", status: pipeline.StatusRunning},
		{name: "passed_no_dur", status: pipeline.StatusPassed},
		{name: "failed_no_dur", status: pipeline.StatusFailed},
		{name: "running_5s", status: pipeline.StatusRunning, start: now.Add(-5 * time.Second)},
		{name: "passed_5s", status: pipeline.StatusPassed, start: now.Add(-5 * time.Second), end: now},
		{name: "failed_5s", status: pipeline.StatusFailed, start: now.Add(-5 * time.Second), end: now},
		// 100s: "100.00s" = 7 chars — the maximum that fits as "X 100.00s" within badgeWidth=9.
		{name: "passed_100s", status: pipeline.StatusPassed, start: now.Add(-100 * time.Second), end: now},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			s := &pipeline.Step{Status: c.status, StartTime: c.start, EndTime: c.end}
			if w := lipgloss.Width(statusBadge(s, 0)); w != badgeWidth {
				t.Errorf("statusBadge width = %d, want %d", w, badgeWidth)
			}
		})
	}
}

// TestJobCardLineWidths verifies the layout invariant: every line produced by
// renderJobCard must be exactly leftWidth visual columns wide. A violation
// means lipgloss will word-wrap step rows (pushing badges to the next line) or
// the card border will be misaligned.
func TestJobCardLineWidths(t *testing.T) {
	t.Parallel()

	now := time.Now()

	type stepSetup struct {
		status pipeline.Status
		start  time.Time
		end    time.Time
		logs   []string
	}

	type tc struct {
		name          string
		jobName       string
		steps         []stepSetup
		folded        bool
		cursorStepIdx int // -1 = cursor on job row
	}

	cases := []tc{
		{
			name:          "all_waiting_unfolded",
			jobName:       "build",
			steps:         []stepSetup{{}, {}},
			cursorStepIdx: -1,
		},
		{
			name:          "all_waiting_folded",
			jobName:       "build",
			steps:         []stepSetup{{}, {}},
			folded:        true,
			cursorStepIdx: -1,
		},
		{
			name:    "cursor_on_job",
			jobName: "build",
			steps:   []stepSetup{{}, {}},
			// cursor on job row (stepIdx = -1) is the default
			cursorStepIdx: -1,
		},
		{
			name:    "cursor_on_first_step",
			jobName: "build",
			steps: []stepSetup{
				{status: pipeline.StatusRunning, start: now.Add(-2 * time.Second)},
				{},
			},
			cursorStepIdx: 0,
		},
		{
			name:    "cursor_on_last_step",
			jobName: "build",
			steps: []stepSetup{
				{status: pipeline.StatusPassed, start: now.Add(-3 * time.Second), end: now.Add(-1 * time.Second)},
				{status: pipeline.StatusRunning, start: now.Add(-1 * time.Second)},
			},
			cursorStepIdx: 1,
		},
		{
			name:    "all_passed",
			jobName: "build",
			steps: []stepSetup{
				{status: pipeline.StatusPassed, start: now.Add(-5 * time.Second), end: now.Add(-3 * time.Second)},
				{status: pipeline.StatusPassed, start: now.Add(-3 * time.Second), end: now},
			},
			cursorStepIdx: -1,
		},
		{
			name:    "failed_first_skipped_rest",
			jobName: "test",
			steps: []stepSetup{
				{
					status: pipeline.StatusFailed,
					start:  now.Add(-2 * time.Second),
					end:    now,
					logs:   []string{"$ go test ./...", "FAIL: something broke"},
				},
				{status: pipeline.StatusSkipped},
			},
			cursorStepIdx: -1,
		},
		{
			name:    "failed_non_last_step_hint",
			jobName: "lint",
			steps: []stepSetup{
				{
					status: pipeline.StatusFailed,
					start:  now.Add(-1 * time.Second),
					end:    now,
					logs:   []string{"$ golangci-lint run", "lint error on line 42"},
				},
				{status: pipeline.StatusSkipped},
				{status: pipeline.StatusSkipped},
			},
			cursorStepIdx: -1,
		},
		{
			name:    "single_step_passed",
			jobName: "lint",
			steps: []stepSetup{
				{status: pipeline.StatusPassed, start: now.Add(-1 * time.Second), end: now},
			},
			cursorStepIdx: 0,
		},
		{
			name:          "long_job_name",
			jobName:       strings.Repeat("x", 40),
			steps:         []stepSetup{{status: pipeline.StatusPassed, start: now.Add(-1 * time.Second), end: now}},
			cursorStepIdx: -1,
		},
		{
			name:    "long_step_names",
			jobName: "build",
			steps: []stepSetup{
				{status: pipeline.StatusPassed, start: now.Add(-1 * time.Second), end: now},
				{status: pipeline.StatusWaiting},
			},
			cursorStepIdx: -1,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			cfg := jobCfg(tc.jobName, len(tc.steps))

			// Override step names with long names for the relevant test case.
			if tc.name == "long_step_names" {
				for i := range cfg.Jobs[0].Steps {
					cfg.Jobs[0].Steps[i].Name = strings.Repeat("s", 40)
				}
			}

			m := newTestModel(cfg)
			j := m.pipeline.Jobs[0]

			for i, setup := range tc.steps {
				j.Steps[i].Status = setup.status
				j.Steps[i].StartTime = setup.start
				j.Steps[i].EndTime = setup.end
				for _, line := range setup.logs {
					j.Steps[i].AppendLog(line)
				}
			}

			m.folded[0] = tc.folded
			m.cursor = cursorPos{jobIdx: 0, stepIdx: tc.cursorStepIdx}

			checkCardWidths(t, m.renderJobCard(0, j))
		})
	}
}
