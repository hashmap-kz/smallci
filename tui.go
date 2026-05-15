package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// palette
//
// Avoid hard-coded background colors - terminal backgrounds vary and mixing
// forced backgrounds with the terminal default creates visible patches.
var (
	colBorder    = lipgloss.Color("#3a3a3a")
	colBorderF   = lipgloss.Color("#f5a623")
	colBorderOK  = lipgloss.Color("#4ec94e")
	colBorderErr = lipgloss.Color("#e05252")
	colMuted     = lipgloss.Color("#5c5c5c")
	colDim       = lipgloss.Color("#8a8a8a")
	colText      = lipgloss.Color("#d7d7d7")
	colAmber     = lipgloss.Color("#f5a623")
	colGreen     = lipgloss.Color("#4ec94e")
	colRed       = lipgloss.Color("#e05252")
	colBlue      = lipgloss.Color("#5b9cf6")
	colLogCmd    = lipgloss.Color("#f5a623")
	colLogText   = lipgloss.Color("#b8b8b8")
)

var (
	stylePane = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colBorder).
			Padding(0, 1)

	stylePaneFocused = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(colBorderF).
				Padding(0, 1)

	styleJobName = lipgloss.NewStyle().
			Foreground(colText).
			Bold(true)

	styleJobNameActive = lipgloss.NewStyle().
				Foreground(colAmber).
				Bold(true)

	styleStepName = lipgloss.NewStyle().
			Foreground(colDim)

	styleStepNameActive = lipgloss.NewStyle().
				Foreground(colText)

	styleCursor = lipgloss.NewStyle().
			Foreground(colAmber).
			Bold(true)

	styleDim = lipgloss.NewStyle().
			Foreground(colMuted)

	styleHelp = lipgloss.NewStyle().
			Foreground(colMuted)

	styleLogCmd  = lipgloss.NewStyle().Foreground(colLogCmd)
	styleLogText = lipgloss.NewStyle().Foreground(colLogText)
	styleLogFail = lipgloss.NewStyle().Foreground(colRed)
	styleErrHint = lipgloss.NewStyle().Foreground(colRed).Faint(true)
)

var spinFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

func statusIcon(s Status, tick int) string {
	switch s {
	case StatusPassed:
		return lipgloss.NewStyle().Foreground(colGreen).Render("✓")
	case StatusFailed:
		return lipgloss.NewStyle().Foreground(colRed).Render("✗")
	case StatusRunning:
		return lipgloss.NewStyle().Foreground(colBlue).Render(spinFrames[tick%len(spinFrames)])
	case StatusSkipped:
		return styleDim.Render("–")
	default:
		return styleDim.Render("·")
	}
}

func durStr(d time.Duration) string {
	return fmt.Sprintf("%.2fs", d.Seconds())
}

// cursorPos points at either a job (stepIdx == -1) or a specific step.
type cursorPos struct {
	jobIdx  int
	stepIdx int
}

func (c cursorPos) isJob() bool { return c.stepIdx == -1 }

type focusPane int

const (
	focusTree focusPane = iota
	focusLog
)

type viewMode int

const (
	viewNormal viewMode = iota
	viewTimeline
)

type (
	tickMsg           time.Time
	pipelineUpdateMsg struct{}
)

const leftWidth = 34

// Model is the top-level Bubble Tea model.
type Model struct {
	pipeline   *Pipeline
	cursor     cursorPos
	tick       int
	width      int
	height     int
	focus      focusPane
	logVP      viewport.Model
	lastLogLen int
	program    *tea.Program

	folded       map[int]bool   // jobIdx -> collapsed
	autoFollow   bool           // cursor tracks running/failing steps automatically
	prevAllDone  bool           // previous "all jobs done" state for edge-free transition
	seenFailed   map[*Step]bool // steps already auto-jumped to
	mode         viewMode
	pipelineDone bool // true once we've detected a run completion
	flashTick    int  // counts down over ~15 ticks (~1.2s) for border flash
	flashPassed  bool // true = green flash, false = red flash
}

