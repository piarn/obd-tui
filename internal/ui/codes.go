package ui

import (
	"fmt"
	"image/color"
	"strings"

	"charm.land/lipgloss/v2"

	"lazyobd/internal/obd"
	"lazyobd/internal/worker"
)

type dtcKind int

const (
	kindStored dtcKind = iota
	kindPending
	kindPermanent
)

var kindNames = []string{"Stored", "Pending", "Permanent"}
var kindHints = []string{
	"Confirmed fault. Usually lights the check-engine lamp (MIL). Clearing only helps once the cause is fixed — it will come back otherwise.",
	"Seen once but not yet confirmed. If the fault shows up again on the next drive cycle it becomes a stored code.",
	"Kept by the ECU until it has verified the repair itself; a scan tool cannot erase it. It clears after enough fault-free drive cycles.",
}

type codeItem struct {
	kind dtcKind
	dtc  obd.DTC
}

type codesState struct {
	all     []codeItem
	items   []codeItem
	loaded  bool
	loading bool
	err     error
	sel     int
	off     int
	filter  string
}

func (c *codesState) set(m worker.DTCMsg) {
	c.loaded, c.loading, c.err = true, false, m.Err
	c.all = nil
	for k, list := range [][]obd.DTC{m.Stored, m.Pending, m.Permanent} {
		for _, d := range list {
			c.all = append(c.all, codeItem{dtcKind(k), d})
		}
	}
}

func (c *codesState) applyFilter(f string) {
	c.filter = f
	c.items = c.items[:0]
	lf := strings.ToLower(f)
	for _, it := range c.all {
		if lf == "" || strings.Contains(strings.ToLower(it.dtc.Code+" "+it.dtc.Desc), lf) {
			c.items = append(c.items, it)
		}
	}
	c.sel = clampInt(c.sel, 0, max(0, len(c.items)-1))
}

func (c *codesState) motion(mv string, h int) {
	n := len(c.items)
	if n == 0 {
		return
	}
	switch mv {
	case "down":
		c.sel = min(n-1, c.sel+1)
	case "up":
		c.sel = max(0, c.sel-1)
	case "top":
		c.sel = 0
	case "bottom":
		c.sel = n - 1
	case "halfdown":
		c.sel = min(n-1, c.sel+max(1, h/2))
	case "halfup":
		c.sel = max(0, c.sel-max(1, h/2))
	}
}

func (m *Model) kindColor(k dtcKind) color.Color {
	switch k {
	case kindStored:
		return m.th.Bad
	case kindPending:
		return m.th.Warn
	}
	return m.th.Accent2
}

func (m *Model) codesView(w, h int) string {
	t := m.th
	c := &m.codes
	if !c.loaded {
		if m.state != worker.Connected {
			return m.connectingView(w, h)
		}
		return center(m.spin.View()+t.Dim.Render(" reading trouble codes…"), w, h)
	}

	wide := w >= 100
	listW, listH := w, h
	detW, detH := w, 0
	if wide {
		listW = w * 55 / 100
		detW = w - listW
		detH = h
	} else {
		listH = max(6, h*55/100)
		detH = h - listH
	}

	// Build list lines with section headers, tracking which line holds sel.
	iw := listW - 4
	var lines []string
	selLine := 0
	counts := [3]int{}
	for _, it := range c.all {
		counts[it.kind]++
	}
	idx := 0
	for k := kindStored; k <= kindPermanent; k++ {
		hdr := lipgloss.NewStyle().Foreground(m.kindColor(k)).Bold(true).Render(strings.ToUpper(kindNames[k])) +
			t.Faint.Render(fmt.Sprintf(" (%d)", counts[k]))
		lines = append(lines, hdr)
		any := false
		for idx < len(c.items) && c.items[idx].kind == k {
			it := c.items[idx]
			dot := t.fg(m.kindColor(k)).Render("●")
			code := t.Bold.Render(it.dtc.Code)
			row := fmt.Sprintf(" %s %s  %s", dot, code, t.Text.Render(it.dtc.Desc))
			if idx == c.sel {
				selLine = len(lines)
				row = lipgloss.NewStyle().Background(t.Surface).Render(fit(
					t.Key.Render("▌")+dot+" "+code+"  "+t.Text.Render(it.dtc.Desc), iw))
			}
			lines = append(lines, row)
			idx++
			any = true
		}
		if !any {
			lines = append(lines, t.Faint.Render("   none"))
		}
		lines = append(lines, "")
	}
	if c.err != nil {
		lines = append(lines, t.fg(t.Bad).Render("read error: "+c.err.Error()))
	}
	// Scroll so the selection stays visible.
	vis := listH - 2
	if selLine < c.off {
		c.off = selLine
	}
	if selLine >= c.off+vis {
		c.off = selLine - vis + 1
	}
	c.off = clampInt(c.off, 0, max(0, len(lines)-vis))
	lines = lines[c.off:]

	title := "Trouble codes"
	if c.filter != "" {
		title += " /" + c.filter
	}
	right := t.fg(t.Good).Render("MIL off")
	if m.status != nil && m.status.MIL {
		right = t.fg(t.Bad).Bold(true).Render("MIL ON")
	}
	if c.loading {
		right = m.spin.View() + " " + right
	}
	list := panel(t, title, right, lines, listW, listH, t.Accent, true)

	det := m.codeDetail(detW, detH)
	if wide {
		return lipgloss.JoinHorizontal(lipgloss.Top, list, det)
	}
	return list + "\n" + det
}

func (m *Model) codeDetail(w, h int) string {
	t := m.th
	c := &m.codes
	iw := w - 4
	var lines []string
	if len(c.items) == 0 {
		if len(c.all) == 0 {
			lines = append(lines, "", t.fg(t.Good).Bold(true).Render("✓ No trouble codes"), "",
				t.Dim.Render("The ECU reports no stored, pending or permanent codes."))
		} else {
			lines = append(lines, "", t.Dim.Render("No codes match the filter."))
		}
		return panel(t, "Details", "", lines, w, h, t.Subtle, false)
	}
	it := c.items[c.sel]
	sys := map[byte]string{'P': "Powertrain", 'C': "Chassis", 'B': "Body", 'U': "Network"}[it.dtc.Code[0]]
	generic := it.dtc.Code[1] == '0' || it.dtc.Code[1] == '2'
	scope := "generic SAE code"
	if !generic {
		scope = "manufacturer-specific code"
	}
	lines = append(lines,
		lipgloss.NewStyle().Foreground(m.kindColor(it.kind)).Bold(true).Render(it.dtc.Code)+"  "+
			t.chip(kindNames[it.kind], m.kindColor(it.kind)),
		"")
	for _, l := range wrap(it.dtc.Desc, iw) {
		lines = append(lines, t.Bold.Render(l))
	}
	lines = append(lines, "",
		t.Dim.Render("System  ")+t.Text.Render(sys+" · "+scope),
		t.Dim.Render("Module  ")+t.Text.Render(obd.ECULabel(it.dtc.ECU)),
		"")
	for _, l := range wrap(kindHints[it.kind], iw) {
		lines = append(lines, t.Dim.Render(l))
	}
	return panel(t, "Details", fmt.Sprintf("%d/%d", c.sel+1, len(c.items)), lines, w, h, t.Subtle, false)
}
