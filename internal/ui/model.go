// Package ui is the Bubble Tea front-end.
package ui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/spinner"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"lazyobd/internal/elm"
	"lazyobd/internal/logcsv"
	"lazyobd/internal/obd"
	"lazyobd/internal/vehicle"
	"lazyobd/internal/worker"
)

// confirmState is the pending y/N question.
type confirmState struct {
	title  string
	text   string
	note   string
	yes    string
	action func() tea.Cmd
}

func (m *Model) ask(c confirmState) {
	m.confirm = c
	m.mode = modeConfirm
}

const (
	tabDash = iota
	tabCodes
	tabModules
	tabInfo
	tabConsole
)

var tabNames = []string{"Dashboard", "Codes", "Modules", "Info", "Console"}

type mode int

const (
	modeNormal mode = iota
	modeInsert
	modeFilter
	modeConfirm
	modeHelp
)

// Options configure the UI.
type Options struct {
	LogPath string // start recording to this CSV once sensors are known
	Sim     bool
}

// Model is the root Bubble Tea model.
type Model struct {
	w      *worker.Worker
	cancel context.CancelFunc
	opts   Options
	th     Theme
	help   help.Model

	width, height int
	tab           int
	mode          mode
	pendingG      bool

	state   worker.State
	detail  string
	lastErr string // last failure, kept on screen while retrying
	info    elm.Info
	spin    spinner.Model
	voltage float64
	hasVolt bool
	status  *obd.Status
	vehicle *obd.Vehicle
	vehErr  error

	filter textinput.Model

	dash  dashState
	codes codesState
	mods  modState

	make     vehicle.Make
	year     int
	override vehicle.Group // "" = follow the VIN

	confirm confirmState
	info2   infoState
	con     consoleState

	logger *logcsv.Logger
	paused bool

	toast     string
	toastBad  bool
	toastSeq  int
	pollCount int
	pollRate  float64
	rateMark  time.Time
}

// New builds the model; the worker must already be running.
func New(w *worker.Worker, cancel context.CancelFunc, opts Options) *Model {
	th := NewTheme(true)
	f := textinput.New()
	f.Prompt = "/"
	f.Placeholder = "filter…"
	m := &Model{
		w: w, cancel: cancel, opts: opts, th: th, help: help.New(),
		spin:   spinner.New(spinner.WithSpinner(spinner.MiniDot)),
		filter: f,
		dash:   newDashState(),
		con:    newConsoleState(),
	}
	m.applyTheme(true)
	return m
}

func (m *Model) applyTheme(dark bool) {
	m.th = NewTheme(dark)
	s := help.DefaultStyles(dark)
	s.ShortKey = lipgloss.NewStyle().Foreground(m.th.Accent).Bold(true)
	s.ShortDesc = m.th.Dim
	s.ShortSeparator = m.th.Faint
	m.help.Styles = s
	m.help.ShortSeparator = " · "
	m.spin.Style = lipgloss.NewStyle().Foreground(m.th.Accent)
	m.con.applyTheme(m.th)
	fs := m.filter.Styles()
	fs.Focused.Prompt = lipgloss.NewStyle().Foreground(m.th.Warn).Bold(true)
	fs.Focused.Text = m.th.Text
	fs.Blurred.Prompt = m.th.Dim
	m.filter.SetStyles(fs)
}

func (m *Model) listen() tea.Cmd {
	return func() tea.Msg { return <-m.w.Out }
}

func (m *Model) Init() tea.Cmd {
	return tea.Batch(m.listen(), m.spin.Tick, tea.RequestBackgroundColor)
}

type toastExpireMsg struct{ seq int }

func (m *Model) setToast(s string, bad bool) tea.Cmd {
	m.toast, m.toastBad = s, bad
	m.toastSeq++
	seq := m.toastSeq
	return tea.Tick(4*time.Second, func(time.Time) tea.Msg { return toastExpireMsg{seq} })
}

