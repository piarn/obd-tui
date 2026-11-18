package ui

import "charm.land/bubbles/v2/key"

// keyMap is the single source of truth for bindings: the bottom hint bar and
// the ? overlay are both rendered from it.
type keyMap struct {
	// global
	Quit, Help, NextTab, PrevTab, Tabs, Reconnect, Record, Pause key.Binding
	// motion
	Left, Down, Up, Right, Top, Bottom, HalfDown, HalfUp, Filter, Esc key.Binding
	// dashboard
	Graph key.Binding
	// codes / info
	Refresh, Clear key.Binding
	// console
	Insert, Send, History, Search, NextMatch, PrevMatch, Normal key.Binding
	// confirm
	Yes, No key.Binding
}

func kb(keys []string, k, desc string) key.Binding {
	return key.NewBinding(key.WithKeys(keys...), key.WithHelp(k, desc))
}

var keys = keyMap{
	Quit:      kb([]string{"q"}, "q", "quit"),
	Help:      kb([]string{"?"}, "?", "help"),
	NextTab:   kb([]string{"tab", "L", "]"}, "L/tab", "next tab"),
	PrevTab:   kb([]string{"shift+tab", "H", "["}, "H/S-tab", "prev tab"),
	Tabs:      kb([]string{"1", "2", "3", "4", "5"}, "1-5", "jump to tab"),
	Reconnect: kb([]string{"R"}, "R", "reconnect"),
	Record:    kb([]string{"w"}, "w", "record csv"),
	Pause:     kb([]string{"space", " "}, "space", "pause"),

	Left:     kb([]string{"h", "left"}, "h", "left"),
	Down:     kb([]string{"j", "down"}, "j", "down"),
	Up:       kb([]string{"k", "up"}, "k", "up"),
	Right:    kb([]string{"l", "right"}, "l", "right"),
	Top:      kb([]string{"g"}, "gg", "top"),
	Bottom:   kb([]string{"G"}, "G", "bottom"),
	HalfDown: kb([]string{"ctrl+d"}, "C-d", "half page down"),
	HalfUp:   kb([]string{"ctrl+u"}, "C-u", "half page up"),
	Filter:   kb([]string{"/"}, "/", "filter"),
	Esc:      kb([]string{"esc"}, "esc", "back"),

	Graph: kb([]string{"enter"}, "enter", "graph"),

	Refresh: kb([]string{"r"}, "r", "refresh"),
	Clear:   kb([]string{"c"}, "c", "clear codes"),

	Insert:    kb([]string{"i", "a", "enter"}, "i", "insert"),
	Send:      kb([]string{"enter"}, "enter", "send"),
	History:   kb([]string{"ctrl+p", "ctrl+n", "up", "down"}, "C-p/C-n", "history"),
	Search:    kb([]string{"/"}, "/", "search"),
	NextMatch: kb([]string{"n"}, "n", "next match"),
	PrevMatch: kb([]string{"N"}, "N", "prev match"),
	Normal:    kb([]string{"esc"}, "esc", "normal mode"),

	Yes: kb([]string{"y"}, "y", "confirm"),
	No:  kb([]string{"n", "esc"}, "n/esc", "cancel"),
}

// motion is the vim motion cluster shown as one hint.
var motionHint = key.NewBinding(key.WithKeys("j"), key.WithHelp("hjkl", "move"))
var vertHint = key.NewBinding(key.WithKeys("j"), key.WithHelp("j/k", "move"))
var tabHint = key.NewBinding(key.WithKeys("L"), key.WithHelp("H/L", "tabs"))

// shortHelp returns the hint-bar bindings for the current context.
func (m *Model) shortHelp() []key.Binding {
	switch m.mode {
	case modeFilter:
		return []key.Binding{hint("enter", "apply"), hint("esc", "clear")}
	case modeInsert:
		return []key.Binding{keys.Send, keys.History, keys.Normal}
	case modeConfirm:
		return []key.Binding{keys.Yes, keys.No}
	case modeHelp:
		return []key.Binding{hint("?/esc", "close help")}
	}
	switch m.tab {
	case tabDash:
		if m.dash.detail {
			return []key.Binding{vertHint, hint("esc/enter", "back"), keys.Pause, keys.Record, keys.Help, keys.Quit}
		}
		return []key.Binding{motionHint, keys.Graph, keys.Filter, keys.Pause, keys.Record, tabHint, keys.Help, keys.Quit}
	case tabCodes:
		return []key.Binding{vertHint, keys.Refresh, keys.Clear, keys.Filter, tabHint, keys.Help, keys.Quit}
	case tabModules:
		return moduleHelp(&m.mods)
	case tabInfo:
		return []key.Binding{vertHint, keys.Refresh, keys.Reconnect, tabHint, keys.Help, keys.Quit}
	case tabConsole:
		return []key.Binding{keys.Insert, vertHint, keys.Search, keys.NextMatch, tabHint, keys.Help, keys.Quit}
	}
	return nil
}

type helpSection struct {
	title string
	binds []key.Binding
}

func fullHelp() []helpSection {
	return []helpSection{
		{"Global", []key.Binding{keys.NextTab, keys.PrevTab, hint("gt/gT", "next/prev tab"), keys.Tabs, keys.Pause, keys.Record, keys.Reconnect, keys.Help, keys.Quit}},
		{"Motion", []key.Binding{hint("h j k l", "move"), keys.Top, keys.Bottom, keys.HalfDown, keys.HalfUp, keys.Filter, keys.Esc}},
		{"Dashboard", []key.Binding{keys.Graph, hint("j/k", "prev/next sensor")}},
		{"Codes & Info", []key.Binding{keys.Refresh, keys.Clear}},
		{"Modules", []key.Binding{hint("s", "scan all modules"), hint("D", "deep scan (29-bit sweep)"), hint("c", "clear module"),
			hint("C", "clear all faulty"), hint("m", "cycle make profile"), hint("a", "show absent"), hint("enter", "focus details"), hint("esc", "stop scan")}},
		{"Console", []key.Binding{keys.Insert, keys.Send, keys.History, keys.Search, keys.NextMatch, keys.PrevMatch, keys.Normal}},
	}
}

// hint is a display-only binding (bubbles/help hides bindings without keys).
func hint(k, desc string) key.Binding {
	return key.NewBinding(key.WithKeys(k), key.WithHelp(k, desc))
}
