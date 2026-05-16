package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/hashmap-kz/smallci/internal/pipeline"
	"github.com/hashmap-kz/smallci/internal/x/fmtx"
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

// jobPalette assigns a distinct accent color to each job by index.
var jobPalette = []lipgloss.Color{
	lipgloss.Color("#5b9cf6"), // blue
	lipgloss.Color("#c678dd"), // purple
	lipgloss.Color("#56b6c2"), // cyan
	lipgloss.Color("#e06c75"), // salmon
	lipgloss.Color("#98c379"), // sage
	lipgloss.Color("#e5c07b"), // gold
}

func jobColor(idx int) lipgloss.Color {
	return jobPalette[idx%len(jobPalette)]
}

var (
	stylePane = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colBorder).
			Padding(0, 1)

	stylePaneFocused = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(colBorderF).
				Padding(0, 1)

	// Tree pane has no inner padding; job cards provide their own borders.
	stylePaneTree = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colBorder).
			Padding(0, 0)

	stylePaneTreeFocused = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(colBorderF).
				Padding(0, 0)

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

	styleLogCmd       = lipgloss.NewStyle().Foreground(colLogCmd)
	styleLogText      = lipgloss.NewStyle().Foreground(colLogText)
	styleLogSearch    = lipgloss.NewStyle().Foreground(lipgloss.Color("#f5a623"))
	styleLogSearchCur = lipgloss.NewStyle().Foreground(lipgloss.Color("#1a1a1a")).Background(lipgloss.Color("#f5a623")).Bold(true)
	styleErrHint      = lipgloss.NewStyle().Foreground(colRed).Faint(true)
)

var spinFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// badgeWidth is the visual width of every statusBadge output.
const badgeWidth = 9

func statusIcon(s pipeline.Status, tick int) string {
	switch s {
	case pipeline.StatusPassed:
		return lipgloss.NewStyle().Foreground(colGreen).Render("✓")
	case pipeline.StatusFailed:
		return lipgloss.NewStyle().Foreground(colRed).Render("✗")
	case pipeline.StatusRunning:
		return lipgloss.NewStyle().Foreground(colBlue).Render(spinFrames[tick%len(spinFrames)])
	case pipeline.StatusSkipped:
		return styleDim.Render("–")
	default:
		return styleDim.Render("·")
	}
}