func (m *Model) quit() (tea.Model, tea.Cmd) {
	if m.logger != nil {
		m.logger.Close()
	}
	m.cancel()
	return m, tea.Quit
}

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.help.SetWidth(msg.Width)
		return m, nil
	case tea.BackgroundColorMsg:
		m.applyTheme(msg.IsDark())
		return m, nil
	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spin, cmd = m.spin.Update(msg)
		return m, cmd
	case toastExpireMsg:
		if msg.seq == m.toastSeq {
			m.toast = ""
		}
		return m, nil
	case tea.KeyPressMsg:
		return m.handleKey(msg)
	case tea.PasteMsg:
		return m.routeInput(msg)
	}
	if cmd, ok := m.handleWorker(msg); ok {
		return m, tea.Batch(cmd, m.listen())
	}
	return m.routeInput(msg)
}

// routeInput forwards non-key messages (cursor blink etc.) to focused inputs.
func (m *Model) routeInput(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	switch m.mode {
	case modeFilter:
		m.filter, cmd = m.filter.Update(msg)
		m.applyFilter()
	case modeInsert:
		m.con.input, cmd = m.con.input.Update(msg)
	}
	return m, cmd
}

func (m *Model) handleWorker(msg tea.Msg) (tea.Cmd, bool) {
	switch msg := msg.(type) {
	case worker.StatusMsg:
		m.state, m.detail = msg.State, msg.Detail
		if msg.Info.Port != "" || msg.Info.Version != "" {
			m.info = msg.Info
		}
		switch msg.State {
		case worker.Connected:
			m.lastErr = ""
			m.con.add(lineSys, fmt.Sprintf("connected · %s · %s", m.info.Version, m.info.ProtocolName))
			return m.setToast("Connected: "+m.info.ProtocolName, false), true
		case worker.Failed:
			if msg.Detail != m.lastErr {
				m.con.add(lineErr, msg.Detail)
			}
			m.lastErr = msg.Detail
		}
		return nil, true
	case worker.SupportedMsg:
		m.dash.setPIDs(msg.PIDs)
		if msg.Err != nil && len(msg.PIDs) == 0 {
			m.dash.err = msg.Err
		}
		if m.opts.LogPath != "" && m.logger == nil && len(msg.PIDs) > 0 {
			return m.startLog(m.opts.LogPath), true
		}
		return nil, true
	case worker.ValueMsg:
		m.dash.update(msg)
		m.pollCount++
		if el := time.Since(m.rateMark); el > 2*time.Second {
			m.pollRate = float64(m.pollCount) / el.Seconds()
			m.pollCount, m.rateMark = 0, time.Now()
		}
		return nil, true
	case worker.CycleMsg:
		if m.logger != nil {
			if err := m.logger.Row(msg.At, m.dash.current()); err != nil {
				m.logger.Close()
				m.logger = nil
				return m.setToast("CSV write failed: "+err.Error(), true), true
			}
		}
		return nil, true
	case worker.VoltageMsg:
		m.voltage, m.hasVolt = msg.V, true
		return nil, true
	case worker.MonitorMsg:
		if msg.Err == nil {
			st := msg.Status
			m.status = &st
		}
		return nil, true
	case worker.VehicleMsg:
		if msg.Err == nil {
			v := msg.Vehicle
			m.vehicle = &v
			if v.VIN != "" {
				m.make, m.year = vehicle.Decode(v.VIN)
			}
		}
		m.vehErr = msg.Err
		return nil, true
	case worker.DTCMsg:
		m.codes.set(msg)
		m.codes.applyFilter(m.filterValue(tabCodes))
		return nil, true
	case worker.ClearMsg:
		if msg.Err != nil {
			return m.setToast("Clear failed: "+msg.Err.Error(), true), true
		}
		return m.setToast("Trouble codes cleared", false), true
	case worker.RawMsg:
		m.con.result(msg)
		return nil, true
	}
	return m.onModuleMsg(msg)
}

func (m *Model) startLog(path string) tea.Cmd {
	if len(m.dash.pids) == 0 {
		return m.setToast("No sensors to record yet", true)
	}
	l, err := logcsv.Start(path, m.dash.pids)
	if err != nil {
		return m.setToast("Cannot record: "+err.Error(), true)
	}
	m.logger = l
	return m.setToast("Recording to "+path, false)
}

