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
// Important UI choice: avoid painting a hard-coded background color.
// Different terminals use slightly different black/gray defaults, and mixing a
// forced background with the terminal background can create visible patches.
// We therefore keep the terminal background transparent and only style
// foreground text and borders.

var (
	colBorder  = lipgloss.Color("#3a3a3a")
	colBorderF = lipgloss.Color("#f5a623") // focused border
	colMuted   = lipgloss.Color("#5c5c5c")
	colDim     = lipgloss.Color("#8a8a8a")
	colText    = lipgloss.Color("#d7d7d7")
	colAmber   = lipgloss.Color("#f5a623")
	colGreen   = lipgloss.Color("#4ec94e")
	colRed     = lipgloss.Color("#e05252")
	colBlue    = lipgloss.Color("#5b9cf6")
	colLogCmd  = lipgloss.Color("#f5a623")
	colLogText = lipgloss.Color("#b8b8b8")
)

// styles

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
)

// spinner

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

// cursor position

// cursor points at either a job (stepIdx == -1) or a specific step.
type cursorPos struct {
	jobIdx  int
	stepIdx int // -1 = job row selected
}

func (c cursorPos) isJob() bool { return c.stepIdx == -1 }

// focus

type focusPane int

const (
	focusTree focusPane = iota
	focusLog
)

// messages

type (
	tickMsg           time.Time
	pipelineUpdateMsg struct{}
)

// model

const leftWidth = 32 // fixed width of the left tree pane (chars)

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
}

func NewModel(p *Pipeline) *Model {
	return &Model{
		pipeline: p,
		cursor:   cursorPos{jobIdx: 0, stepIdx: -1},
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

// update

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.rebuildLogViewport()
		return m, nil

	case tickMsg:
		m.tick++
		m.syncLogContent(false)
		return m, tickCmd()

	case pipelineUpdateMsg:
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

		case "up", "k":
			if m.focus == focusTree {
				m.moveCursor(-1)
			} else {
				var cmd tea.Cmd
				m.logVP, cmd = m.logVP.Update(msg)
				return m, cmd
			}

		case "down", "j":
			if m.focus == focusTree {
				m.moveCursor(1)
			} else {
				var cmd tea.Cmd
				m.logVP, cmd = m.logVP.Update(msg)
				return m, cmd
			}

		default:
			if m.focus == focusLog {
				var cmd tea.Cmd
				m.logVP, cmd = m.logVP.Update(msg)
				return m, cmd
			}
		}
	}

	return m, nil
}

// cursor movement

// flatList returns all navigable rows in order: job row, then its step rows.
type navRow struct {
	jobIdx  int
	stepIdx int // -1 = job
}

