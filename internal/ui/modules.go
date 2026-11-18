package ui

import (
	"fmt"
	"image/color"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"lazyobd/internal/uds"
	"lazyobd/internal/vehicle"
	"lazyobd/internal/worker"
)

type modItem struct {
	res  uds.Result
	done bool // scanned in the current run
}

type modState struct {
	items      []modItem
	scanned    bool
	scanning   bool
	clearing   bool
	deep       bool
	done       int
	total      int
	err        error
	noFC       bool
	at         time.Time
	sel, off   int
	dOff       int
	right      bool // focus on the details panel
	showAbsent bool
	group      vehicle.Group // profile the shown results came from
}

// group is the profile in use: manual override, else VIN-derived.
func (m *Model) group() vehicle.Group {
	if m.override != "" {
		return m.override
	}
	if m.make.Group != "" {
		return m.make.Group
	}
	return vehicle.GroupGeneric
}

func (m *Model) profile() uds.Profile { return uds.ProfileFor(m.group()) }

// visible returns item indexes to show (absent modules hidden by default).
func (s *modState) visible() []int {
	var out []int
	for i, it := range s.items {
		if s.showAbsent || !it.done || it.res.State != uds.Absent {
			out = append(out, i)
		}
	}
	return out
}

func (s *modState) selected() *modItem {
	v := s.visible()
	if len(v) == 0 {
		return nil
	}
	s.sel = clampInt(s.sel, 0, len(v)-1)
	return &s.items[v[s.sel]]
}

func (s *modState) motion(mv string, h int) {
	if s.right {
		switch mv {
		case "down":
			s.dOff++
		case "up":
			s.dOff--
		case "top":
			s.dOff = 0
		case "halfdown":
			s.dOff += h / 2
		case "halfup":
			s.dOff -= h / 2
		case "bottom":
			s.dOff = 1 << 20
		case "left":
			s.right = false
		}
		s.dOff = max(0, s.dOff)
		return
	}
	n := len(s.visible())
	if n == 0 {
		return
	}
	prev := s.sel
	switch mv {
	case "down":
		s.sel = min(n-1, s.sel+1)
	case "up":
		s.sel = max(0, s.sel-1)
	case "top":
		s.sel = 0
	case "bottom":
		s.sel = n - 1
	case "halfdown":
		s.sel = min(n-1, s.sel+max(1, h/2))
	case "halfup":
		s.sel = max(0, s.sel-max(1, h/2))
	case "right":
		s.right = true
	}
	if s.sel != prev {
		s.dOff = 0
	}
}

func (m *Model) onModuleMsg(msg any) (tea.Cmd, bool) {
	s := &m.mods
	switch msg := msg.(type) {
	case worker.ScanStartMsg:
		s.scanning, s.deep, s.done, s.total, s.err, s.noFC = true, msg.Deep, 0, msg.Total, nil, false
		if !msg.Deep {
			s.group = m.group()
			s.items = nil
			for _, mod := range m.profile().Modules {
				s.items = append(s.items, modItem{res: uds.Result{Module: mod}})
			}
			s.sel, s.off, s.dOff = 0, 0, 0
		}
		return nil, true
	case worker.ModuleMsg:
		r := msg.Result
		s.done = msg.Done
		found := false
		for i := range s.items {
			if s.items[i].res.Module.Key() == r.Module.Key() {
				s.items[i] = modItem{res: r, done: true}
				found = true
			}
		}
		// Deep scan: only keep modules that answered.
		if !found && r.State != uds.Absent {
			s.items = append(s.items, modItem{res: r, done: true})
		}
		return nil, true
	case worker.ScanDoneMsg:
		s.scanning, s.scanned, s.at, s.noFC = false, true, time.Now(), msg.NoFlowControl
		s.err = msg.Err
		switch {
		case msg.Err != nil:
			return m.setToast("Scan failed: "+msg.Err.Error(), true), true
		case msg.Cancelled:
			return m.setToast("Scan cancelled", false), true
		}
		n, faults := 0, 0
		for _, it := range s.items {
			if it.done && it.res.State != uds.Absent {
				n++
				if len(it.res.DTCs) > 0 {
					faults++
				}
			}
		}
		return m.setToast(fmt.Sprintf("Scan done: %d modules answered, %d with faults", n, faults), faults > 0), true
	case worker.ModuleClearMsg:
		s.clearing = false
		if msg.Err != nil {
			return m.setToast(fmt.Sprintf("Cleared %d; %v", msg.Cleared, msg.Err), true), true
		}
		return m.setToast(fmt.Sprintf("Cleared codes in %d module(s)", msg.Cleared), false), true
	}
	return nil, false
}