func NewModel(p *Pipeline) *Model {
	return &Model{
		pipeline:   p,
		cursor:     cursorPos{jobIdx: 0, stepIdx: -1},
		folded:     make(map[int]bool),
		seenFailed: make(map[*Step]bool),
		autoFollow: true,
	}
}

func (m *Model) SetProgram(p *tea.Program) { m.program = p }

func (m *Model) Init() tea.Cmd {
	return tea.Batch(tickCmd(), m.startPipeline())
}

func (m *Model) startPipeline() tea.Cmd {
	return func() tea.Msg {
		m.pipeline.Run(func() {
			if m.program != nil {
				m.program.Send(pipelineUpdateMsg{})
			}
		})
		return nil
	}
}

func tickCmd() tea.Cmd {
	return tea.Tick(80*time.Millisecond, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.rebuildLogViewport()
		return m, nil

	case tickMsg:
		m.tick++
		if m.flashTick > 0 {
			m.flashTick--
		}
		m.syncLogContent(false)
		return m, tickCmd()

	case pipelineUpdateMsg:
		m.handlePipelineUpdate()
		m.syncLogContent(false)
		return m, nil

	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			return m, tea.Quit

		case "tab":
			if m.focus == focusTree {
				m.focus = focusLog
			} else {
				m.focus = focusTree
			}

		case "t":
			if m.mode == viewTimeline {
				m.mode = viewNormal
			} else {
				m.mode = viewTimeline
			}

		// h/l: vim tree motions - h collapses/goes up, l expands/goes into
		case "h":
			if m.focus == focusTree {
				m.autoFollow = false
				if m.cursor.isJob() {
					m.folded[m.cursor.jobIdx] = true
				} else {
					// Step -> move up to parent job row
					m.cursor.stepIdx = -1
					m.lastLogLen = -1
					m.syncLogContent(true)
				}
			}

		case "l":
			if m.focus == focusTree {
				m.autoFollow = false
				ji := m.cursor.jobIdx
				if m.cursor.isJob() {
					if m.folded[ji] {
						m.folded[ji] = false
					} else if ji < len(m.pipeline.Jobs) && len(m.pipeline.Jobs[ji].Steps) > 0 {
						m.cursor = cursorPos{ji, 0}
						m.lastLogLen = -1
						m.syncLogContent(true)
					}
				}
				// On a step, l is a no-op (no deeper level)
			}

		case "enter", " ":
			if m.focus == focusTree && m.cursor.isJob() {
				m.folded[m.cursor.jobIdx] = !m.folded[m.cursor.jobIdx]
			}

		case "f":
			m.jumpToFirstFailure()

		case "r":
			if m.focus == focusTree {
				m.rerunJob(m.cursor.jobIdx)
			}

		case "up", "k":
			if m.focus == focusTree {
				m.autoFollow = false
				m.moveCursor(-1)
			} else if m.mode == viewNormal {
				var cmd tea.Cmd
				m.logVP, cmd = m.logVP.Update(msg)
				return m, cmd
			}

		case "down", "j":
			if m.focus == focusTree {
				m.autoFollow = false
				m.moveCursor(1)
			} else if m.mode == viewNormal {
				var cmd tea.Cmd
				m.logVP, cmd = m.logVP.Update(msg)
				return m, cmd
			}

		default:
			if m.focus == focusLog && m.mode == viewNormal {
				var cmd tea.Cmd
				m.logVP, cmd = m.logVP.Update(msg)
				return m, cmd
			}
		}
	}

	return m, nil
}