func (m *Model) toggleLog() tea.Cmd {
	if m.logger != nil {
		p, n := m.logger.Path, m.logger.Rows
		m.logger.Close()
		m.logger = nil
		return m.setToast(fmt.Sprintf("Saved %d rows to %s", n, p), false)
	}
	return m.startLog(logcsv.DefaultPath())
}

func (m *Model) filterValue(tab int) string {
	switch tab {
	case tabDash:
		return m.dash.filter
	case tabCodes:
		return m.codes.filter
	}
	return ""
}

func (m *Model) applyFilter() {
	v := m.filter.Value()
	switch m.tab {
	case tabDash:
		m.dash.filter = v
		m.dash.clampSel()
	case tabCodes:
		m.codes.applyFilter(v)
	}
}

func (m *Model) bodyHeight() int { return max(1, m.height-2) }

func (m *Model) switchTab(t int) {
	m.tab = (t + len(tabNames)) % len(tabNames)
	m.pendingG = false
}

func (m *Model) handleKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	k := msg.String()
	if k == "ctrl+c" {
		return m.quit()
	}
	switch m.mode {
	case modeHelp:
		if k == "?" || k == "esc" || k == "q" || k == "enter" {
			m.mode = modeNormal
		}
		return m, nil
	case modeConfirm:
		switch {
		case key.Matches(msg, keys.Yes):
			m.mode = modeNormal
			if m.confirm.action != nil {
				return m, m.confirm.action()
			}
		case key.Matches(msg, keys.No):
			m.mode = modeNormal
		}
		return m, nil
	case modeFilter:
		switch k {
		case "esc":
			m.filter.SetValue("")
			m.applyFilter()
			m.filter.Blur()
			m.mode = modeNormal
			return m, nil
		case "enter":
			m.filter.Blur()
			m.mode = modeNormal
			if m.tab == tabConsole {
				m.con.setSearch(m.filter.Value())
				m.filter.SetValue("")
			}
			return m, nil
		}
		var cmd tea.Cmd
		m.filter, cmd = m.filter.Update(msg)
		if m.tab != tabConsole {
			m.applyFilter()
		}
		return m, cmd
	case modeInsert:
		cmd, leave := m.con.insertKey(msg, m.w)
		if leave {
			m.mode = modeNormal
		}
		return m, cmd
	}

	// Normal mode: two-key g-prefix (gg, gt, gT).
	if m.pendingG {
		m.pendingG = false
		switch k {
		case "g":
			m.motion("top")
		case "t":
			m.switchTab(m.tab + 1)
		case "T":
			m.switchTab(m.tab - 1)
		}
		return m, nil
	}
	switch {
	case k == "g":
		m.pendingG = true
		return m, nil
	case key.Matches(msg, keys.Quit):
		return m.quit()
	case key.Matches(msg, keys.Help):
		m.mode = modeHelp
		return m, nil
	case key.Matches(msg, keys.NextTab):
		m.switchTab(m.tab + 1)
		return m, nil
	case key.Matches(msg, keys.PrevTab):
		m.switchTab(m.tab - 1)
		return m, nil
	case key.Matches(msg, keys.Tabs):
		m.switchTab(int(k[0] - '1'))
		return m, nil
	case key.Matches(msg, keys.Pause):
		m.paused = !m.paused
		m.w.SetPaused(m.paused)
		if m.paused {
			return m, m.setToast("Live polling paused", false)
		}
		return m, m.setToast("Live polling resumed", false)
	case key.Matches(msg, keys.Record):
		return m, m.toggleLog()
	case key.Matches(msg, keys.Reconnect):
		m.w.Request(worker.ReqReconnect{})
		m.dash = newDashState()
		return m, m.setToast("Reconnecting…", false)
	case key.Matches(msg, keys.Filter):
		m.mode = modeFilter
		m.filter.Prompt = "/"
		if m.tab == tabConsole {
			m.filter.Placeholder = "search output…"
			m.filter.SetValue("")
		} else {
			m.filter.Placeholder = "filter…"
			m.filter.SetValue(m.filterValue(m.tab))
		}
		m.filter.CursorEnd()
		return m, m.filter.Focus()
	case key.Matches(msg, keys.Down):
		m.motion("down")
	case key.Matches(msg, keys.Up):
		m.motion("up")
	case key.Matches(msg, keys.Left):
		m.motion("left")
	case key.Matches(msg, keys.Right):
		m.motion("right")
	case key.Matches(msg, keys.Bottom):
		m.motion("bottom")
	case key.Matches(msg, keys.HalfDown):
		m.motion("halfdown")
	case key.Matches(msg, keys.HalfUp):
		m.motion("halfup")
	default:
		return m, m.tabKey(msg)
	}
	return m, nil
}

