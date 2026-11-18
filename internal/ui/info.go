package ui

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"

	"lazyobd/internal/obd"
	"lazyobd/internal/worker"
)

type infoState struct {
	off   int
	total int // content height last render
}

func (s *infoState) motion(mv string, h int) {
	switch mv {
	case "down":
		s.off++
	case "up":
		s.off--
	case "top":
		s.off = 0
	case "bottom":
		s.off = s.total
	case "halfdown":
		s.off += h / 2
	case "halfup":
		s.off -= h / 2
	}
	s.off = clampInt(s.off, 0, max(0, s.total-h))
}

func kv(t Theme, k, v string) string {
	return t.Dim.Render(fmt.Sprintf("%-12s", k)) + v
}

func (m *Model) infoView(w, h int) string {
	t := m.th
	if m.state != worker.Connected && m.vehicle == nil {
		return m.connectingView(w, h)
	}
	none := t.Faint.Render("—")
	orNone := func(s string) string {
		if strings.TrimSpace(s) == "" {
			return none
		}
		return t.Bold.Render(s)
	}

	var veh []string
	vin := ""
	if m.vehicle != nil {
		vin = m.vehicle.VIN
	}
	mk := m.make.Name
	if m.year > 0 {
		mk += fmt.Sprintf(" · %d", m.year)
	}
	prof := string(m.group())
	if m.override != "" {
		prof += " (manual)"
	}
	veh = append(veh,
		kv(t, "VIN", orNone(vin)),
		kv(t, "Make", orNone(mk)),
		kv(t, "Profile", t.Text.Render(prof)),
		kv(t, "Protocol", orNone(m.info.ProtocolName)),
		kv(t, "Adapter", orNone(m.info.Version)),
	)
	port := m.info.Port
	if m.info.Baud > 0 {
		port += fmt.Sprintf(" @ %d baud", m.info.Baud)
	}
	if m.opts.Sim {
		port = "built-in simulator"
	}
	veh = append(veh, kv(t, "Port", orNone(port)))
	if m.hasVolt {
		vs := t.Bold
		note := ""
		switch {
		case m.voltage < 12.0:
			vs, note = t.fg(t.Bad).Bold(true), t.Dim.Render("  low battery")
		case m.voltage > 13.2:
			note = t.Dim.Render("  charging")
		}
		veh = append(veh, kv(t, "Battery", vs.Render(fmt.Sprintf("%.2f V", m.voltage))+note))
	}
	if m.vehicle != nil {
		if m.vehicle.CalID != "" {
			veh = append(veh, kv(t, "Calibration", t.Text.Render(m.vehicle.CalID)))
		}
		veh = append(veh, "", t.Dim.Render("Modules"))
		for _, e := range m.vehicle.ECUs {
			name := m.vehicle.ECUName[e]
			if name == "" {
				name = t.Faint.Render("(no name reported)")
			} else {
				name = t.Text.Render(name)
			}
			veh = append(veh, "  "+t.fg(t.Accent).Render("◆ ")+fmt.Sprintf("%-20s", obd.ECULabel(e))+name)
		}
	}
	if m.vehErr != nil {
		veh = append(veh, "", t.fg(t.Bad).Render(m.vehErr.Error()))
	}
	if m.dash.known {
		veh = append(veh, "", kv(t, "Live PIDs", t.Text.Render(fmt.Sprintf("%d supported", len(m.dash.pids)))))
	}

	var rd []string
	if m.status == nil {
		rd = append(rd, t.Dim.Render("readiness not available yet"))
	} else {
		st := m.status
		eng := "Spark ignition (petrol)"
		if st.Diesel {
			eng = "Compression ignition (diesel)"
		}
		mil := t.fg(t.Good).Render("off")
		if st.MIL {
			mil = t.fg(t.Bad).Bold(true).Render("ON")
		}
		done, avail := 0, 0
		for _, mo := range st.Monitors {
			if mo.Available {
				avail++
				if mo.Complete {
					done++
				}
			}
		}
		rd = append(rd,
			kv(t, "MIL", mil),
			kv(t, "DTC count", t.Bold.Render(fmt.Sprint(st.DTCCount))),
			kv(t, "Engine", t.Text.Render(eng)),
			kv(t, "Ready", t.Bold.Render(fmt.Sprintf("%d/%d", done, avail))+t.Dim.Render(" monitors complete")),
			"")
		for _, mo := range st.Monitors {
			var mark, state string
			switch {
			case !mo.Available:
				mark, state = t.Faint.Render("–"), t.Faint.Render("not supported")
			case mo.Complete:
				mark, state = t.fg(t.Good).Render("✓"), t.fg(t.Good).Render("complete")
			default:
				mark, state = t.fg(t.Warn).Render("✗"), t.fg(t.Warn).Render("incomplete")
			}
			name := mo.Name
			if mo.Available {
				name = t.Text.Render(fmt.Sprintf("%-22s", name))
			} else {
				name = t.Faint.Render(fmt.Sprintf("%-22s", name))
			}
			rd = append(rd, " "+mark+" "+name+state)
		}
	}

	var out string
	if w >= 96 {
		lw := w / 2
		ph := max(h, max(len(veh), len(rd))+2)
		out = lipgloss.JoinHorizontal(lipgloss.Top,
			panel(t, "Vehicle", "", veh, lw, ph, t.Accent, true),
			panel(t, "Readiness monitors", "", rd, w-lw, ph, t.Subtle, false))
	} else {
		out = panel(t, "Vehicle", "", veh, w, len(veh)+2, t.Accent, true) + "\n" +
			panel(t, "Readiness monitors", "", rd, w, len(rd)+2, t.Subtle, false)
	}
	lines := strings.Split(out, "\n")
	m.info2.total = len(lines)
	m.info2.off = clampInt(m.info2.off, 0, max(0, len(lines)-h))
	return strings.Join(lines[m.info2.off:], "\n")
}