func (m *Model) handlePipelineUpdate() {
	// Detect completion by watching the not-done -> done transition.
	// Using allDone/anyStarted avoids the race where fast jobs emit
	// StatusRunning and StatusPassed before the tea loop processes either
	// notification, which causes the anyJobRunning-based approach to miss
	// the running->idle edge entirely.
	anyStarted := false
	allDone := true
	for _, j := range m.pipeline.Jobs {
		if j.Status != StatusWaiting {
			anyStarted = true
		}
		if j.Status == StatusWaiting || j.Status == StatusRunning {
			allDone = false
		}
	}
	isDone := allDone && anyStarted
	if isDone && !m.prevAllDone && !m.pipelineDone {
		m.pipelineDone = true
		m.flashTick = 15
		m.flashPassed = m.pipeline.AllPassed()
		m.autoFollow = false
	}
	m.prevAllDone = isDone

	// Auto-jump to first new failure.
	for ji, j := range m.pipeline.Jobs {
		for si, s := range j.Steps {
			if s.Status == StatusFailed && !m.seenFailed[s] {
				m.seenFailed[s] = true
				m.folded[ji] = false
				m.cursor = cursorPos{ji, si}
				m.focus = focusLog
				m.autoFollow = false
				m.lastLogLen = -1
				return
			}
		}
	}

	// Auto-follow the currently running step.
	if m.autoFollow {
		for ji, j := range m.pipeline.Jobs {
			for si, s := range j.Steps {
				if s.Status == StatusRunning {
					if m.cursor.jobIdx != ji || m.cursor.stepIdx != si {
						m.folded[ji] = false
						m.cursor = cursorPos{ji, si}
						m.lastLogLen = -1
					}
					return
				}
			}
		}
	}
}

func (m *Model) jumpToFirstFailure() {
	for ji, j := range m.pipeline.Jobs {
		for si, s := range j.Steps {
			if s.Status == StatusFailed {
				m.folded[ji] = false
				m.cursor = cursorPos{ji, si}
				m.focus = focusLog
				m.lastLogLen = -1
				m.syncLogContent(true)
				return
			}
		}
	}
}

func (m *Model) rerunJob(jobIdx int) {
	if jobIdx < 0 || jobIdx >= len(m.pipeline.Jobs) {
		return
	}
	j := m.pipeline.Jobs[jobIdx]
	for _, s := range j.Steps {
		delete(m.seenFailed, s)
	}
	m.pipelineDone = false
	m.prevAllDone = false
	m.autoFollow = true
	m.pipeline.RerunJob(jobIdx)
}

// navRows returns all navigable rows respecting fold state.
type navRow struct {
	jobIdx  int
	stepIdx int
}

func (m *Model) navRows() []navRow {
	var rows []navRow
	for ji, j := range m.pipeline.Jobs {
		rows = append(rows, navRow{ji, -1})
		if !m.folded[ji] {
			for si := range j.Steps {
				rows = append(rows, navRow{ji, si})
			}
		}
	}
	return rows
}

func (m *Model) cursorIndex() int {
	for i, r := range m.navRows() {
		if r.jobIdx == m.cursor.jobIdx && r.stepIdx == m.cursor.stepIdx {
			return i
		}
	}
	return 0
}

func (m *Model) moveCursor(d int) {
	rows := m.navRows()
	if len(rows) == 0 {
		return
	}
	idx := clamp(m.cursorIndex()+d, 0, len(rows)-1)
	r := rows[idx]
	m.cursor = cursorPos{r.jobIdx, r.stepIdx}
	m.lastLogLen = -1
	m.syncLogContent(true)
}

func (m *Model) selectedStep() *Step {
	if m.cursor.isJob() || m.cursor.jobIdx >= len(m.pipeline.Jobs) {
		return nil
	}
	j := m.pipeline.Jobs[m.cursor.jobIdx]
	if m.cursor.stepIdx >= 0 && m.cursor.stepIdx < len(j.Steps) {
		return j.Steps[m.cursor.stepIdx]
	}
	return nil
}

func (m *Model) logVPWidth() int {
	return m.width - leftWidth - 6
}

func (m *Model) logVPHeight() int {
	h := m.height - 4
	if h < 3 {
		h = 3
	}
	return h
}

func (m *Model) rebuildLogViewport() {
	m.logVP = viewport.New(m.logVPWidth(), m.logVPHeight())
	m.logVP.Style = lipgloss.NewStyle()
	m.syncLogContent(true)
}