func (m *Model) startScan(deep bool) tea.Cmd {
	if m.state != worker.Connected {
		return m.setToast("Not connected", true)
	}
	if m.mods.scanning {
		return nil
	}
	m.mods.scanning = true
	m.w.Request(worker.ReqScan{Modules: m.profile().Modules, Deep: deep})
	if deep {
		return m.setToast("Deep scan: sweeping 255 29-bit addresses (esc to stop)…", false)
	}
	return m.setToast(fmt.Sprintf("Scanning %d modules (%s profile)…", len(m.profile().Modules), m.group()), false)
}

func (m *Model) modulesKey(msg tea.KeyPressMsg) tea.Cmd {
	s := &m.mods
	switch msg.String() {
	case "s":
		return m.startScan(false)
	case "D":
		return m.startScan(true)
	case "esc":
		if s.scanning {
			m.w.CancelScan()
			return m.setToast("Stopping after the current module…", false)
		}
		s.right = false
	case "enter":
		s.right = !s.right
	case "a":
		s.showAbsent = !s.showAbsent
		s.sel = 0
	case "m":
		// Cycle to the next profile; landing on the VIN's own group means auto.
		cur := m.group()
		next := vehicle.Groups[0]
		for i, g := range vehicle.Groups {
			if g == cur {
				next = vehicle.Groups[(i+1)%len(vehicle.Groups)]
			}
		}
		if next == m.make.Group {
			next = ""
		}
		m.override = next
		label := string(m.group())
		if next == "" {
			label += " (auto from VIN)"
		}
		return m.setToast("Module profile: "+label+" — press s to scan", false)
	case "c", "C":
		all := msg.String() == "C"
		var targets []worker.ClearTarget
		var names []string
		if all {
			for _, it := range s.items {
				if it.done && len(it.res.DTCs) > 0 {
					targets = append(targets, worker.ClearTarget{Module: it.res.Module, KWP: it.res.KWP})
					names = append(names, it.res.Module.Name)
				}
			}
		} else if it := s.selected(); it != nil && it.done && it.res.State == uds.OK {
			targets = []worker.ClearTarget{{Module: it.res.Module, KWP: it.res.KWP}}
			names = []string{it.res.Module.Name}
		}
		if len(targets) == 0 {
			return m.setToast("Nothing to clear — scan first (s)", true)
		}
		if s.scanning || m.state != worker.Connected {
			return m.setToast("Busy or not connected", true)
		}
		title := "Clear codes in " + names[0] + "?"
		if len(names) > 1 {
			title = fmt.Sprintf("Clear codes in %d modules?", len(names))
		}
		m.ask(confirmState{
			title: title,
			text:  "Sends UDS ClearDiagnosticInformation to: " + strings.Join(names, ", ") + ". Codes come back if the fault is still present; airbag and ABS faults usually need the cause fixed first.",
			note:  "Ignition ON, engine OFF. The module is rescanned afterwards.",
			yes:   "clear",
			action: func() tea.Cmd {
				s.clearing = true
				m.w.Request(worker.ReqClearModules{Targets: targets})
				return m.setToast("Clearing…", false)
			},
		})
	}
	return nil
}

// moduleHelp is the hint bar for the Modules tab.
func moduleHelp(s *modState) []key.Binding {
	if s.scanning {
		return []key.Binding{hint("esc", "stop scan"), vertHint, keys.Help, keys.Quit}
	}
	return []key.Binding{vertHint, hint("s", "scan"), hint("D", "deep scan"), hint("c/C", "clear one/all"),
		hint("m", "make"), hint("a", "show absent"), hint("enter", "details"), keys.Help, keys.Quit}
}

