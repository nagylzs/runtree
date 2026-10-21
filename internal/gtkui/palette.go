package gtkui

// The palettes define the named colors of main.css (@rt_...) for a dark and a light theme. The dark one has the
// colors that runtree was designed with; the status colors (Solarized) are the same in both.

const statusColors = `
@define-color rt_status_fg #002b36;
@define-color rt_waiting #cb4b16;
@define-color rt_frozen #d33682;
@define-color rt_running #268bd2;
@define-color rt_success #859900;
@define-color rt_failed #dc322f;
@define-color rt_cancelled #2aa198;
@define-color rt_paused #6c71c4;
@define-color rt_warning #b58900;
@define-color rt_toast_bg rgba(0, 0, 0, 0.8);
@define-color rt_toast_fg #ffffff;
@define-color rt_type_fg #4b4b4b;
@define-color rt_type_bg #ffffff;
`

const darkPalette = statusColors + `
@define-color rt_node_hover_fg #a1b5b0;
@define-color rt_node_hover_bg #38464b;
@define-color rt_node_border rgba(0, 0, 0, 0.25);
@define-color rt_node_selected_fg #ffffff;
@define-color rt_node_selected_bg #205246;
@define-color rt_node_disabled_border rgba(0, 0, 0, 0.15);
@define-color rt_node_disabled_bg rgba(38, 48, 52, 0.55);
@define-color rt_node_disabled_fg rgba(204, 215, 212, 0.45);
@define-color rt_node_disabled_selected_bg rgba(46, 179, 152, 0.75);
@define-color rt_node_disabled_selected_fg rgba(255, 255, 255, 0.8);
@define-color rt_sigkill #dc322f;
@define-color rt_sigint #cb4b16;
@define-color rt_sighup #6c71c4;
@define-color rt_sigabrt #cb4b16;
@define-color rt_signal_bg #002b36;
`

const lightPalette = statusColors + `
@define-color rt_node_hover_fg #1f3833;
@define-color rt_node_hover_bg #dfe8e6;
@define-color rt_node_border rgba(0, 0, 0, 0.12);
@define-color rt_node_selected_fg #ffffff;
@define-color rt_node_selected_bg #2a7a66;
@define-color rt_node_disabled_border rgba(0, 0, 0, 0.08);
@define-color rt_node_disabled_bg rgba(210, 218, 220, 0.55);
@define-color rt_node_disabled_fg rgba(40, 52, 50, 0.5);
@define-color rt_node_disabled_selected_bg rgba(42, 122, 102, 0.6);
@define-color rt_node_disabled_selected_fg rgba(255, 255, 255, 0.9);
@define-color rt_sigkill #c0262b;
@define-color rt_sigint #b03f0f;
@define-color rt_sighup #4d52a8;
@define-color rt_sigabrt #b03f0f;
@define-color rt_signal_bg #f6f5f4;
`

// paletteCSS returns the stylesheet of runtree with the palette of a dark or a light theme
func paletteCSS(dark bool) string {
	if dark {
		return darkPalette + mainCss
	}
	return lightPalette + mainCss
}

// color is an opaque color, its components are from 0 to 1
type color struct {
	r, g, b float32
}

// terminalColors are the colors of the terminals: the text, the background and the 16 colors of the palette
type terminalColors struct {
	fg, bg  color
	palette []color
}

// lightTerminal are the colors of the terminals with a light theme (Tango). With a dark theme, the terminals have the
// default colors of VTE.
var lightTerminal = terminalColors{
	fg: rgb(0x2e3436),
	bg: rgb(0xffffff),
	palette: []color{
		rgb(0x2e3436), rgb(0xcc0000), rgb(0x4e9a06), rgb(0xc4a000),
		rgb(0x3465a4), rgb(0x75507b), rgb(0x06989a), rgb(0xd3d7cf),
		rgb(0x555753), rgb(0xef2929), rgb(0x4e9a06), rgb(0xc4a000),
		rgb(0x729fcf), rgb(0xad7fa8), rgb(0x06989a), rgb(0xeeeeec),
	},
}

// rgb returns a color from its 0xRRGGBB value
func rgb(c uint32) color {
	return color{float32(c>>16&0xff) / 255, float32(c>>8&0xff) / 255, float32(c&0xff) / 255}
}
