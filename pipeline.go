package main

import (
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"time"
)

type Status int

const (
	StatusWaiting Status = iota
	StatusRunning
	StatusPassed
	StatusFailed
	StatusSkipped
)

// Step is one command inside a Job.
type Step struct {
	Name    string
	Command string

	Status    Status
	StartTime time.Time
	EndTime   time.Time
	Logs      []string
	mu        sync.Mutex
}

func (s *Step) Duration() time.Duration {
	if s.StartTime.IsZero() {
		return 0
	}
	end := s.EndTime
	if end.IsZero() {
		end = time.Now()
	}
	return end.Sub(s.StartTime)
}

func (s *Step) AppendLog(line string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Logs = append(s.Logs, line)
}

func (s *Step) GetLogs() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, len(s.Logs))
	copy(out, s.Logs)
	return out
}

// Job groups steps that run sequentially. Jobs themselves run in parallel.
type Job struct {
	Name  string
	Steps []*Step

	Status Status // aggregate: running if any step running, failed if any failed, etc.
	mu     sync.Mutex
}

func (j *Job) Duration() time.Duration {
	var start, end time.Time
	for _, s := range j.Steps {
		if !s.StartTime.IsZero() {
			if start.IsZero() || s.StartTime.Before(start) {
				start = s.StartTime
			}
		}
		e := s.EndTime
		if e.IsZero() && s.Status == StatusRunning {
			e = time.Now()
		}
		if !e.IsZero() && e.After(end) {
			end = e
		}
	}
	if start.IsZero() {
		return 0
	}
	if end.IsZero() {
		end = time.Now()
	}
	return end.Sub(start)
}

type Pipeline struct {
	Jobs   []*Job
	mu     sync.Mutex
	done   chan struct{}
	notify func()
}

func NewPipeline(cfg *Config) *Pipeline {
	p := &Pipeline{
		done: make(chan struct{}),
	}
	for _, jc := range cfg.Jobs {
		job := &Job{
			Name:   jc.Name,
			Status: StatusWaiting,
		}
		for _, sc := range jc.Steps {
			name := sc.Name
			if name == "" {
				// derive name from the command if not given
				parts := strings.Fields(sc.Run)
				if len(parts) > 0 {
					name = parts[0]
				} else {
					name = sc.Run
				}
			}
			job.Steps = append(job.Steps, &Step{
				Name:    name,
				Command: sc.Run,
				Status:  StatusWaiting,
			})
		}
		p.Jobs = append(p.Jobs, job)
	}
	return p
}

// Run launches all jobs concurrently. Steps within each job run sequentially;
// a failed step skips the rest of that job.
func (p *Pipeline) Run(notify func()) {
	p.notify = notify

	var wg sync.WaitGroup
	for _, j := range p.Jobs {
		j := j
		wg.Add(1)
		go func() {
			defer wg.Done()
			p.runJob(j)
		}()
	}

	go func() {
		wg.Wait()
		close(p.done)
		p.notify()
	}()
}

// AllPassed reports whether every job finished with StatusPassed.
func (p *Pipeline) AllPassed() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, j := range p.Jobs {
		if j.Status != StatusPassed {
			return false
		}
	}
	return true
}

// RerunJob resets the job at jobIdx and re-runs it in a new goroutine.
func (p *Pipeline) RerunJob(jobIdx int) {
	if jobIdx < 0 || jobIdx >= len(p.Jobs) {
		return
	}
	j := p.Jobs[jobIdx]
	p.mu.Lock()
	j.Status = StatusWaiting
	for _, s := range j.Steps {
		s.mu.Lock()
		s.Status = StatusWaiting
		s.Logs = nil
		s.StartTime = time.Time{}
		s.EndTime = time.Time{}
		s.mu.Unlock()
	}
	p.mu.Unlock()
	go p.runJob(j)
}

func (p *Pipeline) setJobStatus(j *Job, status Status) {
	p.mu.Lock()
	j.Status = status
	p.mu.Unlock()
	p.notify()
}

func (p *Pipeline) runJob(j *Job) {
	p.setJobStatus(j, StatusRunning)

	for _, s := range j.Steps {
		p.runStep(j, s)
		if s.Status == StatusFailed {
			// mark remaining steps skipped
			skipping := false
			for _, s2 := range j.Steps {
				if s2 == s {
					skipping = true
					continue
				}
				if skipping {
					p.mu.Lock()
					s2.Status = StatusSkipped
					p.mu.Unlock()
				}
			}
			p.setJobStatus(j, StatusFailed)
			return
		}
	}
	p.setJobStatus(j, StatusPassed)
}

func (p *Pipeline) runStep(j *Job, s *Step) {
	p.mu.Lock()
	s.Status = StatusRunning
	s.StartTime = time.Now()
	p.mu.Unlock()
	p.notify()

	s.AppendLog(fmt.Sprintf("$ %s", s.Command))
	p.notify()

	if strings.TrimSpace(s.Command) == "" {
		p.mu.Lock()
		s.Status = StatusFailed
		s.EndTime = time.Now()
		p.mu.Unlock()
		s.AppendLog("error: empty command")
		p.notify()
		return
	}

	cmd := exec.Command("sh", "-c", s.Command)
	cmd.Stdout = &lineWriter{step: s, notify: p.notify}
	cmd.Stderr = &lineWriter{step: s, notify: p.notify}

	err := cmd.Run()

	p.mu.Lock()
	s.EndTime = time.Now()
	if err != nil {
		s.Status = StatusFailed
	} else {
		s.Status = StatusPassed
	}
	p.mu.Unlock()
	if err != nil {
		s.AppendLog(fmt.Sprintf("error: %v", err))
	}
	p.notify()
}

// lineWriter streams command output line-by-line into step logs.
type lineWriter struct {
	step    *Step
	notify  func()
	partial string
}

func (lw *lineWriter) Write(p []byte) (int, error) {
	lw.partial += string(p)
	for {
		idx := strings.IndexByte(lw.partial, '\n')
		if idx < 0 {
			break
		}
		lw.step.AppendLog(lw.partial[:idx])
		lw.partial = lw.partial[idx+1:]
		lw.notify()
	}
	return len(p), nil
}
