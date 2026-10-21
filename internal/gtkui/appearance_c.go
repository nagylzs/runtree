package gtkui

// #cgo pkg-config: gtk4 vte-2.91-gtk4
// #include <gtk/gtk.h>
// #include <vte/vte.h>
//
// // set_color_scheme sets gtk-interface-color-scheme (GTK 4.20 and later; the values of GtkInterfaceColorScheme:
// // 1 default, 2 dark, 3 light), it tells if the setting exists
// static gboolean set_color_scheme(int scheme) {
//     GtkSettings *settings = gtk_settings_get_default();
//     if (settings == NULL ||
//         g_object_class_find_property(G_OBJECT_GET_CLASS(settings), "gtk-interface-color-scheme") == NULL) {
//         return FALSE;
//     }
//     g_object_set(settings, "gtk-interface-color-scheme", scheme, NULL);
//     return TRUE;
// }
//
// static void set_prefer_dark(gboolean dark) {
//     GtkSettings *settings = gtk_settings_get_default();
//     if (settings != NULL) {
//         g_object_set(settings, "gtk-application-prefer-dark-theme", dark, NULL);
//     }
// }
//
// static void set_terminal_colors(uintptr_t terminal, const GdkRGBA *fg, const GdkRGBA *bg, const GdkRGBA *palette,
//                                 gsize size) {
//     vte_terminal_set_colors(VTE_TERMINAL((gpointer)terminal), fg, bg, palette, size);
// }
//
// static void set_default_terminal_colors(uintptr_t terminal) {
//     vte_terminal_set_default_colors(VTE_TERMINAL((gpointer)terminal));
// }
import "C"

import (
	"runtime"

	vte "github.com/nagylzs/gotk4-vte"
)

// the values of GtkInterfaceColorScheme (GTK 4.20)
const (
	colorSchemeDefault = 1
	colorSchemeDark    = 2
	colorSchemeLight   = 3
)

// setThemeVariant asks for the dark or the light variant of the GTK theme (unless GTK_THEME sets it). It must be
// called from the main loop.
func setThemeVariant(dark bool) {
	scheme := colorSchemeLight
	if dark {
		scheme = colorSchemeDark
	}
	// gtk-application-prefer-dark-theme is deprecated since GTK 4.20, but the older versions only have that
	if C.set_color_scheme(C.int(scheme)) == C.FALSE {
		C.set_prefer_dark(gboolean(dark))
	}
}

func gboolean(b bool) C.gboolean {
	if b {
		return C.TRUE
	}
	return C.FALSE
}

func rgba(c color) C.GdkRGBA {
	return C.GdkRGBA{red: C.float(c.r), green: C.float(c.g), blue: C.float(c.b), alpha: 1}
}

// setTerminalColors sets the colors of a terminal, or its default colors (nil). It must be called from the main loop.
func setTerminalColors(term *vte.Terminal, colors *terminalColors) {
	defer runtime.KeepAlive(term)
	p := C.uintptr_t(term.Widget.Object.Native())
	if colors == nil {
		C.set_default_terminal_colors(p)
		return
	}
	fg, bg := rgba(colors.fg), rgba(colors.bg)
	palette := make([]C.GdkRGBA, len(colors.palette))
	for i, c := range colors.palette {
		palette[i] = rgba(c)
	}
	C.set_terminal_colors(p, &fg, &bg, &palette[0], C.gsize(len(palette)))
}
