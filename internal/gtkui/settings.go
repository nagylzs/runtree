package gtkui

import (
	"encoding/json"
	"errors"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
)

// Settings are the preferences of the GUI that are kept between runs, in settingsPath. They are chosen in the
// settings menu (settingsmenu.go); the command line options override them for one run.
type Settings struct {
	Appearance string `json:"appearance,omitempty"` // system (the default), dark or light, see Appearance
	Buttons    string `json:"buttons,omitempty"`    // auto (the default), text or icons, see ButtonMode
}

// settingsPath is the file of the settings: runtree/settings.json in the configuration directory of the user (e.g.
// ~/.config on Linux, ~/Library/Application Support on macOS)
func settingsPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "runtree", "settings.json"), nil
}

// loadSettings reads the settings; a missing file is the default settings
func loadSettings(path string) (Settings, error) {
	var s Settings
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return s, err
	}
	err = json.Unmarshal(data, &s)
	return s, err
}

// saveSettings writes the settings: a new file is renamed to path, so it is never half written
func saveSettings(path string, s Settings) error {
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".new"
	if err := os.WriteFile(tmp, append(data, '\n'), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// savedSettings are the settings in the file (a command line option does not change them)
var savedSettings Settings

// loadSavedSettings applies the saved settings, unless the command line options set them. It is called before the
// GUI starts.
func loadSavedSettings() {
	path, err := settingsPath()
	if err != nil {
		return
	}
	s, err := loadSettings(path)
	if err != nil {
		slog.Warn("could not read the settings", "file", path, "error", err)
		return
	}
	savedSettings = s
	if a := Appearance(s.Appearance); !appearanceFixed && validAppearance(a) {
		appearance = a
	}
	if mode, ok := ParseButtonMode(s.Buttons); !buttonModeFixed && ok {
		buttonMode = mode
	}
}

// saveChangedSettings saves the settings after change changed one of them in savedSettings, from the main loop
func saveChangedSettings(change func(s *Settings)) {
	change(&savedSettings)
	path, err := settingsPath()
	if err == nil {
		err = saveSettings(path, savedSettings)
	}
	if err != nil {
		slog.Warn("could not save the settings", "error", err)
		ShowToast("Could not save the settings: " + err.Error())
	}
}

// validAppearance tells if a is an appearance
func validAppearance(a Appearance) bool {
	return a == AppearanceSystem || a == AppearanceDark || a == AppearanceLight
}
