package main

import (
	"context"
	"embed"
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	goruntime "runtime"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/menu"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/mac"

	"git-ui/internal/ai/chatstore"
	"git-ui/internal/ai/prompts"
	"git-ui/internal/ai/settings"
	"git-ui/internal/app"
	"git-ui/internal/credprompt"
	"git-ui/internal/repos"
	"git-ui/internal/update"
)

//go:embed all:frontend/dist
var assets embed.FS

// wailsJSON is the only source of the running version (info.productVersion).
//
//go:embed wails.json
var wailsJSON []byte

// productVersion is the version in wails.json.
func productVersion() (string, error) {
	var cfg struct {
		Info struct {
			ProductVersion string `json:"productVersion"`
		} `json:"info"`
	}
	if err := json.Unmarshal(wailsJSON, &cfg); err != nil {
		return "", err
	}
	return cfg.Info.ProductVersion, nil
}

func main() {
	// Relaunched by git or ssh as the credential helper / askpass program:
	// answer through the running app and exit, before any GUI start.
	if os.Getenv(credprompt.EnvSock) != "" {
		os.Exit(credprompt.RunHelper(os.Args[1:], os.Getenv, os.Stdin, os.Stdout))
	}

	app.FixPath()

	path, err := repos.DefaultPath()
	if err != nil {
		log.Fatal(err)
	}
	store, err := repos.Open(path)
	if err != nil {
		log.Fatal(err)
	}
	api := app.New(store)

	settingsPath, err := settings.DefaultPath()
	if err != nil {
		log.Fatal(err)
	}
	chatsDir, err := chatstore.DefaultDir()
	if err != nil {
		log.Fatal(err)
	}
	promptsDir, err := prompts.DefaultDir()
	if err != nil {
		log.Fatal(err)
	}
	app.WithAI(api, app.AIDeps{
		SettingsPath: settingsPath,
		Chats:        chatstore.New(chatsDir),
		Prompts:      prompts.New(promptsDir),
	})

	// Credential prompts and in-app updates both need this executable, with
	// symlinks resolved; without it neither is available.
	exe, err := os.Executable()
	if err == nil {
		exe, err = filepath.EvalSymlinks(exe)
	}
	if err != nil {
		log.Printf("credential prompts and updates off: %v", err)
	} else {
		// Credential prompts are optional: without them (Windows, no socket)
		// network commands behave as before.
		err = app.WithCredentials(api, credprompt.Options{
			Exe:      exe,
			Platform: credprompt.DetectPlatformHelper(context.Background()),
		})
		if err != nil {
			log.Printf("credential prompts off: %v", err)
		}
		// In-app updates replace this executable (its bundle on macOS).
		if version, err := productVersion(); err != nil {
			log.Printf("updates off: %v", err)
		} else {
			app.WithUpdates(api, app.UpdateOptions{
				Version: version,
				Source:  update.NewChecker(os.Getenv),
				Exe:     exe,
				GOOS:    goruntime.GOOS,
				Dev:     update.DevBuild,
			})
		}
	}

	// macOS gets a native menu bar with Settings… (⌘,). Linux gets none: a
	// Wails menu there is a GTK bar inside the window, and the frontend
	// handles Ctrl+, itself.
	var appMenu *menu.Menu
	if goruntime.GOOS == "darwin" {
		appMenu = api.Menu()
	}

	err = wails.Run(&options.App{
		Menu:             appMenu,
		Title:            "CommitTree",
		Width:            1440,
		Height:           900,
		MinWidth:         960,
		MinHeight:        600,
		AssetServer:      &assetserver.Options{Assets: assets},
		BackgroundColour: &options.RGBA{R: 249, G: 248, B: 246, A: 255},
		OnStartup:        api.Startup,
		OnShutdown:       api.Shutdown,
		Bind:             []interface{}{api},
		Mac: &mac.Options{
			TitleBar: mac.TitleBarHiddenInset(),
		},
	})
	if err != nil {
		log.Fatal(err)
	}
}