func (m *Model) syncLogContent(forceBottom bool) {
	s := m.selectedStep()
	if s == nil {
		m.logVP.SetContent("")
		m.lastLogLen = 0
		return
	}
	logs := s.GetLogs()
	newLen := len(logs)
	if newLen == m.lastLogLen && !forceBottom {
		return
	}
	var sb strings.Builder
	for _, line := range logs {
		switch {
		case strings.HasPrefix(line, "$"):
			sb.WriteString(styleLogCmd.Render(line))
		case strings.HasPrefix(line, "FAIL") || strings.HasPrefix(line, "--- FAIL"):
			sb.WriteString(styleLogFail.Render(line))
		default:
			sb.WriteString(styleLogText.Render(line))
		}
		sb.WriteByte('\n')
	}
	m.logVP.SetContent(sb.String())
	if forceBottom || newLen > m.lastLogLen {
		m.logVP.GotoBottom()
	}
	m.lastLogLen = newLen
}

func (m *Model) View() string {
	if m.width == 0 {
		return "loading…"
	}
	left := m.renderTree()
	var right string
	switch {
	case m.mode == viewTimeline:
		right = m.renderTimeline()
	case m.cursor.isJob():
		right = m.renderJobSummary()
	default:
		right = m.renderLogPane()
	}
	return lipgloss.JoinVertical(lipgloss.Left,
		lipgloss.JoinHorizontal(lipgloss.Top, left, right),
		m.renderHelp(),
	)
}

// paneStyle returns the border style for a pane, applying the completion flash
// which temporarily overrides the focused border color.
func (m *Model) paneStyle(side focusPane) lipgloss.Style {
	style := stylePane
	if m.focus == side {
		style = stylePaneFocused
	}
	if m.flashTick > 0 {
		if m.flashPassed {
			style = style.BorderForeground(colBorderOK)
		} else {
			style = style.BorderForeground(colBorderErr)
		}
	}
	return style
}

func (m *Model) renderTree() string {
	// innerW is the usable content width inside the pane border+padding.
	innerW := leftWidth - 2
	var lines []string

	for ji, j := range m.pipeline.Jobs {
		onJob := m.cursor.jobIdx == ji && m.cursor.isJob()
		folded := m.folded[ji]

		// Job row layout:
		//   {cur:2}{icon:1}{ :1}{name}{pad}{fold:1}{ :1}{dur:5}
		// Name starts at column 4; duration+fold on the right.
		cur := "  "
		if onJob {
			cur = styleCursor.Render("▶ ")
		}
		nameStyle := styleJobName
		if onJob {
			nameStyle = styleJobNameActive
		}

		name := truncate(j.Name, innerW-12)
		jobLine := fmt.Sprintf("%s%s %s", cur, statusIcon(j.Status, m.tick), nameStyle.Render(name))

		foldMark := styleDim.Render("▾")
		if folded {
			foldMark = styleDim.Render("▸")
		}
		var right string
		if j.Status == StatusRunning || j.Status == StatusPassed || j.Status == StatusFailed {
			right = foldMark + " " + styleDim.Render(durStr(j.Duration()))
		} else {
			right = foldMark
		}
		if pad := innerW - lipgloss.Width(jobLine) - lipgloss.Width(right); pad >= 1 {
			jobLine += strings.Repeat(" ", pad) + right
		}
		lines = append(lines, jobLine)

		if folded {
			if ji < len(m.pipeline.Jobs)-1 {
				lines = append(lines, styleDim.Render(strings.Repeat("─", innerW)))
			}
			continue
		}

		// Step row layout:
		//   {"  ":2}{cur:2}{icon:1}{ :1}{name}{pad}{dur:5}
		// Name starts at column 6; two chars deeper than the job name.
		for si, s := range j.Steps {
			onStep := m.cursor.jobIdx == ji && m.cursor.stepIdx == si

			stepCur := "  "
			if onStep {
				stepCur = styleCursor.Render("▶ ")
			}
			sNameStyle := styleStepName
			if onStep {
				sNameStyle = styleStepNameActive
			}

			sName := truncate(s.Name, innerW-12)
			stepLine := fmt.Sprintf("  %s%s %s", stepCur, statusIcon(s.Status, m.tick), sNameStyle.Render(sName))

			if s.Status == StatusRunning || s.Status == StatusPassed || s.Status == StatusFailed {
				dur := styleDim.Render(durStr(s.Duration()))
				if pad := innerW - lipgloss.Width(stepLine) - lipgloss.Width(dur); pad >= 1 {
					stepLine += strings.Repeat(" ", pad) + dur
				}
			}
			lines = append(lines, stepLine)

			if s.Status == StatusFailed {
				if hint := lastErrLine(s); hint != "" {
					lines = append(lines, fmt.Sprintf("      %s", styleErrHint.Render("↳ "+truncate(hint, innerW-8))))
				}
			}
		}

		if ji < len(m.pipeline.Jobs)-1 {
			lines = append(lines, styleDim.Render(strings.Repeat("─", innerW)))
		}
	}

	paneH := m.height - 3
	return m.paneStyle(focusTree).Width(leftWidth).Height(paneH).Render(strings.Join(lines, "\n"))
}