func (m *Model) modStatus(it modItem) (string, color.Color) {
	t := m.th
	if !it.done {
		if m.mods.scanning {
			return "…", t.Muted
		}
		return "·", t.Subtle
	}
	r := it.res
	switch {
	case r.Err != nil:
		return "!", t.Bad
	case r.State == uds.Absent:
		return "–", t.Subtle
	case r.State == uds.Refused:
		return "✗", t.Warn
	case len(r.DTCs) == 0:
		return "✓", t.Good
	}
	for _, d := range r.DTCs {
		if d.Status&(uds.StTestFailed|uds.StConfirmed) != 0 {
			return "●", t.Bad
		}
	}
	return "●", t.Warn
}

func (m *Model) modulesView(w, h int) string {
	t := m.th
	s := &m.mods
	if m.state != worker.Connected && !s.scanned {
		return m.connectingView(w, h)
	}
	p := m.profile()
	if len(s.items) == 0 {
		mk := m.make.Name
		if mk == "" {
			mk = "unknown make"
		}
		lines := []string{
			t.Bold.Render("Module scan") + t.Dim.Render(fmt.Sprintf("  ·  %s → %s profile", mk, p.Group)), "",
		}
		for _, l := range wrap(p.Note, 64) {
			lines = append(lines, t.Dim.Render(l))
		}
		lines = append(lines, "",
			t.Key.Render("s")+t.Dim.Render(fmt.Sprintf("  scan %d modules for trouble codes", len(p.Modules))),
			t.Key.Render("D")+t.Dim.Render("  deep scan: sweep all 29-bit addresses (~1 min)"),
			t.Key.Render("m")+t.Dim.Render("  change make profile (VAG / BMW / Honda / Generic)"))
		if s.err != nil {
			lines = append(lines, "")
			for _, l := range wrap(s.err.Error(), 64) {
				lines = append(lines, t.fg(t.Bad).Render(l))
			}
		}
		bw := 0
		for _, l := range lines {
			bw = max(bw, lipgloss.Width(l))
		}
		return center(panel(t, "Modules", "", lines, min(w, bw+6), len(lines)+2, t.Subtle, false), w, h)
	}

	wide := w >= 100
	listW, listH, detW, detH := w, h, w, 0
	if wide {
		listW = w * 45 / 100
		detW, detH = w-listW, h
	} else {
		listH = max(6, h/2)
		detH = h - listH
	}

	vis := s.visible()
	s.sel = clampInt(s.sel, 0, max(0, len(vis)-1))
	iw := listW - 4
	var lines []string
	for row, idx := range vis {
		it := s.items[idx]
		icon, c := m.modStatus(it)
		count := "  "
		if it.done && it.res.State == uds.OK && len(it.res.DTCs) > 0 {
			count = t.fg(c).Bold(true).Render(fmt.Sprintf("%2d", len(it.res.DTCs)))
		}
		left := t.fg(c).Render(icon) + " " + t.Text.Render(it.res.Module.Name)
		right := t.Faint.Render(it.res.Module.ID()) + " " + count
		line := spread(left, right, iw-1)
		if row == s.sel {
			bar := t.Key.Render("▌")
			if s.right {
				bar = t.Dim.Render("▌")
			}
			line = lipgloss.NewStyle().Background(t.Surface).Render(fit(bar+line, iw))
		} else {
			line = " " + line
		}
		lines = append(lines, line)
	}
	if len(vis) == 0 {
		lines = append(lines, t.Dim.Render("no module answered — a shows absent ones"))
	}
	vh := listH - 2
	if s.sel < s.off {
		s.off = s.sel
	}
	if s.sel >= s.off+vh {
		s.off = s.sel - vh + 1
	}
	s.off = clampInt(s.off, 0, max(0, len(lines)-vh))
	lines = lines[s.off:]

	mk := string(s.group)
	if m.make.Name != "" && s.group == m.make.Group {
		mk = m.make.Name + " · " + mk
	}
	if p.Group != s.group {
		mk += " (s: rescan as " + string(p.Group) + ")"
	}
	right := ""
	switch {
	case s.scanning:
		right = m.spin.View() + fmt.Sprintf(" %d/%d", s.done, s.total)
	case s.clearing:
		right = m.spin.View() + " clearing"
	case s.scanned:
		right = s.at.Format("15:04")
	}
	list := panel(t, "Modules · "+mk, right, lines, listW, listH, focusColor(t, !s.right), !s.right)
	det := m.moduleDetail(detW, detH)
	if wide {
		return lipgloss.JoinHorizontal(lipgloss.Top, list, det)
	}
	return list + "\n" + det
}