// statusBadge renders a right-aligned fixed-width (badgeWidth) status+timing label.
func statusBadge(s *pipeline.Step, tick int) string {
	frame := spinFrames[tick%len(spinFrames)]
	var content string
	var st lipgloss.Style
	switch s.Status {
	case pipeline.StatusPassed:
		content, st = "✓ "+durStr(s.Duration()), lipgloss.NewStyle().Foreground(colGreen).Bold(true)
	case pipeline.StatusFailed:
		content, st = "✗ "+durStr(s.Duration()), lipgloss.NewStyle().Foreground(colRed).Bold(true)
	case pipeline.StatusRunning:
		content, st = frame+" "+durStr(s.Duration()), lipgloss.NewStyle().Foreground(colBlue)
	case pipeline.StatusSkipped:
		content, st = "skip", styleDim
	default:
		content, st = "·", styleDim
	}
	rendered := st.Render(content)
	if pad := badgeWidth - lipgloss.Width(rendered); pad > 0 {
		return strings.Repeat(" ", pad) + rendered
	}
	return rendered
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

const leftWidth = 38

// cardContentW is the content width inside a job card (leftWidth minus card border+padding).
const cardContentW = leftWidth - 4

// Model is the top-level Bubble Tea model.
type Model struct {
	pipeline   *pipeline.Pipeline
	cursor     cursorPos
	tick       int
	width      int
	height     int
	focus      focusPane
	logVP      viewport.Model
	lastLogLen int
	program    *tea.Program

	folded       map[int]bool
	autoFollow   bool
	prevAllDone  bool
	seenFailed   map[*pipeline.Step]bool
	mode         viewMode
	pipelineDone bool
	flashTick    int
	flashPassed  bool

	// fullLog expands the right pane to full width, hiding the tree.
	fullLog bool

	// search state for log pane filtering.
	searchMode      bool
	searchQuery     string
	lastSearchQuery string
	searchMatches   []int
	searchCurrent   int

	// treeOffset is the first visible rendered line index in the tree.
	treeOffset int
}

// NewModel creates a new Model wrapping the given pipeline.
func NewModel(p *pipeline.Pipeline) *Model {
	return &Model{
		pipeline:   p,
		cursor:     cursorPos{jobIdx: 0, stepIdx: -1},
		folded:     make(map[int]bool),
		seenFailed: make(map[*pipeline.Step]bool),
		autoFollow: true,
	}
}

// SetProgram stores the tea.Program so the model can send messages from goroutines.
func (m *Model) SetProgram(p *tea.Program) { m.program = p }

// Init starts the pipeline and the tick loop.
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

//nolint:gocyclo
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
		if m.searchMode {
			m.handleSearchKey(msg)
			return m, nil
		}

		switch msg.String() {
		case "ctrl+c":
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

		case "z":
			m.fullLog = !m.fullLog
			m.rebuildLogViewport()

		case "/":
			if m.focus == focusLog {
				m.searchMode = true
				m.searchQuery = ""
				m.searchMatches = nil
				m.searchCurrent = 0
			}

		case "n":
			if m.focus == focusLog && len(m.searchMatches) > 0 {
				m.nextSearchMatch(1)
			}

		case "N":
			if m.focus == focusLog && len(m.searchMatches) > 0 {
				m.nextSearchMatch(-1)
			}

		case "h":
			if m.focus == focusTree {
				m.autoFollow = false
				if m.cursor.isJob() {
					m.folded[m.cursor.jobIdx] = true
				} else {
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
			}

		case "enter", " ":
			if m.focus == focusTree && m.cursor.isJob() {
				m.folded[m.cursor.jobIdx] = !m.folded[m.cursor.jobIdx]
			}

		case "f":
			m.jumpToFirstFailure()

		case "r":
			if m.focus == focusTree {
				if m.cursor.isJob() {
					m.rerunJob(m.cursor.jobIdx)
				} else {
					m.rerunStep(m.cursor.jobIdx, m.cursor.stepIdx)
				}
			}

		case "R":
			m.rerunAll()

		case "J":
			if m.focus == focusTree {
				m.autoFollow = false
				m.jumpToJob(1)
			}

		case "K":
			if m.focus == focusTree {
				m.autoFollow = false
				m.jumpToJob(-1)
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

func (m *Model) handleSearchKey(msg tea.KeyMsg) {
	switch msg.String() {
	case "esc":
		m.searchMode = false
		m.searchQuery = ""
		m.searchMatches = nil
		m.syncLogContent(false)
	case "enter":
		m.searchMode = false
	case "backspace", "ctrl+h":
		runes := []rune(m.searchQuery)
		if len(runes) > 0 {
			m.searchQuery = string(runes[:len(runes)-1])
			m.recomputeSearchMatches()
			m.syncLogContent(false)
		}
	default:
		if len(msg.Runes) > 0 {
			m.searchQuery += string(msg.Runes)
			m.recomputeSearchMatches()
			m.syncLogContent(false)
		}
	}
}

func (m *Model) recomputeSearchMatches() {
	m.searchMatches = nil
	if m.searchQuery == "" {
		return
	}
	s := m.selectedStep()
	if s == nil {
		return
	}
	q := strings.ToLower(m.searchQuery)
	for i, line := range s.GetLogs() {
		if strings.Contains(strings.ToLower(line), q) {
			m.searchMatches = append(m.searchMatches, i)
		}
	}
	if m.searchCurrent >= len(m.searchMatches) {
		m.searchCurrent = 0
	}
}

func (m *Model) nextSearchMatch(dir int) {
	if len(m.searchMatches) == 0 {
		return
	}
	m.searchCurrent = (m.searchCurrent + dir + len(m.searchMatches)) % len(m.searchMatches)
	m.logVP.SetYOffset(m.searchMatches[m.searchCurrent])
	m.lastSearchQuery = "" // force re-render so current-match highlight updates
	m.syncLogContent(false)
}

func (m *Model) handlePipelineUpdate() {
	anyStarted := false
	allDone := true
	for _, j := range m.pipeline.Jobs {
		if j.Status != pipeline.StatusWaiting {
			anyStarted = true
		}
		if j.Status == pipeline.StatusWaiting || j.Status == pipeline.StatusRunning {
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

	for ji, j := range m.pipeline.Jobs {
		for si, s := range j.Steps {
			if s.Status != pipeline.StatusFailed || m.seenFailed[s] {
				continue
			}
			m.seenFailed[s] = true
			m.folded[ji] = false
			m.cursor = cursorPos{ji, si}
			m.focus = focusLog
			m.autoFollow = false
			m.lastLogLen = -1
			m.adjustTreeOffset()
			return
		}
	}

	if m.autoFollow {
		for ji, j := range m.pipeline.Jobs {
			for si, s := range j.Steps {
				if s.Status == pipeline.StatusRunning {
					if m.cursor.jobIdx != ji || m.cursor.stepIdx != si {
						m.folded[ji] = false
						m.cursor = cursorPos{ji, si}
						m.lastLogLen = -1
					}
					m.adjustTreeOffset()
					return
				}
			}
		}
	}
}

func (m *Model) jumpToFirstFailure() {
	for ji, j := range m.pipeline.Jobs {
		for si, s := range j.Steps {
			if s.Status != pipeline.StatusFailed {
				continue
			}
			m.folded[ji] = false
			m.cursor = cursorPos{ji, si}
			m.focus = focusLog
			m.lastLogLen = -1
			m.syncLogContent(true)
			m.adjustTreeOffset()
			return
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

func (m *Model) rerunStep(jobIdx, stepIdx int) {
	if jobIdx < 0 || jobIdx >= len(m.pipeline.Jobs) {
		return
	}
	j := m.pipeline.Jobs[jobIdx]
	if stepIdx < 0 || stepIdx >= len(j.Steps) {
		return
	}
	delete(m.seenFailed, j.Steps[stepIdx])
	m.pipelineDone = false
	m.prevAllDone = false
	m.autoFollow = false
	m.pipeline.RerunStep(jobIdx, stepIdx)
}

func (m *Model) rerunAll() {
	for _, j := range m.pipeline.Jobs {
		for _, s := range j.Steps {
			delete(m.seenFailed, s)
		}
	}
	m.pipelineDone = false
	m.prevAllDone = false
	m.autoFollow = true
	m.pipeline.RerunAll()
}

// navRow is a navigable tree row (job or step).
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

// cursorLineInTree returns the rendered line index of the cursor within allLines,
// accounting for card border lines, step rows, hint rows, and inter-card gaps.
func (m *Model) cursorLineInTree() int {
	line := 0
	for ji, j := range m.pipeline.Jobs {
		line++ // top border of card
		// job header line is at 'line'
		if m.cursor.jobIdx == ji && m.cursor.isJob() {
			return line
		}
		line++ // job header
		if !m.folded[ji] {
			for si, s := range j.Steps {
				if m.cursor.jobIdx == ji && m.cursor.stepIdx == si {
					return line
				}
				line++ // step row
				if s.Status == pipeline.StatusFailed && lastErrLine(s) != "" {
					line++ // error hint row
				}
			}
		}
		line++ // bottom border of card
		if ji < len(m.pipeline.Jobs)-1 {
			line++ // blank gap between cards
		}
	}
	return 0
}

// adjustTreeOffset keeps the cursor visible within the tree pane.
func (m *Model) adjustTreeOffset() {
	if m.height == 0 {
		return
	}
	innerH := m.height - 4
	if innerH < 3 {
		innerH = 3
	}
	curLine := m.cursorLineInTree()
	if curLine < m.treeOffset {
		m.treeOffset = curLine
	} else if curLine >= m.treeOffset+innerH {
		m.treeOffset = curLine - innerH + 1
	}
	if m.treeOffset < 0 {
		m.treeOffset = 0
	}
}

func (m *Model) jumpToJob(dir int) {
	jobs := m.pipeline.Jobs
	if len(jobs) == 0 {
		return
	}
	next := clamp(m.cursor.jobIdx+dir, 0, len(jobs)-1)
	m.cursor = cursorPos{next, -1}
	m.lastLogLen = -1
	m.syncLogContent(true)
	m.adjustTreeOffset()
}

func (m *Model) moveCursor(d int) {
	rows := m.navRows()
	if len(rows) == 0 {
		return
	}
	idx := clamp(m.cursorIndex()+d, 0, len(rows)-1)
	r := rows[idx]
	//nolint:staticcheck
	m.cursor = cursorPos{r.jobIdx, r.stepIdx}
	m.lastLogLen = -1
	m.syncLogContent(true)
	m.adjustTreeOffset()
}

func (m *Model) selectedStep() *pipeline.Step {
	if m.cursor.isJob() || m.cursor.jobIdx >= len(m.pipeline.Jobs) {
		return nil
	}
	j := m.pipeline.Jobs[m.cursor.jobIdx]
	if m.cursor.stepIdx >= 0 && m.cursor.stepIdx < len(j.Steps) {
		return j.Steps[m.cursor.stepIdx]
	}
	return nil
}

func (m *Model) rightPaneWidth() int {
	if m.fullLog {
		return m.width - 4
	}
	return m.width - leftWidth - 4
}

func (m *Model) logVPWidth() int {
	return m.rightPaneWidth() - 2
}

func (m *Model) logVPHeight() int {
	h := m.height - 5
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
	queryChanged := m.searchQuery != m.lastSearchQuery
	if newLen == m.lastLogLen && !queryChanged && !forceBottom {
		return
	}
	q := strings.ToLower(m.searchQuery)
	currentLine := -1
	if q != "" && len(m.searchMatches) > 0 {
		currentLine = m.searchMatches[m.searchCurrent]
	}
	var sb strings.Builder
	for i, line := range logs {
		switch {
		case q != "" && strings.Contains(strings.ToLower(line), q) && i == currentLine:
			sb.WriteString(highlightLine(line, q, &styleLogSearchCur))
		case q != "" && strings.Contains(strings.ToLower(line), q):
			sb.WriteString(highlightLine(line, q, &styleLogSearch))
		case strings.HasPrefix(line, "$"):
			sb.WriteString(styleLogCmd.Render(line))
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
	m.lastSearchQuery = m.searchQuery
}

// pipelineElapsed returns the wall-clock span of the current run.
// For running pipelines this grows; for completed ones it is fixed.
func (m *Model) pipelineElapsed() time.Duration {
	var start, end time.Time
	for _, j := range m.pipeline.Jobs {
		for _, s := range j.Steps {
			if s.StartTime.IsZero() {
				continue
			}
			if start.IsZero() || s.StartTime.Before(start) {
				start = s.StartTime
			}
			e := s.EndTime
			if e.IsZero() {
				e = time.Now()
			}
			if e.After(end) {
				end = e
			}
		}
	}
	if start.IsZero() {
		return 0
	}
	return end.Sub(start)
}

// View renders the full TUI frame.
func (m *Model) View() string {
	if m.width == 0 {
		return "loading…"
	}

	var right string
	switch {
	case m.mode == viewTimeline:
		right = m.renderTimeline()
	case m.cursor.isJob():
		right = m.renderJobSummary()
	default:
		right = m.renderLogPane()
	}

	if m.fullLog {
		return lipgloss.JoinVertical(lipgloss.Left, right, m.renderStatusBar())
	}

	left := m.renderTree()
	return lipgloss.JoinVertical(lipgloss.Left,
		lipgloss.JoinHorizontal(lipgloss.Top, left, right),
		m.renderStatusBar(),
	)
}

// paneStyle returns the border style for a pane, applying the completion flash.
func (m *Model) paneStyle(side focusPane) lipgloss.Style {
	var style lipgloss.Style
	if side == focusTree {
		style = stylePaneTree
		if m.focus == side {
			style = stylePaneTreeFocused
		}
	} else {
		style = stylePane
		if m.focus == side {
			style = stylePaneFocused
		}
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
	paneH := m.height - 4

	var allLines []string
	for ji, j := range m.pipeline.Jobs {
		card := m.renderJobCard(ji, j)
		allLines = append(allLines, strings.Split(card, "\n")...)
		if ji < len(m.pipeline.Jobs)-1 {
			allLines = append(allLines, "") // gap between cards
		}
	}

	start := m.treeOffset
	if start < 0 {
		start = 0
	}
	if start < len(allLines) {
		allLines = allLines[start:]
	}

	return m.paneStyle(focusTree).Width(leftWidth).Height(paneH).Render(strings.Join(allLines, "\n"))
}

func (m *Model) renderJobCard(ji int, j *pipeline.Job) string {
	onJob := m.cursor.jobIdx == ji && m.cursor.isJob()
	cursorOnCard := m.cursor.jobIdx == ji
	folded := m.folded[ji]
	jCol := jobColor(ji)

	// Right side of header: N/M fold [dur]
	done := 0
	for _, s := range j.Steps {
		if s.Status == pipeline.StatusPassed || s.Status == pipeline.StatusFailed || s.Status == pipeline.StatusSkipped {
			done++
		}
	}
	progress := styleDim.Render(fmt.Sprintf("%d/%d", done, len(j.Steps)))
	foldMark := styleDim.Render("▾")
	if folded {
		foldMark = styleDim.Render("▸")
	}
	var rightSide string
	if j.Status == pipeline.StatusRunning || j.Status == pipeline.StatusPassed || j.Status == pipeline.StatusFailed {
		rightSide = progress + " " + foldMark + " " + styleDim.Render(durStr(j.Duration()))
	} else {
		rightSide = progress + " " + foldMark
	}
	rightW := lipgloss.Width(rightSide)

	// Header layout: cur(2) + icon(1) + sp(1) + name(nameW) + right(rightW) = cardContentW
	// Force the name column to an exact width so padding is trivially correct.
	cur := "  "
	if onJob {
		cur = styleCursor.Render("▶ ")
	}
	var nameStyle lipgloss.Style
	if onJob {
		nameStyle = lipgloss.NewStyle().Foreground(jCol).Bold(true)
	} else {
		nameStyle = lipgloss.NewStyle().Foreground(jCol)
	}
	nameW := cardContentW - 4 - rightW
	if nameW < 1 {
		nameW = 1
	}
	nameRendered := nameStyle.Width(nameW).Render(truncate(j.Name, nameW))
	header := fmt.Sprintf("%s%s %s%s", cur, statusIcon(j.Status, m.tick), nameRendered, rightSide)

	// Step layout: " "(1) + treeConn(2) + " "(1) + cur(2) + name(stepNameW) + badge(badgeWidth) = cardContentW
	const stepPrefix = 6
	stepNameW := cardContentW - stepPrefix - badgeWidth

	var rows []string
	if !folded {
		for si, s := range j.Steps {
			onStep := m.cursor.jobIdx == ji && m.cursor.stepIdx == si
			isLast := si == len(j.Steps)-1
			stepCur := "  "
			if onStep {
				stepCur = styleCursor.Render("▶ ")
			}
			sNameStyle := styleStepName
			if onStep {
				sNameStyle = styleStepNameActive
			}
			conn := styleDim.Render("├─")
			if isLast {
				conn = styleDim.Render("└─")
			}
			badge := statusBadge(s, m.tick)
			sNameRendered := sNameStyle.Width(stepNameW).Render(truncate(s.Name, stepNameW))
			rows = append(rows, fmt.Sprintf(" %s %s%s%s", conn, stepCur, sNameRendered, badge))
			if s.Status == pipeline.StatusFailed {
				if hint := lastErrLine(s); hint != "" {
					contConn := styleDim.Render("│")
					if isLast {
						contConn = " "
					}
					rows = append(rows, fmt.Sprintf(" %s    %s", contConn, styleErrHint.Render("↳ "+truncate(hint, cardContentW-8))))
				}
			}
		}
	}

	content := header
	if len(rows) > 0 {
		content += "\n" + strings.Join(rows, "\n")
	}

	borderCol := colBorder
	if cursorOnCard {
		borderCol = jCol
	}
	if m.flashTick > 0 {
		if m.flashPassed {
			borderCol = colBorderOK
		} else {
			borderCol = colBorderErr
		}
	}
	// No Width() here — setting Width triggers lipgloss word-wrap which
	// splits trailing pad-spaces from the name column and pushes the badge
	// to the next line. Content lines are already exactly cardContentW wide,
	// so the border draws at the right size without enforcement.
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(borderCol).
		Padding(0, 1).
		Render(content)
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

	rightW := m.rightPaneWidth()
	paneH := m.height - 4
	return m.paneStyle(focusLog).Width(rightW).Height(paneH).Render(header + "\n" + m.logVP.View())
}

func (m *Model) renderJobSummary() string {
	rightW := m.rightPaneWidth()
	paneH := m.height - 4
	innerW := rightW - 4

	if m.cursor.jobIdx >= len(m.pipeline.Jobs) {
		return m.paneStyle(focusLog).Width(rightW).Height(paneH).Render("")
	}
	j := m.pipeline.Jobs[m.cursor.jobIdx]

	var sb strings.Builder

	if m.pipelineDone {
		if m.flashPassed {
			fmtx.Fprintf(&sb, " %s\n\n", lipgloss.NewStyle().Foreground(colGreen).Bold(true).Render("✓ all jobs passed"))
		} else {
			fmtx.Fprintf(&sb, " %s\n\n", lipgloss.NewStyle().Foreground(colRed).Bold(true).Render("✗ pipeline failed"))
		}
	}
	fmtx.Fprintf(&sb, " %s\n\n", lipgloss.NewStyle().Foreground(jobColor(m.cursor.jobIdx)).Bold(true).Render(j.Name))

	for _, s := range j.Steps {
		badge := statusBadge(s, m.tick)
		name := truncate(s.Name, innerW-2-badgeWidth-1)
		row := fmt.Sprintf("  %s", styleStepName.Render(name))
		if pad := innerW - lipgloss.Width(row) - badgeWidth; pad >= 1 {
			row += strings.Repeat(" ", pad) + badge
		} else {
			row += " " + badge
		}
		fmtx.Fprintf(&sb, "%s\n", row)
		if s.Status == pipeline.StatusFailed {
			if hint := lastErrLine(s); hint != "" {
				fmtx.Fprintf(&sb, "    %s\n", styleErrHint.Render("↳ "+truncate(hint, innerW-6)))
			}
		}
	}

	return m.paneStyle(focusLog).Width(rightW).Height(paneH).Render(sb.String())
}

func (m *Model) renderTimeline() string {
	rightW := m.rightPaneWidth()
	paneH := m.height - 4
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
	const stepIndent = 2
	barW := innerW - labelW - 2 - 7
	if barW < 8 {
		barW = 8
	}

	ctx := timelineCtx{globalStart: globalStart, totalDur: totalDur, labelW: labelW, barW: barW}

	var sb strings.Builder
	fmtx.Fprintf(&sb, " Timeline  %s total\n\n", durStr(totalDur))

	for ji, j := range m.pipeline.Jobs {
		jStart, jEnd := jobTimeBounds(j)
		if jStart.IsZero() {
			label := lipgloss.NewStyle().Foreground(jobColor(ji)).Render(fmt.Sprintf("%-*s", labelW, truncate(j.Name, labelW)))
			fmtx.Fprintf(&sb, "  %s  %s\n", label, styleDim.Render("·"))
			continue
		}
		if jEnd.IsZero() {
			jEnd = time.Now()
		}
		sb.WriteString(renderTimelineBar(&timelineBarOpts{
			timelineCtx: ctx,
			name:        j.Name,
			start:       jStart,
			end:         jEnd,
			indent:      0,
			status:      j.Status,
			barChar:     "█",
			labelColor:  jobColor(ji),
		}))
		for _, s := range j.Steps {
			row := renderTimelineStepRow(ctx, s, stepIndent)
			if row != "" {
				sb.WriteString(row)
			}
		}
	}

	axisRight := durStr(totalDur)
	gap := barW - 2 - len(axisRight)
	if gap < 0 {
		gap = 0
	}
	fmtx.Fprintf(&sb, "\n%s", styleDim.Render(
		fmt.Sprintf("  %s  0s%s%s", strings.Repeat(" ", labelW), strings.Repeat(" ", gap), axisRight),
	))

	return style.Width(rightW).Height(paneH).Render(sb.String())
}

func (m *Model) renderStatusBar() string {
	if m.searchMode {
		return m.renderSearchBar()
	}

	var running, passed, failed int
	for _, j := range m.pipeline.Jobs {
		switch j.Status {
		case pipeline.StatusRunning:
			running++
		case pipeline.StatusPassed:
			passed++
		case pipeline.StatusFailed:
			failed++
		}
	}

	elapsed := m.pipelineElapsed()

	var statusParts []string
	if running > 0 {
		statusParts = append(statusParts, lipgloss.NewStyle().Foreground(colAmber).Render(fmt.Sprintf("● %d running", running)))
	}
	if passed > 0 {
		statusParts = append(statusParts, lipgloss.NewStyle().Foreground(colGreen).Render(fmt.Sprintf("✓ %d passed", passed)))
	}
	if failed > 0 {
		statusParts = append(statusParts, lipgloss.NewStyle().Foreground(colRed).Render(fmt.Sprintf("✗ %d failed", failed)))
	}
	if elapsed > 0 {
		statusParts = append(statusParts, styleDim.Render("⏱ "+durStr(elapsed)))
	}

	// Current selection breadcrumb on the right of line 1.
	var selInfo string
	if m.cursor.jobIdx < len(m.pipeline.Jobs) {
		j := m.pipeline.Jobs[m.cursor.jobIdx]
		jLabel := lipgloss.NewStyle().Foreground(jobColor(m.cursor.jobIdx)).Render(j.Name)
		if m.cursor.isJob() {
			selInfo = jLabel
		} else if m.cursor.stepIdx < len(j.Steps) {
			s := j.Steps[m.cursor.stepIdx]
			selInfo = jLabel + styleDim.Render(" › ") + lipgloss.NewStyle().Foreground(colText).Render(s.Name)
		}
	}

	line1Left := "  " + strings.Join(statusParts, "  ")
	line1Right := selInfo + "  "
	pad1 := m.width - lipgloss.Width(line1Left) - lipgloss.Width(line1Right)
	if pad1 < 0 {
		pad1 = 0
	}
	line1 := line1Left + strings.Repeat(" ", pad1) + line1Right

	pane := "tree"
	if m.focus == focusLog {
		pane = "logs"
	}
	viewToggle := "t timeline"
	if m.mode == viewTimeline {
		viewToggle = "t normal"
	}
	zLabel := "z zoom"
	if m.fullLog {
		zLabel = "z split"
	}
	hints := []string{
		fmt.Sprintf("tab[%s]", pane),
		"↑↓/jk nav", "J/K jobs", "h/l fold",
		"f fail", "r rerun", "R reload",
		viewToggle, zLabel,
		"/ search",
		"ctrl+c quit",
	}
	line2 := styleHelp.Width(m.width).Render("  " + strings.Join(hints, "  ·  "))

	return line1 + "\n" + line2
}

func (m *Model) renderSearchBar() string {
	cursor := lipgloss.NewStyle().Foreground(colAmber).Render("|")
	prompt := styleDim.Render("/") + " " +
		lipgloss.NewStyle().Foreground(colText).Render(m.searchQuery) + cursor

	var matchInfo string
	switch {
	case len(m.searchMatches) > 0:
		matchInfo = styleDim.Render(fmt.Sprintf("  [%d/%d]", m.searchCurrent+1, len(m.searchMatches)))
	case m.searchQuery != "":
		matchInfo = lipgloss.NewStyle().Foreground(colRed).Render("  no matches")
	}

	hints := styleHelp.Render("Enter confirm  Esc cancel  ")
	line1Left := "  " + prompt + matchInfo
	pad := m.width - lipgloss.Width(line1Left) - lipgloss.Width(hints)
	if pad < 0 {
		pad = 0
	}
	line1 := line1Left + strings.Repeat(" ", pad) + hints
	line2 := styleHelp.Width(m.width).Render("")
	return line1 + "\n" + line2
}

// timelineCtx holds shared timeline geometry passed to bar-rendering helpers.
type timelineCtx struct {
	globalStart time.Time
	totalDur    time.Duration
	labelW      int
	barW        int
}

// timelineBarOpts parameterizes a single timeline bar row.
type timelineBarOpts struct {
	timelineCtx
	name       string
	start      time.Time
	end        time.Time
	indent     int
	status     pipeline.Status
	barChar    string
	labelColor lipgloss.Color
}

func renderTimelineBar(o *timelineBarOpts) string {
	offsetFrac := float64(o.start.Sub(o.globalStart)) / float64(o.totalDur)
	durFrac := float64(o.end.Sub(o.start)) / float64(o.totalDur)
	lead := int(offsetFrac * float64(o.barW))
	barLen := int(durFrac * float64(o.barW))
	if barLen < 1 {
		barLen = 1
	}
	if lead+barLen > o.barW {
		barLen = o.barW - lead
	}
	bar := lipgloss.NewStyle().Foreground(statusBarColor(o.status)).Render(strings.Repeat(o.barChar, barLen))
	labelStyle := styleDim
	if o.labelColor != "" {
		labelStyle = lipgloss.NewStyle().Foreground(o.labelColor)
	}
	label := labelStyle.Render(fmt.Sprintf("%-*s", o.labelW-o.indent, truncate(o.name, o.labelW-o.indent)))
	return fmt.Sprintf("  %s%s  %s%s  %s\n",
		strings.Repeat(" ", o.indent), label,
		strings.Repeat(" ", lead), bar,
		styleDim.Render(durStr(o.end.Sub(o.start))))
}

func renderTimelineStepRow(ctx timelineCtx, s *pipeline.Step, indent int) string {
	if s.StartTime.IsZero() {
		return ""
	}
	sEnd := s.EndTime
	if sEnd.IsZero() && s.Status == pipeline.StatusRunning {
		sEnd = time.Now()
	}
	if sEnd.IsZero() {
		return ""
	}
	return renderTimelineBar(&timelineBarOpts{
		timelineCtx: ctx,
		name:        s.Name,
		start:       s.StartTime,
		end:         sEnd,
		indent:      indent,
		status:      s.Status,
		barChar:     "▪",
	})
}

func jobTimeBounds(j *pipeline.Job) (start, end time.Time) {
	for _, s := range j.Steps {
		if !s.StartTime.IsZero() {
			if start.IsZero() || s.StartTime.Before(start) {
				start = s.StartTime
			}
		}
		e := s.EndTime
		if e.IsZero() && s.Status == pipeline.StatusRunning {
			e = time.Now()
		}
		if !e.IsZero() && e.After(end) {
			end = e
		}
	}
	return start, end
}

func statusBarColor(s pipeline.Status) lipgloss.Color {
	switch s {
	case pipeline.StatusPassed:
		return colGreen
	case pipeline.StatusFailed:
		return colRed
	case pipeline.StatusRunning:
		return colAmber
	default:
		return colMuted
	}
}

func truncate(s string, fMax int) string {
	if fMax < 1 {
		return ""
	}
	if len(s) <= fMax {
		return s
	}
	return s[:fMax-1] + "…"
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

// highlightLine renders a log line with each occurrence of query (already lowercased)
// highlighted using matchStyle; non-matching segments use normal text style.
func highlightLine(line, query string, matchStyle *lipgloss.Style) string {
	lower := strings.ToLower(line)
	var sb strings.Builder
	remaining := line
	lowerRem := lower
	for {
		idx := strings.Index(lowerRem, query)
		if idx < 0 {
			sb.WriteString(styleLogText.Render(remaining))
			break
		}
		if idx > 0 {
			sb.WriteString(styleLogText.Render(remaining[:idx]))
		}
		sb.WriteString(matchStyle.Render(remaining[idx : idx+len(query)]))
		remaining = remaining[idx+len(query):]
		lowerRem = lowerRem[idx+len(query):]
	}
	return sb.String()
}

// lastErrLine returns the last non-empty, non-command log line from a step.
func lastErrLine(s *pipeline.Step) string {
	logs := s.GetLogs()
	for i := len(logs) - 1; i >= 0; i-- {
		line := strings.TrimSpace(logs[i])
		if line != "" && !strings.HasPrefix(line, "$") {
			return line
		}
	}
	return ""
}