func (m *Model) renderLogPane() string {
	s := m.selectedStep()
	var header string
	if s != nil {
		header = " " + lipgloss.NewStyle().Foreground(colAmber).Bold(true).Render(s.Name) +
			styleDim.Render(" · "+s.Command)
	} else {
		header = " " + styleDim.Render("select a step")
	}

	m.logVP.Width = m.logVPWidth()
	m.logVP.Height = m.logVPHeight()

	rightW := m.width - leftWidth - 4
	paneH := m.height - 3
	return m.paneStyle(focusLog).Width(rightW).Height(paneH).Render(header + "\n" + m.logVP.View())
}

func (m *Model) renderJobSummary() string {
	rightW := m.width - leftWidth - 4
	paneH := m.height - 3
	innerW := rightW - 4

	if m.cursor.jobIdx >= len(m.pipeline.Jobs) {
		return m.paneStyle(focusLog).Width(rightW).Height(paneH).Render("")
	}
	j := m.pipeline.Jobs[m.cursor.jobIdx]

	var sb strings.Builder

	if m.pipelineDone {
		if m.flashPassed {
			fmt.Fprintf(&sb, " %s\n\n", lipgloss.NewStyle().Foreground(colGreen).Bold(true).Render("✓ all jobs passed"))
		} else {
			fmt.Fprintf(&sb, " %s\n\n", lipgloss.NewStyle().Foreground(colRed).Bold(true).Render("✗ pipeline failed"))
		}
	}
	fmt.Fprintf(&sb, " %s\n\n", lipgloss.NewStyle().Foreground(colAmber).Bold(true).Render(j.Name))

	for _, s := range j.Steps {
		row := fmt.Sprintf("  %s %s", statusIcon(s.Status, m.tick), styleStepName.Render(s.Name))
		if s.Status == StatusRunning || s.Status == StatusPassed || s.Status == StatusFailed {
			dur := styleDim.Render(durStr(s.Duration()))
			if pad := innerW - lipgloss.Width(row) - lipgloss.Width(dur); pad >= 1 {
				row += strings.Repeat(" ", pad) + dur
			}
		}
		sb.WriteString(row + "\n")
		if s.Status == StatusFailed {
			if hint := lastErrLine(s); hint != "" {
				fmt.Fprintf(&sb, "    %s\n", styleErrHint.Render("↳ "+truncate(hint, innerW-6)))
			}
		}
	}

	return m.paneStyle(focusLog).Width(rightW).Height(paneH).Render(sb.String())
}

