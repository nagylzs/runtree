package gtkui

import (
	"context"
	"log/slog"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/diamondburned/gotk4/pkg/core/glib"
	"github.com/diamondburned/gotk4/pkg/gdk/v4"
	"github.com/diamondburned/gotk4/pkg/gio/v2"
	glib2 "github.com/diamondburned/gotk4/pkg/glib/v2"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"
)

// Appearance is the color scheme of the GUI: the variant of the GTK theme, and the palette of runtree's own colors
// (palette.go) and of the terminals
type Appearance string

const (
	AppearanceSystem Appearance = "system" // the color scheme of the desktop, dark when it is not known
	AppearanceDark   Appearance = "dark"
	AppearanceLight  Appearance = "light"
)

var appearance = AppearanceSystem

// SetAppearance sets the appearance (runtree --appearance) before the GUI starts; it overrides the saved setting
func SetAppearance(a Appearance) {
	appearance = a
	appearanceFixed = true
}

// the state of the appearance, used from the main loop
var (
	appearanceFixed   bool  // set by SetAppearance, the saved setting is not used
	systemPrefersDark *bool // the color scheme of the desktop, nil when it is not known
	cssProvider       *gtk.CSSProvider
	paletteDark       bool // the palette that is loaded (when cssProvider is not nil)
)

// wantDark tells if the appearance asks for the dark variant of the theme
func wantDark() bool {
	switch appearance {
	case AppearanceLight:
		return false
	case AppearanceSystem:
		if systemPrefersDark != nil {
			return *systemPrefersDark
		}
	}
	return true // runtree was designed for dark themes
}

// startAppearance loads runtree's stylesheet, applies the appearance, and follows the color scheme of the desktop.
// It is called when the main window is created, from the main loop.
func startAppearance(window *gtk.ApplicationWindow) {
	cssProvider = gtk.NewCSSProvider()
	gtk.StyleContextAddProviderForDisplay(gdk.DisplayGetDefault(), cssProvider, gtk.STYLE_PROVIDER_PRIORITY_APPLICATION)
	paletteDark = wantDark()
	cssProvider.LoadFromData(paletteCSS(paletteDark))
	applyAppearance()
	watchSystemColorScheme()
	// the theme can change in other ways too (e.g. GTK_THEME, the theme of the desktop)
	glib.TimeoutAdd(1000, func() bool {
		updatePalette()
		return true
	})
}

// applyAppearance asks for the variant of the theme, then loads the palette that fits it. It must be called from the
// main loop.
func applyAppearance() {
	setThemeVariant(wantDark())
	updatePalette()
	// GTK changes the style of the theme for the next frame
	glib.TimeoutAdd(100, func() bool {
		updatePalette()
		return false
	})
}

// updatePalette loads runtree's palette and sets the colors of the terminals for the theme in use: its text color
// tells if it is dark (GTK_THEME may set a variant that differs from the appearance). It must be called from the
// main loop.
func updatePalette() {
	if cssProvider == nil || Application == nil || Application.window == nil {
		return
	}
	dark := isDarkTheme(Application.window.Color())
	showThemeNote(dark)
	if dark == paletteDark {
		return
	}
	paletteDark = dark
	slog.Debug("palette", "dark", dark)
	cssProvider.LoadFromData(paletteCSS(dark))
	Application.lock.Lock()
	terminals := make([]*nodeTerminal, 0, len(Application.terminals))
	for _, nt := range Application.terminals {
		terminals = append(terminals, nt)
	}
	Application.lock.Unlock()
	for _, nt := range terminals {
		setTerminalPalette(nt)
	}
}

// setTerminalPalette sets the colors of a terminal for the palette in use
func setTerminalPalette(nt *nodeTerminal) {
	if paletteDark {
		setTerminalColors(nt.term, nil)
	} else {
		setTerminalColors(nt.term, &lightTerminal)
	}
}

// isDarkTheme tells if a theme is dark from its text color: light text is on a dark background
func isDarkTheme(text *gdk.RGBA) bool {
	if text == nil {
		return true
	}
	return 0.2126*text.Red()+0.7152*text.Green()+0.0722*text.Blue() > 0.5
}

