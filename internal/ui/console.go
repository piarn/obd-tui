package ui

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"lazyobd/internal/worker"
)

type lineKind int

const (
	lineCmd lineKind = iota
	lineResp
	lineErr
	lineSys
)

type conLine struct {
	kind lineKind
	text string
	at   time.Time
}

type consoleState struct {
	lines   []conLine
	input   textinput.Model
	hist    []string
	histPos int
	off     int
	follow  bool
	search  string
	matches []int
	cur     int
	pending int
	th      Theme
}

func newConsoleState() consoleState {
	in := textinput.New()
	in.Prompt = "› "
	in.Placeholder = "AT or OBD command, e.g. ATRV · 010C · 03 · 0902"
	c := consoleState{input: in, follow: true}
	c.add(lineSys, "raw console — press i to type, enter to send, esc for normal mode")
	c.add(lineSys, "useful: ATI (version) · ATRV (battery) · ATDP (protocol) · 0100 (PIDs) · 03 (codes) · 0902 (VIN)")
	return c
}

func (c *consoleState) applyTheme(t Theme) {
	c.th = t
	s := c.input.Styles()
	s.Focused.Prompt = lipgloss.NewStyle().Foreground(t.Good).Bold(true)
	s.Blurred.Prompt = t.Dim
	s.Focused.Text = t.Text
	s.Blurred.Text = t.Dim
	s.Focused.Placeholder = t.Faint
	s.Blurred.Placeholder = t.Faint
	c.input.SetStyles(s)
}

func (c *consoleState) add(k lineKind, text string) {
	for _, l := range strings.Split(text, "\n") {
		c.lines = append(c.lines, conLine{k, l, time.Now()})
	}
	if len(c.lines) > 5000 {
		c.lines = c.lines[len(c.lines)-5000:]
	}
	if c.search != "" {
		c.setSearch(c.search)
	}
}

func (c *consoleState) result(m worker.RawMsg) {
	c.pending--
	if m.Resp != "" {
		c.add(lineResp, m.Resp)
	}
	if m.Err != nil {
		c.add(lineErr, m.Err.Error())
	}
	c.add(lineSys, fmt.Sprintf("%dms", m.Took.Milliseconds()))
}

// insertKey handles keys in insert mode; returns leave=true to go to normal.
func (c *consoleState) insertKey(msg tea.KeyPressMsg, w *worker.Worker) (tea.Cmd, bool) {
	switch msg.String() {
	case "esc":
		c.input.Blur()
		return nil, true
	case "enter":
		cmd := strings.TrimSpace(c.input.Value())
		if cmd == "" { // a bare CR would repeat the last command on the ELM
			return nil, false
		}
		c.input.SetValue("")
		c.add(lineCmd, cmd)
		if len(c.hist) == 0 || c.hist[len(c.hist)-1] != cmd {
			c.hist = append(c.hist, cmd)
		}
		c.histPos = len(c.hist)
		c.follow = true
		c.pending++
		w.Request(worker.ReqRaw{Cmd: cmd})
		return nil, false
	case "ctrl+p", "up":
		if c.histPos > 0 {
			c.histPos--
			c.input.SetValue(c.hist[c.histPos])
			c.input.CursorEnd()
		}
		return nil, false
	case "ctrl+n", "down":
		if c.histPos < len(c.hist)-1 {
			c.histPos++
			c.input.SetValue(c.hist[c.histPos])
		} else {
			c.histPos = len(c.hist)
			c.input.SetValue("")
		}
		c.input.CursorEnd()
		return nil, false
	}
	var cmd tea.Cmd
	c.input, cmd = c.input.Update(msg)
	return cmd, false
}

func (c *consoleState) viewHeight(h int) int { return max(1, h-4) }

func (c *consoleState) motion(mv string, h int) {
	vh := c.viewHeight(h)
	maxOff := max(0, len(c.lines)-vh)
	if c.follow {
		c.off = maxOff
	}
	switch mv {
	case "down":
		c.off++
	case "up":
		c.off--
	case "top":
		c.off = 0
	case "bottom":
		c.off = maxOff
	case "halfdown":
		c.off += vh / 2
	case "halfup":
		c.off -= vh / 2
	}
	c.off = clampInt(c.off, 0, maxOff)
	c.follow = c.off == maxOff
}

func (c *consoleState) setSearch(q string) {
	c.search = q
	c.matches = nil
	if q == "" {
		return
	}
	lq := strings.ToLower(q)
	for i, l := range c.lines {
		if strings.Contains(strings.ToLower(l.text), lq) {
			c.matches = append(c.matches, i)
		}
	}
	c.cur = max(0, len(c.matches)-1)
}

func (c *consoleState) jump(dir, h int) {
	if len(c.matches) == 0 {
		return
	}
	c.cur = (c.cur + dir + len(c.matches)) % len(c.matches)
	vh := c.viewHeight(h)
	c.off = clampInt(c.matches[c.cur]-vh/2, 0, max(0, len(c.lines)-vh))
	c.follow = false
}

func (m *Model) consoleView(w, h int) string {
	t := m.th
	c := &m.con
	vh := c.viewHeight(h)
	maxOff := max(0, len(c.lines)-vh)
	if c.follow {
		c.off = maxOff
	}
	c.off = clampInt(c.off, 0, maxOff)

	var hl *regexp.Regexp
	if c.search != "" {
		hl = regexp.MustCompile("(?i)" + regexp.QuoteMeta(c.search))
	}
	mark := lipgloss.NewStyle().Background(t.Warn).Foreground(t.OnAccent)
	iw := w - 4
	var lines []string
	for i := c.off; i < min(len(c.lines), c.off+vh); i++ {
		l := c.lines[i]
		text := l.text
		var st lipgloss.Style
		prefix := "  "
		switch l.kind {
		case lineCmd:
			st, prefix = t.fg(t.Accent).Bold(true), t.fg(t.Accent).Render("› ")
		case lineResp:
			st = t.Text
		case lineErr:
			st, prefix = t.fg(t.Bad), t.fg(t.Bad).Render("✗ ")
		case lineSys:
			st = t.Faint
		}
		var rendered string
		if hl != nil && hl.MatchString(text) {
			idx := hl.FindAllStringIndex(text, -1)
			last := 0
			for _, r := range idx {
				rendered += st.Render(text[last:r[0]]) + mark.Render(text[r[0]:r[1]])
				last = r[1]
			}
			rendered += st.Render(text[last:])
		} else {
			rendered = st.Render(text)
		}
		ts := t.Faint.Render(l.at.Format("15:04:05") + " ")
		if l.kind == lineSys && strings.HasSuffix(text, "ms") {
			ts = "         "
		}
		lines = append(lines, ts+prefix+rendered)
	}
	for len(lines) < vh {
		lines = append(lines, "")
	}
	lines = append(lines, t.Faint.Render(strings.Repeat("─", iw)))
	c.input.SetWidth(iw - 4)
	inputLine := c.input.View()
	if c.pending > 0 {
		inputLine = spread(inputLine, m.spin.View(), iw)
	}
	lines = append(lines, inputLine)

	right := ""
	if c.search != "" {
		if len(c.matches) == 0 {
			right = t.fg(t.Bad).Render("/" + c.search + " no match")
		} else {
			right = fmt.Sprintf("/%s %d/%d", c.search, c.cur+1, len(c.matches))
		}
	} else if !c.follow {
		right = fmt.Sprintf("%d%%", 100*(c.off+vh)/max(1, len(c.lines)))
	}
	border := t.Accent
	if m.mode == modeInsert {
		border = t.Good
	}
	return panel(t, "Console", right, lines, w, h, border, true)
}
