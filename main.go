package main

import (
	"embed"
	"log"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/mac"

	"git-ui/internal/app"
	"git-ui/internal/repos"
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
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

	err = wails.Run(&options.App{
		Title:            "git-ui",
		Width:            1440,
		Height:           900,
		MinWidth:         960,
		MinHeight:        600,
		AssetServer:      &assetserver.Options{Assets: assets},
		BackgroundColour: &options.RGBA{R: 249, G: 248, B: 246, A: 255},
		OnStartup:        api.Startup,
		Bind:             []interface{}{api},
		Mac: &mac.Options{
			TitleBar: mac.TitleBarHiddenInset(),
		},
	})
	if err != nil {
		log.Fatal(err)
	}
}