// motion dispatches a vim motion to the active tab.
func (m *Model) motion(mv string) {
	h := m.bodyHeight()
	switch m.tab {
	case tabDash:
		m.dash.motion(mv, m.width, h)
	case tabCodes:
		m.codes.motion(mv, h)
	case tabModules:
		m.mods.motion(mv, h)
	case tabInfo:
		m.info2.motion(mv, h)
	case tabConsole:
		m.con.motion(mv, h)
	}
}

// tabKey handles tab-specific keys in normal mode.
func (m *Model) tabKey(msg tea.KeyPressMsg) tea.Cmd {
	switch m.tab {
	case tabDash:
		switch {
		case key.Matches(msg, keys.Graph):
			m.dash.detail = !m.dash.detail && len(m.dash.visible()) > 0
		case key.Matches(msg, keys.Esc):
			if m.dash.detail {
				m.dash.detail = false
			} else if m.dash.filter != "" {
				m.dash.filter = ""
				m.dash.clampSel()
			}
		}
	case tabCodes:
		switch {
		case key.Matches(msg, keys.Refresh):
			m.codes.loading = true
			m.w.Request(worker.ReqDTCs{})
		case key.Matches(msg, keys.Clear):
			if m.state != worker.Connected {
				return m.setToast("Not connected", true)
			}
			m.ask(confirmState{
				title: "Clear all trouble codes?",
				text:  "This sends OBD service 04 to every ECU: stored and pending codes, freeze-frame data and emissions readiness monitors are erased. Monitors will read \"incomplete\" until the car has been driven through its drive cycles.",
				note:  "Do it with the ignition ON and the engine OFF.",
				yes:   "clear codes",
				action: func() tea.Cmd {
					m.codes.loading = true
					m.w.Request(worker.ReqClear{})
					return m.setToast("Clearing trouble codes…", false)
				},
			})
		case key.Matches(msg, keys.Esc):
			m.codes.applyFilter("")
		}
	case tabModules:
		return m.modulesKey(msg)
	case tabInfo:
		if key.Matches(msg, keys.Refresh) {
			m.w.Request(worker.ReqVehicle{})
			return m.setToast("Refreshing vehicle info…", false)
		}
	case tabConsole:
		switch {
		case key.Matches(msg, keys.Insert):
			m.mode = modeInsert
			return m.con.input.Focus()
		case key.Matches(msg, keys.NextMatch):
			m.con.jump(1, m.bodyHeight())
		case key.Matches(msg, keys.PrevMatch):
			m.con.jump(-1, m.bodyHeight())
		case key.Matches(msg, keys.Esc):
			m.con.setSearch("")
		}
	}
	return nil
}

// --- view ---

func (m *Model) View() tea.View {
	var v tea.View
	v.AltScreen = true
	if m.width == 0 {
		v.SetContent("starting…")
		return v
	}
	body := ""
	bh := m.bodyHeight()
	switch m.tab {
	case tabDash:
		body = m.dashView(m.width, bh)
	case tabCodes:
		body = m.codesView(m.width, bh)
	case tabModules:
		body = m.modulesView(m.width, bh)
	case tabInfo:
		body = m.infoView(m.width, bh)
	case tabConsole:
		body = m.consoleView(m.width, bh)
	}
	screen := m.headerView() + "\n" + block(body, m.width, bh) + "\n" + m.footerView()

	var overlay string
	switch m.mode {
	case modeHelp:
		overlay = m.helpOverlay()
	case modeConfirm:
		overlay = m.confirmOverlay()
	}
	if overlay != "" {
		ow, oh := lipgloss.Width(overlay), lipgloss.Height(overlay)
		x, y := max(0, (m.width-ow)/2), max(0, (m.height-oh)/2)
		screen = lipgloss.NewCompositor(
			lipgloss.NewLayer(screen),
			lipgloss.NewLayer(overlay).X(x).Y(y).Z(1),
		).Render()
	}
	v.SetContent(screen)
	return v
}

