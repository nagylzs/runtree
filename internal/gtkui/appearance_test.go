package gtkui

import (
	"path/filepath"
	"regexp"
	"testing"

	"github.com/diamondburned/gotk4/pkg/gdk/v4"
)

// Every named color of main.css is defined by both palettes, and the palettes define the same names
func TestPalettes(t *testing.T) {
	used := regexp.MustCompile(`@(rt_[a-z_]+)`)
	defined := regexp.MustCompile(`@define-color (rt_[a-z_]+) `)
	names := func(palette string) map[string]bool {
		m := map[string]bool{}
		for _, match := range defined.FindAllStringSubmatch(palette, -1) {
			if m[match[1]] {
				t.Errorf("%s is defined twice", match[1])
			}
			m[match[1]] = true
		}
		return m
	}
	dark, light := names(darkPalette), names(lightPalette)
	for _, match := range used.FindAllStringSubmatch(mainCss, -1) {
		if !dark[match[1]] || !light[match[1]] {
			t.Errorf("@%s is not defined in both palettes", match[1])
		}
	}
	for name := range dark {
		if !light[name] {
			t.Errorf("%s is only in the dark palette", name)
		}
	}
	if len(dark) != len(light) {
		t.Errorf("the dark palette has %d colors, the light one %d", len(dark), len(light))
	}
	if len(lightTerminal.palette) != 16 {
		t.Errorf("the terminal palette has %d colors", len(lightTerminal.palette))
	}
}

func TestIsDarkTheme(t *testing.T) {
	for _, tc := range []struct {
		r, g, b float32
		dark    bool
	}{
		{1, 1, 1, true},          // white text
		{0.8, 0.84, 0.83, true},  // the light grey text of Adwaita dark
		{0, 0, 0, false},         // black text
		{0.2, 0.2, 0.2, false},   // the dark grey text of Adwaita
		{0.5, 0.25, 0.25, false}, // a dark red
	} {
		c := gdk.NewRGBA(tc.r, tc.g, tc.b, 1)
		if got := isDarkTheme(&c); got != tc.dark {
			t.Errorf("text %v %v %v: dark is %v", tc.r, tc.g, tc.b, got)
		}
	}
	if !isDarkTheme(nil) {
		t.Error("an unknown theme is not dark")
	}
}

func TestPortalColorScheme(t *testing.T) {
	if v := portalColorScheme(1); v == nil || !*v {
		t.Error("1 is not dark")
	}
	if v := portalColorScheme(2); v == nil || *v {
		t.Error("2 is not light")
	}
	if v := portalColorScheme(0); v != nil {
		t.Error("0 (no preference) is known")
	}
}

func TestSettingsFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "runtree", "settings.json")
	s, err := loadSettings(path)
	if err != nil || s != (Settings{}) {
		t.Fatalf("a missing file: %+v, %v", s, err)
	}
	want := Settings{Appearance: "light", Buttons: "icons"}
	if err := saveSettings(path, want); err != nil {
		t.Fatal(err)
	}
	if s, err = loadSettings(path); err != nil || s != want {
		t.Errorf("loaded %+v, %v", s, err)
	}
	for name, mode := range map[string]ButtonMode{"auto": ButtonsAuto, "text": ButtonsText, "icons": ButtonsIcons} {
		if m, ok := ParseButtonMode(name); !ok || m != mode {
			t.Errorf("%s: %v, %v", name, m, ok)
		}
		if buttonModeNames[mode] != name {
			t.Errorf("the name of %v is %s", mode, buttonModeNames[mode])
		}
	}
	if _, ok := ParseButtonMode(""); ok {
		t.Error("an empty mode is valid")
	}
}
