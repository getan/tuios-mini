package input

import (
	tea "charm.land/bubbletea/v2"
	"github.com/getan/tuios-mini/internal/app"
)

// handleGitDrawerInput owns every key while a Git history drawer is open.
// The drawers are read-only: j/k walk, g/G jump, enter chooses a repository,
// and esc closes. Nothing behind the panel receives a key.
func handleGitDrawerInput(msg tea.KeyPressMsg, o *app.OS) (*app.OS, tea.Cmd) {
	switch msg.String() {
	case "j", "down":
		o.GitDrawerMove(1)
	case "k", "up":
		o.GitDrawerMove(-1)
	case "pgdown", "ctrl+d", "space":
		o.GitDrawerMove(10)
	case "pgup", "ctrl+u":
		o.GitDrawerMove(-10)
	case "g", "home":
		o.GitDrawerEdge(false)
	case "G", "end":
		o.GitDrawerEdge(true)
	case "enter":
		return o, o.GitDrawerActivate()
	case "esc", "q":
		o.GitDrawerClose()
	}
	return o, nil
}
