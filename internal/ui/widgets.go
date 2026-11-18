package ui

import (
	"fmt"
	"image/color"
	"math"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// fit truncates or pads a styled line to exactly w cells.
func fit(s string, w int) string {
	if w <= 0 {
		return ""
	}
	if lipgloss.Width(s) > w {
		s = ansi.Truncate(s, w, "…")
	}
	if pad := w - lipgloss.Width(s); pad > 0 {
		s += strings.Repeat(" ", pad)
	}
	return s
}

// spread places left and right on one line of width w.
func spread(left, right string, w int) string {
	gap := w - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 1 {
		return fit(left, w)
	}
	return left + strings.Repeat(" ", gap) + right
}

// block forces s to exactly w×h cells.
func block(s string, w, h int) string {
	lines := strings.Split(s, "\n")
	out := make([]string, h)
	for i := range out {
		if i < len(lines) {
			out[i] = fit(lines[i], w)
		} else {
			out[i] = strings.Repeat(" ", w)
		}
	}
	return strings.Join(out, "\n")
}

// panel draws a rounded box of exactly w×h with the title set into the top
// border (lazygit style) and an optional right-aligned label.
func panel(t Theme, title, right string, lines []string, w, h int, border color.Color, focused bool) string {
	if w < 6 || h < 2 {
		return block("", w, h)
	}
	bs := t.fg(border)
	ts := t.Dim
	if focused {
		ts = lipgloss.NewStyle().Foreground(t.Accent).Bold(true)
	}
	inner := w - 2
	var top strings.Builder
	top.WriteString(bs.Render("╭─"))
	used := 1
	if title != "" {
		tt := ts.Render(" " + ansi.Truncate(title, inner-4, "…") + " ")
		top.WriteString(tt)
		used += lipgloss.Width(tt)
	}
	rt := ""
	if right != "" {
		rt = " " + right + " "
	}
	fill := inner - used
	if rt != "" {
		fill -= lipgloss.Width(rt) + 1
		if fill < 0 {
			rt, fill = "", inner-used
		}
	}
	fill = max(0, fill)
	top.WriteString(bs.Render(strings.Repeat("─", fill)))
	if rt != "" {
		top.WriteString(t.Dim.Render(rt) + bs.Render("─"))
	}
	top.WriteString(bs.Render("╮"))

	out := []string{top.String()}
	side := bs.Render("│")
	for i := 0; i < h-2; i++ {
		l := ""
		if i < len(lines) {
			l = lines[i]
		}
		out = append(out, side+" "+fit(l, inner-2)+" "+side)
	}
	out = append(out, bs.Render("╰"+strings.Repeat("─", inner)+"╯"))
	return strings.Join(out, "\n")
}

var sparkRunes = []rune("▁▂▃▄▅▆▇█")

// sparkline renders the last w values scaled to their own min/max.
func sparkline(vals []float64, w int) string {
	if w <= 0 {
		return ""
	}
	if len(vals) > w {
		vals = vals[len(vals)-w:]
	}
	if len(vals) == 0 {
		return ""
	}
	lo, hi := minMax(vals)
	var b strings.Builder
	for _, v := range vals {
		i := 0
		if hi > lo {
			i = int((v - lo) / (hi - lo) * float64(len(sparkRunes)-1))
		}
		b.WriteRune(sparkRunes[clampInt(i, 0, len(sparkRunes)-1)])
	}
	return b.String()
}

// gauge is a thin horizontal bar for frac in [0,1].
func gauge(t Theme, frac float64, w int, c color.Color) string {
	if math.IsNaN(frac) {
		frac = 0
	}
	n := int(math.Round(math.Max(0, math.Min(1, frac)) * float64(w)))
	return t.fg(c).Render(strings.Repeat("━", n)) + t.Faint.Render(strings.Repeat("━", w-n))
}

var eighths = []rune(" ▁▂▃▄▅▆▇█")

// chart renders a block-column chart of h rows with a left y-axis.
func chart(t Theme, vals []float64, w, h int, prec int, c color.Color) []string {
	if h < 2 || w < 12 {
		return nil
	}
	lo, hi := minMax(vals)
	if hi-lo < 1e-9 {
		lo, hi = lo-1, hi+1
	}
	labels := []string{fmt.Sprintf("%.*f", prec, hi), fmt.Sprintf("%.*f", prec, (lo+hi)/2), fmt.Sprintf("%.*f", prec, lo)}
	lw := 0
	for _, l := range labels {
		lw = max(lw, len(l))
	}
	cw := w - lw - 2
	if len(vals) > cw {
		vals = vals[len(vals)-cw:]
	}
	rows := make([]strings.Builder, h)
	for _, v := range vals {
		level := (v - lo) / (hi - lo) * float64(h*8)
		for r := 0; r < h; r++ {
			base := float64((h - 1 - r) * 8)
			d := int(math.Round(level - base))
			rows[r].WriteRune(eighths[clampInt(d, 0, 8)])
		}
	}
	out := make([]string, h)
	cs := t.fg(c)
	for r := 0; r < h; r++ {
		lbl := ""
		switch r {
		case 0:
			lbl = labels[0]
		case h / 2:
			lbl = labels[1]
		case h - 1:
			lbl = labels[2]
		}
		out[r] = t.Dim.Render(fmt.Sprintf("%*s", lw, lbl)) + t.Faint.Render(" ┤") + cs.Render(rows[r].String())
	}
	return out
}

func minMax(v []float64) (lo, hi float64) {
	if len(v) == 0 {
		return 0, 0
	}
	lo, hi = v[0], v[0]
	for _, x := range v[1:] {
		lo, hi = math.Min(lo, x), math.Max(hi, x)
	}
	return
}

func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// wrap word-wraps plain text to width w.
func wrap(s string, w int) []string {
	if w <= 0 {
		return nil
	}
	return strings.Split(ansi.Wrap(s, w, ""), "\n")
}

// center places s in the middle of a w×h area.
func center(s string, w, h int) string {
	return lipgloss.Place(w, h, lipgloss.Center, lipgloss.Center, s)
}