func (m *Model) headerView() string {
	t := m.th
	logo := lipgloss.NewStyle().Background(t.Accent).Foreground(t.OnAccent).Bold(true).Padding(0, 1).Render("lazyobd")
	var tabs []string
	for i, n := range tabNames {
		label := fmt.Sprintf(" %d %s ", i+1, n)
		if i == m.tab {
			tabs = append(tabs, lipgloss.NewStyle().Background(t.Surface).Foreground(t.Accent).Bold(true).Render(label))
		} else {
			tabs = append(tabs, t.Dim.Render(label))
		}
	}
	left := logo + " " + strings.Join(tabs, t.Faint.Render("│"))

	var chips []string
	switch m.state {
	case worker.Connecting:
		chips = append(chips, m.spin.View()+t.fg(t.Warn).Render(" connecting"))
	case worker.Connected:
		name := m.info.ProtocolName
		if name == "" {
			name = "connected"
		}
		if m.opts.Sim {
			name = "SIM · " + name
		}
		chips = append(chips, t.fg(t.Good).Render("● ")+t.Text.Render(name))
	case worker.Failed:
		chips = append(chips, t.fg(t.Bad).Render("● offline"))
	}
	if m.hasVolt {
		c := t.Text
		if m.voltage < 12.0 || m.voltage > 15.0 {
			c = t.fg(t.Bad)
		}
		chips = append(chips, c.Render(fmt.Sprintf("%.1fV", m.voltage)))
	}
	if m.status != nil {
		if m.status.MIL {
			chips = append(chips, t.chip(fmt.Sprintf("MIL ON %d", m.status.DTCCount), t.Bad))
		} else {
			chips = append(chips, t.Dim.Render("MIL off"))
		}
	}
	if m.make.Name != "" {
		label := m.make.Name
		if m.year > 0 {
			label += fmt.Sprintf(" %d", m.year)
		}
		chips = append(chips, t.fg(t.Accent2).Render(label))
	}
	if m.mods.scanning {
		chips = append(chips, t.chip(fmt.Sprintf("SCAN %d/%d", m.mods.done, m.mods.total), t.Accent2))
	}
	if m.paused {
		chips = append(chips, t.chip("PAUSED", t.Warn))
	}
	if m.logger != nil {
		chips = append(chips, t.chip(fmt.Sprintf("● REC %d", m.logger.Rows), t.Bad))
	}
	join := func() string { return strings.Join(chips, t.Faint.Render("  ")) }
	fits := func() bool { return lipgloss.Width(left)+lipgloss.Width(join())+2 <= m.width }
	// Too wide: shorten the link chip, then drop the least important chips
	// (voltage, then MIL) but keep link state, PAUSED and REC.
	if !fits() && m.state == worker.Connected && len(chips) > 0 {
		chips[0] = t.fg(t.Good).Render("● ") + t.Text.Render(shortProto(m.info.ProtocolName, m.opts.Sim))
	}
	optional := 0 // voltage, MIL and make chips sit right after the link chip
	if m.hasVolt {
		optional++
	}
	if m.status != nil {
		optional++
	}
	if m.make.Name != "" {
		optional++
	}
	for ; !fits() && optional > 0 && len(chips) > 1; optional-- {
		chips = append(chips[:1], chips[2:]...)
	}
	for !fits() && len(chips) > 0 {
		chips = chips[:len(chips)-1]
	}
	return spread(left, join()+" ", m.width)
}

