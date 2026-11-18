package ui

import (
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"charm.land/lipgloss/v2"

	"lazyobd/internal/obd"
	"lazyobd/internal/worker"
)

const histLen = 600

type series struct {
	cur, min, max, sum float64
	n                  int
	ok                 bool
	err                error
	hist               []float64
	at                 time.Time
}

type dashState struct {
	pids   []obd.PID
	known  bool
	err    error
	vals   map[byte]*series
	sel    int // index into visible()
	rowOff int
	detail bool
	filter string
}

func newDashState() dashState { return dashState{vals: map[byte]*series{}} }

func (d *dashState) setPIDs(p []obd.PID) {
	d.pids, d.known = p, true
	for _, x := range p {
		if d.vals[x.Code] == nil {
			d.vals[x.Code] = &series{}
		}
	}
	d.clampSel()
}

func (d *dashState) update(v worker.ValueMsg) {
	s := d.vals[v.PID]
	if s == nil {
		s = &series{}
		d.vals[v.PID] = s
	}
	s.err = v.Err
	if v.Err != nil {
		return
	}
	if !s.ok || v.Value < s.min {
		s.min = v.Value
	}
	if !s.ok || v.Value > s.max {
		s.max = v.Value
	}
	s.cur, s.ok, s.at = v.Value, true, v.At
	s.sum += v.Value
	s.n++
	s.hist = append(s.hist, v.Value)
	if len(s.hist) > histLen {
		s.hist = s.hist[len(s.hist)-histLen:]
	}
}

// current returns the latest good value of every PID (for CSV rows).
func (d *dashState) current() map[byte]float64 {
	out := map[byte]float64{}
	for c, s := range d.vals {
		if s.ok && s.err == nil {
			out[c] = s.cur
		}
	}
	return out
}

func (d *dashState) visible() []obd.PID {
	if d.filter == "" {
		return d.pids
	}
	f := strings.ToLower(d.filter)
	var out []obd.PID
	for _, p := range d.pids {
		if strings.Contains(strings.ToLower(p.Name), f) || strings.Contains(strings.ToLower(fmt.Sprintf("%02x", p.Code)), f) {
			out = append(out, p)
		}
	}
	return out
}

func (d *dashState) clampSel() {
	d.sel = clampInt(d.sel, 0, max(0, len(d.visible())-1))
}

const cardMinW, cardH = 26, 5

func gridCols(width int) int { return max(1, width/cardMinW) }

func (d *dashState) motion(mv string, width, height int) {
	n := len(d.visible())
	if n == 0 {
		return
	}
	cols := gridCols(width)
	if d.detail { // in graph view, any motion walks the sensor list
		cols = 1
	}
	rowsFit := max(1, height/cardH)
	switch mv {
	case "down":
		if d.sel+cols < n {
			d.sel += cols
		} else if d.detail {
			d.sel = 0
		}
	case "up":
		if d.sel-cols >= 0 {
			d.sel -= cols
		} else if d.detail {
			d.sel = n - 1
		}
	case "left":
		if d.detail || d.sel%cols > 0 {
			d.sel = (d.sel - 1 + n) % n
		}
	case "right":
		if d.detail || (d.sel%cols < cols-1 && d.sel+1 < n) {
			d.sel = (d.sel + 1) % n
		}
	case "top":
		d.sel = 0
	case "bottom":
		d.sel = n - 1
	case "halfdown":
		d.sel = min(n-1, d.sel+cols*max(1, rowsFit/2))
	case "halfup":
		d.sel = max(0, d.sel-cols*max(1, rowsFit/2))
	}
}

func (m *Model) dashView(w, h int) string {
	t := m.th
	d := &m.dash
	if !d.known {
		return m.connectingView(w, h)
	}
	if len(d.pids) == 0 {
		msg := "The vehicle did not report any supported live sensors."
		if d.err != nil {
			msg += "\n" + t.Dim.Render(d.err.Error())
		}
		return center(t.Text.Render(msg), w, h)
	}
	vis := d.visible()
	if len(vis) == 0 {
		return center(t.Dim.Render(fmt.Sprintf("No sensors match “%s” — esc to clear", d.filter)), w, h)
	}
	d.clampSel()
	if d.detail {
		return m.graphView(vis[d.sel], w, h)
	}

	cols := gridCols(w)
	cw := w / cols
	rowsFit := max(1, h/cardH)
	totalRows := (len(vis) + cols - 1) / cols
	selRow := d.sel / cols
	if selRow < d.rowOff {
		d.rowOff = selRow
	}
	if selRow >= d.rowOff+rowsFit {
		d.rowOff = selRow - rowsFit + 1
	}
	d.rowOff = clampInt(d.rowOff, 0, max(0, totalRows-rowsFit))

	var rows []string
	for r := d.rowOff; r < min(totalRows, d.rowOff+rowsFit); r++ {
		var cards []string
		for c := 0; c < cols; c++ {
			i := r*cols + c
			if i >= len(vis) {
				cards = append(cards, block("", cw, cardH))
				continue
			}
			cards = append(cards, m.card(vis[i], cw, i == d.sel))
		}
		rows = append(rows, lipgloss.JoinHorizontal(lipgloss.Top, cards...))
	}
	out := strings.Join(rows, "\n")
	if totalRows > rowsFit {
		// Scroll indicator on the last line.
		ind := t.Faint.Render(fmt.Sprintf("── %d/%d sensors · rows %d-%d of %d ──", d.sel+1, len(vis), d.rowOff+1, min(totalRows, d.rowOff+rowsFit), totalRows))
		lines := strings.Split(out, "\n")
		for len(lines) < h-1 {
			lines = append(lines, "")
		}
		lines = append(lines[:h-1], lipgloss.PlaceHorizontal(w, lipgloss.Center, ind))
		out = strings.Join(lines, "\n")
	}
	return out
}