func (m *Model) navRows() []navRow {
	var rows []navRow
	for ji, j := range m.pipeline.Jobs {
		rows = append(rows, navRow{ji, -1})
		for si := range j.Steps {
			rows = append(rows, navRow{ji, si})
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

// selected step

// selectedStep returns the step whose logs to show, or nil when the cursor is
// on a job row (which renders a summary instead).
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

// log viewport

func (m *Model) logVPWidth() int {
	// total width minus left pane (leftWidth + 2 border + 2 padding) minus right border/padding
	return m.width - leftWidth - 6
}

func (m *Model) logVPHeight() int {
	// pane inner = height-3 (border=2, help=1), minus header line = height-4
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

// view

func (m *Model) View() string {
	if m.width == 0 {
		return "loading…"
	}

	left := m.renderTree()
	right := m.renderLog()

	// join side by side, then add help below
	row := lipgloss.JoinHorizontal(lipgloss.Top, left, right)

	help := m.renderHelp()
	return lipgloss.JoinVertical(lipgloss.Left, row, help)
}

// tree pane

func (m *Model) renderTree() string {
	innerW := leftWidth - 2 // subtract padding
	var lines []string

	for ji, j := range m.pipeline.Jobs {
		onJob := m.cursor.jobIdx == ji && m.cursor.isJob()

		// job row
		icon := statusIcon(j.Status, m.tick)
		cur := "  "
		if onJob {
			cur = styleCursor.Render("▶ ")
		}

		nameStyle := styleJobName
		if onJob {
			nameStyle = styleJobNameActive
		}

		name := truncate(j.Name, innerW-6)
		var right string
		if j.Status == StatusRunning || j.Status == StatusPassed || j.Status == StatusFailed {
			right = styleDim.Render(durStr(j.Duration()))
		}

		jobLine := fmt.Sprintf("%s%s %s", cur, icon, nameStyle.Render(name))
		if right != "" {
			pad := innerW - lipgloss.Width(jobLine) - lipgloss.Width(right)
			if pad < 1 {
				pad = 1
			}
			jobLine += strings.Repeat(" ", pad) + right
		}
		lines = append(lines, jobLine)

		// step rows
		for si, s := range j.Steps {
			onStep := m.cursor.jobIdx == ji && m.cursor.stepIdx == si

			stepCur := "  "
			if onStep {
				stepCur = styleCursor.Render("▶ ")
			}
			stepIcon := statusIcon(s.Status, m.tick)

			sNameStyle := styleStepName
			if onStep {
				sNameStyle = styleStepNameActive
			}

			sName := truncate(s.Name, innerW-8)
			stepLine := fmt.Sprintf("  %s%s %s", stepCur, stepIcon, sNameStyle.Render(sName))

			if s.Status == StatusRunning || s.Status == StatusPassed || s.Status == StatusFailed {
				dur := styleDim.Render(durStr(s.Duration()))
				pad := innerW - lipgloss.Width(stepLine) - lipgloss.Width(dur)
				if pad < 1 {
					pad = 1
				}
				stepLine += strings.Repeat(" ", pad) + dur
			}
			lines = append(lines, stepLine)
		}

		// separator between jobs (not after the last one)
		if ji < len(m.pipeline.Jobs)-1 {
			lines = append(lines, styleDim.Render(strings.Repeat("─", innerW)))
		}
	}

	content := strings.Join(lines, "\n")

	style := stylePane
	if m.focus == focusTree {
		style = stylePaneFocused
	}

	paneH := m.height - 3 // border(2) + help(1) = 3 overhead
	return style.Width(leftWidth).Height(paneH).Render(content)
}

// log pane

func (m *Model) renderLog() string {
	if m.cursor.isJob() {
		return m.renderJobSummary()
	}

	s := m.selectedStep()

	var header string
	if s != nil {
		stepLabel := lipgloss.NewStyle().Foreground(colAmber).Bold(true).Render(s.Name)
		cmdLabel := styleDim.Render(" · " + s.Command)
		header = " " + stepLabel + cmdLabel
	} else {
		header = " " + styleDim.Render("select a step")
	}

	m.logVP.Width = m.logVPWidth()
	m.logVP.Height = m.logVPHeight()

	content := header + "\n" + m.logVP.View()

	style := stylePane
	if m.focus == focusLog {
		style = stylePaneFocused
	}

	rightW := m.width - leftWidth - 4
	paneH := m.height - 3 // border(2) + help(1) = 3 overhead
	return style.Width(rightW).Height(paneH).Render(content)
}

func (m *Model) renderJobSummary() string {
	style := stylePane
	if m.focus == focusLog {
		style = stylePaneFocused
	}
	rightW := m.width - leftWidth - 4
	paneH := m.height - 3

	if m.cursor.jobIdx >= len(m.pipeline.Jobs) {
		return style.Width(rightW).Height(paneH).Render("")
	}
	j := m.pipeline.Jobs[m.cursor.jobIdx]

	header := " " + lipgloss.NewStyle().Foreground(colAmber).Bold(true).Render(j.Name)

	innerW := rightW - 4 // border(2) + padding(2)
	var lines []string
	for _, s := range j.Steps {
		icon := statusIcon(s.Status, m.tick)
		namePart := fmt.Sprintf("  %s %s", icon, styleStepName.Render(s.Name))
		if s.Status == StatusRunning || s.Status == StatusPassed || s.Status == StatusFailed {
			dur := styleDim.Render(durStr(s.Duration()))
			pad := innerW - lipgloss.Width(namePart) - lipgloss.Width(dur)
			if pad < 1 {
				pad = 1
			}
			namePart += strings.Repeat(" ", pad) + dur
		}
		lines = append(lines, namePart)
	}

	content := header + "\n\n" + strings.Join(lines, "\n")
	return style.Width(rightW).Height(paneH).Render(content)
}

// help bar

func (m *Model) renderHelp() string {
	pane := "tree"
	if m.focus == focusLog {
		pane = "logs"
	}
	parts := []string{
		fmt.Sprintf("tab [%s]", pane),
		"↑/↓ navigate",
		"q quit",
	}
	return styleHelp.Width(m.width).Render("  " + strings.Join(parts, "  ·  "))
}

// helpers

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