func (m *Model) footerView() string {
	t := m.th
	var chip string
	switch m.mode {
	case modeInsert:
		chip = t.chip("INSERT", t.Good)
	case modeFilter:
		chip = t.chip("FILTER", t.Warn)
	case modeConfirm:
		chip = t.chip("CONFIRM", t.Bad)
	case modeHelp:
		chip = t.chip("HELP", t.Accent2)
	default:
		chip = t.chip("NORMAL", t.Accent)
	}
	if m.pendingG {
		chip += t.Key.Render(" g…")
	}
	left := chip + " "
	if m.mode == modeFilter {
		m.filter.SetWidth(max(10, m.width/3))
		left += m.filter.View() + "  "
	}
	left += m.help.ShortHelpView(m.shortHelp())

	right := ""
	if m.toast != "" {
		c := t.Good
		if m.toastBad {
			c = t.Bad
		}
		right = t.fg(c).Render(m.toast)
	} else if m.state == worker.Connected && !m.paused && m.pollRate > 0 {
		right = t.Faint.Render(fmt.Sprintf("%.1f req/s", m.pollRate))
	}
	if right != "" && lipgloss.Width(left)+lipgloss.Width(right)+2 > m.width {
		// Toast wins over the tail of the hint bar.
		left = fit(left, max(0, m.width-lipgloss.Width(right)-2))
	}
	return spread(left, right+" ", m.width)
}

func (m *Model) helpOverlay() string {
	t := m.th
	var cols []string
	for _, s := range fullHelp() {
		lines := []string{lipgloss.NewStyle().Foreground(t.Accent2).Bold(true).Render(s.title)}
		for _, b := range s.binds {
			lines = append(lines, t.Key.Render(fmt.Sprintf("%-9s", b.Help().Key))+" "+t.Dim.Render(b.Help().Desc))
		}
		cols = append(cols, strings.Join(lines, "\n"))
	}
	// Arrange sections into as many columns as fit.
	colW := 0
	for _, c := range cols {
		colW = max(colW, lipgloss.Width(c))
	}
	colW += 4
	// Stack sections into up to 3 columns of roughly equal height.
	k := clampInt((m.width-6)/colW, 1, 3)
	total := 0
	for _, c := range cols {
		total += lipgloss.Height(c) + 1
	}
	target := (total + k - 1) / k
	var columns []string
	var cur []string
	h0 := 0
	for _, c := range cols {
		if h0 > 0 && h0+lipgloss.Height(c) > target && len(columns) < k-1 {
			columns = append(columns, lipgloss.NewStyle().Width(colW).Render(strings.Join(cur, "\n\n")))
			cur, h0 = nil, 0
		}
		cur = append(cur, c)
		h0 += lipgloss.Height(c) + 1
	}
	columns = append(columns, lipgloss.NewStyle().Width(colW).Render(strings.Join(cur, "\n\n")))
	body := lipgloss.JoinHorizontal(lipgloss.Top, columns...)
	lines := strings.Split(body, "\n")
	w := min(m.width-2, lipgloss.Width(body)+4)
	h := min(m.height-2, len(lines)+2)
	return panel(t, "Keybindings", "? close", lines, w, h, t.Accent, true)
}

func (m *Model) confirmOverlay() string {
	t := m.th
	c := m.confirm
	w := min(m.width-4, 64)
	lines := []string{""}
	for _, l := range wrap(c.text, w-6) {
		lines = append(lines, t.Text.Render(l))
	}
	if c.note != "" {
		lines = append(lines, "")
		for _, l := range wrap(c.note, w-6) {
			lines = append(lines, t.Dim.Render(l))
		}
	}
	lines = append(lines, "", t.Key.Render("y")+t.Dim.Render(" "+c.yes+"   ")+t.Key.Render("n/esc")+t.Dim.Render(" cancel"))
	return panel(t, c.title, "", lines, w, len(lines)+2, t.Bad, true)
}

// shortProto turns "ISO 15765-4 (CAN 11/500)" into "CAN 11/500".
func shortProto(name string, sim bool) string {
	if i, j := strings.IndexByte(name, '('), strings.IndexByte(name, ')'); i >= 0 && j > i {
		name = name[i+1 : j]
	}
	if name == "" {
		name = "connected"
	}
	if sim {
		name = "SIM " + name
	}
	return name
}