func focusColor(t Theme, focused bool) color.Color {
	if focused {
		return t.Accent
	}
	return t.Subtle
}

func (m *Model) moduleDetail(w, h int) string {
	t := m.th
	s := &m.mods
	it := s.selected()
	iw := w - 4
	var lines []string
	if it == nil {
		return panel(t, "Details", "", nil, w, h, t.Subtle, false)
	}
	r := it.res
	lines = append(lines, t.Bold.Render(r.Module.Name), t.Dim.Render("address  ")+t.Text.Render(r.Module.ID()))
	if r.PartNo != "" {
		lines = append(lines, t.Dim.Render("part no  ")+t.Text.Render(r.PartNo))
	}
	if r.SysName != "" {
		lines = append(lines, t.Dim.Render("system   ")+t.Text.Render(r.SysName))
	}
	if r.KWP {
		lines = append(lines, t.Dim.Render("protocol ")+t.Text.Render("KWP2000 on CAN"))
	}
	lines = append(lines, "")
	switch {
	case !it.done:
		lines = append(lines, t.Dim.Render("not scanned yet"))
	case r.Err != nil:
		for _, l := range wrap("error: "+r.Err.Error(), iw) {
			lines = append(lines, t.fg(t.Bad).Render(l))
		}
	case r.State == uds.Absent:
		for _, l := range wrap("No answer. The module may not be fitted, may be asleep, or uses a different address on this model — try a deep scan (D) or another profile (m).", iw) {
			lines = append(lines, t.Dim.Render(l))
		}
	case r.State == uds.Refused:
		lines = append(lines, t.fg(t.Warn).Render("Answered but refused: "+uds.NRCText(r.NRC)))
		for _, l := range wrap("Some modules only allow reading codes with the engine off, in a diagnostic session, or after security access.", iw) {
			lines = append(lines, t.Dim.Render(l))
		}
	case len(r.DTCs) == 0:
		lines = append(lines, t.fg(t.Good).Bold(true).Render("✓ No trouble codes"))
	default:
		lines = append(lines, t.Dim.Render(fmt.Sprintf("%d trouble code(s)", len(r.DTCs))), "")
		for _, d := range r.DTCs {
			c := t.Warn
			if d.Status&(uds.StTestFailed|uds.StConfirmed) != 0 {
				c = t.Bad
			}
			head := lipgloss.NewStyle().Foreground(c).Bold(true).Render(d.Display())
			for _, f := range d.Flags() {
				head += " " + t.Faint.Render("["+f+"]")
			}
			lines = append(lines, head)
			for _, l := range wrap(d.Desc, iw-2) {
				lines = append(lines, "  "+t.Text.Render(l))
			}
			if ft := uds.FTBText(d.FTB); ft != "" {
				lines = append(lines, "  "+t.Dim.Render("failure: "+ft))
			}
			lines = append(lines, "")
		}
	}
	if s.noFC {
		lines = append(lines, "")
		for _, l := range wrap("Note: adapter rejected flow-control setup (ATFCSH); modules with several codes may report incompletely.", iw) {
			lines = append(lines, t.fg(t.Warn).Render(l))
		}
	}
	vh := h - 2
	s.dOff = clampInt(s.dOff, 0, max(0, len(lines)-vh))
	right := ""
	if len(lines) > vh {
		right = fmt.Sprintf("%d%%", 100*(s.dOff+vh)/len(lines))
	}
	return panel(t, "Details", right, lines[s.dOff:], w, h, focusColor(t, s.right), s.right)
}
