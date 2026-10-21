package gtkui

import (
	"github.com/diamondburned/gotk4/pkg/gtk/v4"
	"github.com/nagylzs/runtree/internal/version"
)

// newSettingsMenu returns the menu button of the settings: the appearance and the operation buttons. A choice is
// applied at once, and saved (see Settings).
func newSettingsMenu() *gtk.MenuButton {
	menu := gtk.NewMenuButton()
	menu.SetChild(newOpIconView(iconMenu))
	menu.SetTooltipText("Settings")

	box := gtk.NewBox(gtk.OrientationVertical, 8)
	box.SetMarginStart(5)
	box.SetMarginEnd(5)

	appearances := []struct {
		label string
		value Appearance
		tip   string
	}{
		{"System", AppearanceSystem, "The color scheme of the desktop (dark when it is not known)"},
		{"Dark", AppearanceDark, "A dark theme"},
		{"Light", AppearanceLight, "A light theme"},
	}
	box.Append(popoverHeading("Appearance"))
	row := linkedRow()
	var group *gtk.ToggleButton
	for _, a := range appearances {
		tb := gtk.NewToggleButtonWithLabel(a.label)
		tb.SetTooltipText(a.tip)
		tb.SetGroup(group)
		group = tb
		tb.SetActive(a.value == appearance)
		tb.ConnectToggled(func() {
			if !tb.Active() || appearance == a.value {
				return
			}
			appearance = a.value
			applyAppearance()
			saveChangedSettings(func(s *Settings) { s.Appearance = string(a.value) })
		})
		row.Append(tb)
	}
	box.Append(row)
	themeNote = gtk.NewLabel("")
	themeNote.AddCSSClass("dim-label")
	themeNote.SetWrap(true)
	themeNote.SetMaxWidthChars(30)
	themeNote.SetXAlign(0)
	themeNote.SetVisible(false)
	box.Append(themeNote)

	modes := []struct {
		label string
		value ButtonMode
		tip   string
	}{
		{"Auto", ButtonsAuto, "Icons and texts, only the icons when the texts do not fit"},
		{"Text", ButtonsText, "Icons and texts"},
		{"Icons", ButtonsIcons, "Only the icons (the tooltips tell the operations)"},
	}
	box.Append(popoverHeading("Operation buttons"))
	row = linkedRow()
	group = nil
	for _, m := range modes {
		tb := gtk.NewToggleButtonWithLabel(m.label)
		tb.SetTooltipText(m.tip)
		tb.SetGroup(group)
		group = tb
		tb.SetActive(m.value == buttonMode)
		tb.ConnectToggled(func() {
			if !tb.Active() || buttonMode == m.value {
				return
			}
			buttonMode = m.value
			if Application.pane != nil {
				Application.pane.toolBox.header.relayout()
			}
			saveChangedSettings(func(s *Settings) { s.Buttons = buttonModeNames[m.value] })
		})
		row.Append(tb)
	}
	box.Append(row)

	box.Append(gtk.NewSeparator(gtk.OrientationHorizontal))
	about := gtk.NewLabel("runtree " + version.String())
	about.AddCSSClass("dim-label")
	about.SetSelectable(true)
	about.SetWrap(true)
	about.SetMaxWidthChars(30)
	about.SetXAlign(0)
	box.Append(about)

	popover := gtk.NewPopover()
	popover.SetChild(scrollable(box))
	menu.SetPopover(popover)
	return menu
}

// popoverHeading is a bold label of a section of a popover
func popoverHeading(text string) *gtk.Label {
	l := gtk.NewLabel(text)
	l.SetXAlign(0)
	l.AddCSSClass("popover-heading")
	return l
}

// linkedRow is a row of buttons without space between them
func linkedRow() *gtk.Box {
	row := gtk.NewBox(gtk.OrientationHorizontal, 0)
	row.AddCSSClass("linked")
	return row
}
