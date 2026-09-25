package app

import (
	"testing"

	"github.com/wailsapp/wails/v2/pkg/menu"
	"github.com/wailsapp/wails/v2/pkg/menu/keys"
)

func TestMenuHasSettingsAndTheStandardMenus(t *testing.T) {
	srv := fakeOllama(t, nil)
	a, _, ev := newAIApp(t, srv.URL)
	m := a.Menu()

	if len(m.Items) != 3 || m.Items[1].Role != menu.EditMenuRole || m.Items[2].Role != menu.WindowMenuRole {
		t.Fatalf("top-level items = %+v", m.Items)
	}
	appMenu := m.Items[0].SubMenu
	var labels []string
	var settings *menu.MenuItem
	for _, item := range appMenu.Items {
		labels = append(labels, item.Label)
		if item.Label == "Settings…" {
			settings = item
		}
	}
	if settings == nil {
		t.Fatalf("no Settings… in %q", labels)
	}
	acc := settings.Accelerator
	if acc == nil || acc.Key != "," || len(acc.Modifiers) != 1 || acc.Modifiers[0] != keys.CmdOrCtrlKey {
		t.Fatalf("Settings… accelerator = %+v", acc)
	}
	settings.Click(&menu.CallbackData{MenuItem: settings})
	ev.wait(t, EventOpenSettings)
}
