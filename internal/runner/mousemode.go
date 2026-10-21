package runner

import "sync/atomic"

// mouseModes are the private modes of xterm that send the mouse events to the program (DECSET 9, 1000-1003)
var mouseModes = map[string]uint32{"9": 1, "1000": 2, "1001": 4, "1002": 8, "1003": 16}

// maxModeParams is the longest parameter list of a private mode sequence that is parsed
const maxModeParams = 64

// mouseModeTracker follows the output of a process (it is an io.Writer), and tells if the process turned on mouse
// tracking: then the terminal sends it the mouse clicks, e.g. the middle button does not paste. It parses
// "ESC [ ? Pm h" and "ESC [ ? Pm l", also when they are split between writes, and "ESC c" (reset).
type mouseModeTracker struct {
	modes  atomic.Uint32 // the bits of mouseModes that are set
	state  int
	params []byte
}

// the states of mouseModeTracker
const (
	mouseGround  = iota
	mouseEscape  // after ESC
	mouseCSI     // after ESC [
	mousePrivate // after ESC [ ?, in the parameters
)

func (m *mouseModeTracker) Write(p []byte) (int, error) {
	modes := m.modes.Load()
	for _, b := range p {
		switch m.state {
		case mouseGround:
			if b == 0x1b {
				m.state = mouseEscape
			}
		case mouseEscape:
			switch b {
			case '[':
				m.state = mouseCSI
			case 'c':
				modes = 0
				m.state = mouseGround
			case 0x1b:
			default:
				m.state = mouseGround
			}
		case mouseCSI:
			if b == '?' {
				m.state, m.params = mousePrivate, m.params[:0]
			} else {
				m.state = mouseGround
			}
		case mousePrivate:
			switch {
			case b >= '0' && b <= '9' || b == ';':
				if len(m.params) < maxModeParams {
					m.params = append(m.params, b)
				}
			case b == 'h' || b == 'l':
				modes = applyMouseModes(modes, m.params, b == 'h')
				m.state = mouseGround
			case b == 0x1b:
				m.state = mouseEscape
			default:
				m.state = mouseGround
			}
		}
	}
	m.modes.Store(modes)
	return len(p), nil
}

// applyMouseModes sets or resets the mouse modes in the parameters (e.g. "1000;1006") of a private mode sequence
func applyMouseModes(modes uint32, params []byte, set bool) uint32 {
	start := 0
	for i := 0; i <= len(params); i++ {
		if i < len(params) && params[i] != ';' {
			continue
		}
		if bit, ok := mouseModes[string(params[start:i])]; ok {
			if set {
				modes |= bit
			} else {
				modes &^= bit
			}
		}
		start = i + 1
	}
	return modes
}

// on tells if a mouse mode is set. It can be called from any goroutine.
func (m *mouseModeTracker) on() bool {
	return m.modes.Load() != 0
}