func (m *Model) renderTimeline() string {
	rightW := m.width - leftWidth - 4
	paneH := m.height - 3
	innerW := rightW - 4

	style := m.paneStyle(focusLog)

	var globalStart, globalEnd time.Time
	for _, j := range m.pipeline.Jobs {
		for _, s := range j.Steps {
			if s.StartTime.IsZero() {
				continue
			}
			if globalStart.IsZero() || s.StartTime.Before(globalStart) {
				globalStart = s.StartTime
			}
			end := s.EndTime
			if end.IsZero() {
				end = time.Now()
			}
			if end.After(globalEnd) {
				globalEnd = end
			}
		}
	}

	if globalStart.IsZero() {
		return style.Width(rightW).Height(paneH).Render(" Timeline\n\n  pipeline has not started yet")
	}

	totalDur := globalEnd.Sub(globalStart)
	if totalDur < time.Millisecond {
		totalDur = time.Millisecond
	}

	const labelW = 12
	barW := innerW - labelW - 2 - 7
	if barW < 8 {
		barW = 8
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, " Timeline  %s total\n\n", durStr(totalDur))

	for _, j := range m.pipeline.Jobs {
		var jStart, jEnd time.Time
		for _, s := range j.Steps {
			if !s.StartTime.IsZero() {
				if jStart.IsZero() || s.StartTime.Before(jStart) {
					jStart = s.StartTime
				}
			}
			end := s.EndTime
			if end.IsZero() && s.Status == StatusRunning {
				end = time.Now()
			}
			if !end.IsZero() && end.After(jEnd) {
				jEnd = end
			}
		}

		label := styleDim.Render(fmt.Sprintf("%-*s", labelW, truncate(j.Name, labelW)))

		if jStart.IsZero() {
			fmt.Fprintf(&sb, "  %s  %s\n", label, styleDim.Render("·"))
			continue
		}
		if jEnd.IsZero() {
			jEnd = time.Now()
		}

		offsetFrac := float64(jStart.Sub(globalStart)) / float64(totalDur)
		durFrac := float64(jEnd.Sub(jStart)) / float64(totalDur)
		lead := int(offsetFrac * float64(barW))
		barLen := int(durFrac * float64(barW))
		if barLen < 1 {
			barLen = 1
		}
		if lead+barLen > barW {
			barLen = barW - lead
		}

		var barColor lipgloss.Color
		switch j.Status {
		case StatusPassed:
			barColor = colGreen
		case StatusFailed:
			barColor = colRed
		case StatusRunning:
			barColor = colAmber
		default:
			barColor = colMuted
		}
		bar := lipgloss.NewStyle().Foreground(barColor).Render(strings.Repeat("█", barLen))
		fmt.Fprintf(&sb, "  %s  %s%s  %s\n",
			label, strings.Repeat(" ", lead), bar, styleDim.Render(durStr(jEnd.Sub(jStart))))
	}

	axisRight := durStr(totalDur)
	gap := barW - 2 - len(axisRight)
	if gap < 0 {
		gap = 0
	}
	fmt.Fprintf(&sb, "\n%s", styleDim.Render(
		fmt.Sprintf("  %s  0s%s%s", strings.Repeat(" ", labelW), strings.Repeat(" ", gap), axisRight),
	))

	return style.Width(rightW).Height(paneH).Render(sb.String())
}

func (m *Model) renderHelp() string {
	pane := "tree"
	if m.focus == focusLog {
		pane = "logs"
	}
	viewToggle := "t timeline"
	if m.mode == viewTimeline {
		viewToggle = "t normal"
	}
	parts := []string{
		fmt.Sprintf("tab [%s]", pane),
		"↑/↓ navigate",
		"h/l fold/unfold",
		"f jump failure",
		"r rerun job",
		viewToggle,
		"q quit",
	}
	return styleHelp.Width(m.width).Render("  " + strings.Join(parts, "  ·  "))
}

func truncate(s string, max int) string {
	if max < 1 {
		return ""
	}
	if len(s) <= max {
		return s
	}
	return s[:max-1] + "…"
}

func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// lastErrLine returns the last non-empty, non-command log line from a step.
func lastErrLine(s *Step) string {
	logs := s.GetLogs()
	for i := len(logs) - 1; i >= 0; i-- {
		line := strings.TrimSpace(logs[i])
		if line != "" && !strings.HasPrefix(line, "$") {
			return line
		}
	}
	return ""
}
