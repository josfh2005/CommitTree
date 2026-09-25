package app

import (
	"github.com/wailsapp/wails/v2/pkg/menu"
	"github.com/wailsapp/wails/v2/pkg/menu/keys"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// EventOpenSettings asks the frontend to open the Settings dialog.
const EventOpenSettings = "menu:settings"

// Menu is the macOS application menu. Wails' AppMenu role cannot take extra
// items, so the app menu is built by hand to hold Settings… (⌘,) where macOS
// apps keep it; Hide Others and Show All are lost because Wails exposes no
// way to trigger them. Edit and Window stay the standard role menus so copy,
// paste, undo and minimise keep working. Callbacks read a.ctx when clicked,
// after Startup has set it.
func (a *App) Menu() *menu.Menu {
	m := menu.NewMenu()
	appMenu := m.AddSubmenu("CommitTree")
	appMenu.AddText("Settings…", keys.CmdOrCtrl(","), func(*menu.CallbackData) {
		a.emit(EventOpenSettings, nil)
	})
	appMenu.AddSeparator()
	appMenu.AddText("Hide CommitTree", keys.CmdOrCtrl("h"), func(*menu.CallbackData) {
		runtime.Hide(a.ctx)
	})
	appMenu.AddSeparator()
	appMenu.AddText("Quit CommitTree", keys.CmdOrCtrl("q"), func(*menu.CallbackData) {
		runtime.Quit(a.ctx)
	})
	m.Append(menu.EditMenu())
	m.Append(menu.WindowMenu())
	return m
}
