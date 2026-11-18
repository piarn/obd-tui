package ui

import (
	"image/color"

	"charm.land/lipgloss/v2"
)

// Theme is the palette plus derived styles. Rebuilt when the terminal
// reports its background colour.
type Theme struct {
	Accent, Accent2, Fg, Muted, Subtle, Surface, Good, Warn, Bad, OnAccent color.Color

	Text, Dim, Faint, Bold, Key lipgloss.Style
}

func NewTheme(dark bool) Theme {
	c := lipgloss.Color
	var t Theme
	if dark {
		t = Theme{
			Accent: c("#7AA2F7"), Accent2: c("#BB9AF7"), Fg: c("#C0CAF5"), Muted: c("#7A82AA"),
			Subtle: c("#3B4261"), Surface: c("#292E42"), Good: c("#9ECE6A"), Warn: c("#E0AF68"),
			Bad: c("#F7768E"), OnAccent: c("#1A1B26"),
		}
	} else {
		t = Theme{
			Accent: c("#2E7DE9"), Accent2: c("#9854F1"), Fg: c("#3760BF"), Muted: c("#6172B0"),
			Subtle: c("#B4B9D2"), Surface: c("#DCDEE8"), Good: c("#587539"), Warn: c("#8C6C3E"),
			Bad: c("#E0305A"), OnAccent: c("#F5F6FA"),
		}
	}
	t.Text = lipgloss.NewStyle().Foreground(t.Fg)
	t.Dim = lipgloss.NewStyle().Foreground(t.Muted)
	t.Faint = lipgloss.NewStyle().Foreground(t.Subtle)
	t.Bold = lipgloss.NewStyle().Foreground(t.Fg).Bold(true)
	t.Key = lipgloss.NewStyle().Foreground(t.Accent).Bold(true)
	return t
}

func (t Theme) fg(c color.Color) lipgloss.Style { return lipgloss.NewStyle().Foreground(c) }

// chip renders a small coloured label.
func (t Theme) chip(text string, bg color.Color) string {
	return lipgloss.NewStyle().Background(bg).Foreground(t.OnAccent).Bold(true).Padding(0, 1).Render(text)
}