// setSystemColorScheme is called when the color scheme of the desktop is known or changed
func setSystemColorScheme(dark *bool) {
	if dark != nil && systemPrefersDark != nil && *dark == *systemPrefersDark {
		return
	}
	systemPrefersDark = dark
	slog.Debug("the color scheme of the desktop: " + colorSchemeName(dark))
	if appearance == AppearanceSystem {
		applyAppearance()
	}
}

// portalColorScheme converts the color-scheme of the settings portal (0: no preference, 1: dark, 2: light)
func portalColorScheme(v uint32) *bool {
	switch v {
	case 1:
		return boolPtr(true)
	case 2:
		return boolPtr(false)
	}
	return nil
}

// colorSchemeName is "dark", "light" or "unknown" (nil)
func colorSchemeName(dark *bool) string {
	switch {
	case dark == nil:
		return "unknown"
	case *dark:
		return "dark"
	}
	return "light"
}

// themeNote tells in the settings menu when the theme in use does not follow the appearance (e.g. a theme that has
// only a dark variant), see showThemeNote
var themeNote *gtk.Label

// showThemeNote shows themeNote when the theme in use is dark and a light appearance was chosen, or the other way
func showThemeNote(dark bool) {
	if themeNote == nil {
		return
	}
	mismatch := dark != wantDark()
	if mismatch {
		text := "The theme in use is light"
		if dark {
			text = "The theme in use is dark"
		}
		if theme := os.Getenv("GTK_THEME"); theme != "" {
			text += " (GTK_THEME=" + theme + ")"
		}
		setText(themeNote, text+".")
	}
	if themeNote.Visible() != mismatch {
		themeNote.SetVisible(mismatch)
	}
}

func boolPtr(b bool) *bool {
	return &b
}

// watchSystemColorScheme reads the color scheme of the desktop, and follows its changes: on macOS from the defaults,
// elsewhere from the settings portal of the desktop (org.freedesktop.appearance color-scheme), when there is a
// session bus. It is called from the main loop.
func watchSystemColorScheme() {
	if runtime.GOOS == "darwin" {
		go watchMacColorScheme()
		return
	}
	if os.Getenv("DBUS_SESSION_BUS_ADDRESS") == "" {
		return // without it, GIO would start a bus (X11 autolaunch)
	}
	conn, err := gio.BusGetSync(context.Background(), gio.BusTypeSession)
	if err != nil {
		slog.Debug("no session bus for the color scheme", "error", err)
		return
	}
	const (
		portal    = "org.freedesktop.portal.Desktop"
		path      = "/org/freedesktop/portal/desktop"
		settings  = "org.freedesktop.portal.Settings"
		namespace = "org.freedesktop.appearance"
		key       = "color-scheme"
	)
	args := glib2.NewVariantTuple([]*glib2.Variant{glib2.NewVariantString(namespace), glib2.NewVariantString(key)})
	conn.Call(context.Background(), portal, path, settings, "ReadOne", args, nil, gio.DBusCallFlagsNone, 2000,
		func(res gio.AsyncResulter) {
			reply, err := conn.CallFinish(res)
			if err != nil {
				slog.Debug("no color scheme from the settings portal", "error", err)
				return
			}
			// (v) with a u
			setSystemColorScheme(portalColorScheme(reply.ChildValue(0).Variant().Uint32()))
		})
	conn.SignalSubscribe(portal, settings, "SettingChanged", path, namespace, gio.DBusSignalFlagsNone,
		func(_ *gio.DBusConnection, _, _, _, _ string, params *glib2.Variant) {
			// (ssv)
			if params.ChildValue(1).String() == key {
				setSystemColorScheme(portalColorScheme(params.ChildValue(2).Variant().Uint32()))
			}
		})
}

// watchMacColorScheme reads the appearance of macOS every few seconds (AppleInterfaceStyle is Dark in dark mode, it
// is not set in light mode). It runs in a goroutine, the changes go to the main loop.
func watchMacColorScheme() {
	var last *bool
	for {
		out, err := exec.Command("defaults", "read", "-g", "AppleInterfaceStyle").Output()
		dark := err == nil && strings.TrimSpace(string(out)) == "Dark"
		if last == nil || *last != dark {
			last = &dark
			runOnMain(func() {
				setSystemColorScheme(boolPtr(dark))
			})
		}
		time.Sleep(5 * time.Second)
	}
}