func (m *Model) card(p obd.PID, w int, selected bool) string {
	t := m.th
	s := m.dash.vals[p.Code]
	iw := w - 4
	border := t.Subtle
	valStyle := t.Bold
	barColor := t.Accent
	var value string
	switch {
	case s == nil || (!s.ok && s.err == nil):
		value = t.Faint.Render("—")
	case !s.ok && errors.Is(s.err, obd.ErrNotSupported):
		value = t.Faint.Render("n/a")
	case !s.ok:
		value = t.fg(t.Bad).Render("err")
	default:
		if p.Warn(s.cur) {
			valStyle = lipgloss.NewStyle().Foreground(t.Bad).Bold(true)
			barColor, border = t.Bad, t.Bad
		}
		value = valStyle.Render(p.Format(s.cur)) + " " + t.Dim.Render(p.Unit)
	}
	stale := s != nil && s.ok && s.err != nil
	if selected {
		border = t.Accent
	}
	right := fmt.Sprintf("%02X", p.Code)
	if m.paused || stale {
		right = "‖ " + right
	}

	frac := math.NaN()
	var spark string
	var mm string
	if s != nil && s.ok {
		frac = (s.cur - p.Min) / (p.Max - p.Min)
		spark = t.fg(t.Accent2).Render(sparkline(s.hist, iw))
		mm = t.Faint.Render(fmt.Sprintf("↓%s ↑%s", p.Format(s.min), p.Format(s.max)))
	}
	lines := []string{
		spread(value, mm, iw),
		gauge(t, frac, iw, barColor),
		spark,
	}
	return panel(t, p.Name, right, lines, w, cardH, border, selected)
}

func (m *Model) graphView(p obd.PID, w, h int) string {
	t := m.th
	s := m.dash.vals[p.Code]
	iw := w - 4
	var lines []string
	if s == nil || !s.ok {
		lines = append(lines, "", t.Dim.Render("waiting for data…"))
	} else {
		vs := t.Bold
		if p.Warn(s.cur) {
			vs = lipgloss.NewStyle().Foreground(t.Bad).Bold(true)
		}
		big := vs.Render(p.Format(s.cur)) + " " + t.Dim.Render(p.Unit)
		stats := fmt.Sprintf("min %s · avg %s · max %s · %d samples", p.Format(s.min), p.Format(s.sum/float64(s.n)), p.Format(s.max), s.n)
		lines = append(lines, spread(big, t.Dim.Render(stats), iw), "")
		lines = append(lines, chart(t, s.hist, iw, max(3, h-6), p.Prec, t.Accent2)...)
		lines = append(lines, t.Faint.Render(fmt.Sprintf("range %s…%s %s · last %s", p.Format(p.Min), p.Format(p.Max), p.Unit, s.at.Format("15:04:05"))))
	}
	return panel(t, fmt.Sprintf("%s · PID %02X", p.Name, p.Code), fmt.Sprintf("%d/%d", m.dash.sel+1, len(m.dash.visible())), lines, w, h, t.Accent, true)
}

func (m *Model) connectingView(w, h int) string {
	t := m.th
	var lines []string
	switch {
	case m.state == worker.Failed || (m.state == worker.Connecting && m.lastErr != ""):
		lines = append(lines, t.fg(t.Bad).Bold(true).Render("Could not talk to the vehicle"), "")
		for _, l := range wrap(m.lastErr, min(70, w-8)) {
			lines = append(lines, t.Text.Render(l))
		}
		lines = append(lines, "",
			t.Dim.Render("• plug the cable into the OBD port and turn the ignition on"),
			t.Dim.Render("• check the port with --port, or try --baud 9600 / 115200"),
			t.Dim.Render("• try the console tab (5) to send raw AT commands"),
			"")
		if m.state == worker.Connecting {
			lines = append(lines, m.spin.View()+t.Dim.Render(" retrying: "+m.detail))
		} else {
			lines = append(lines, t.Dim.Render("retrying automatically · ")+t.Key.Render("R")+t.Dim.Render(" retry now"))
		}
	default:
		lines = append(lines, m.spin.View()+" "+t.Bold.Render("Connecting"), "")
		if m.detail != "" {
			lines = append(lines, t.Dim.Render(m.detail))
		}
		if m.state == worker.Connected {
			lines = append(lines, t.Dim.Render("detecting supported sensors…"))
		}
	}
	bw := 0
	for _, l := range lines {
		bw = max(bw, lipgloss.Width(l))
	}
	box := panel(t, "Link", "", lines, min(w, bw+6), len(lines)+2, t.Subtle, false)
	return center(box, w, h)
}
