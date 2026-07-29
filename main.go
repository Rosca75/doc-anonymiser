// main.go — Wails v2 bootstrap ONLY. No business logic lives here
// (CLAUDE.md §3): this file embeds the frontend assets, creates the window
// and binds the App struct. Everything the app actually *does* is in
// engine/* (logic) and app.go (thin bridge adapters).
package main

import (
	"embed"
	"log"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"

	"doc-anonymiser/backend"
)

// The whole vanilla-JS frontend (the frontend/ folder) is compiled into the
// binary via go:embed, no npm, no bundler, no files on disk at runtime, and
// (crucially for the local-only guarantee) no possibility of loading remote
// assets.
//
// This directive, wails.json's assetdir, and the frontend/ folder name are a
// coupled triple: go:embed cannot reference a parent directory, so this file
// (and wails.json) must stay at the module root, at or above frontend/.
//
//go:embed all:frontend
var assets embed.FS

func main() {
	// The App struct (backend/app.go) holds the methods exposed to
	// JavaScript. It now lives in package backend, so Wails exposes its
	// methods to the frontend as window.go.backend.App.<Method> (was
	// window.go.main.App before the backend/ split); frontend/api.js is the
	// only JS file allowed to call them (CLAUDE.md §4).
	app := backend.NewApp()

	// Run the Wails v2 application. This blocks until the window closes.
	err := wails.Run(&options.App{
		Title:  "doc-anonymiser",
		Width:  1200,
		Height: 800,
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		// Native file drag-and-drop: the OS hands us absolute paths and
		// app.go routes them through the SAME validation as the dialog
		// (CLAUDE.md §5 — rejection must also happen on drop).
		DragAndDrop: &options.DragAndDrop{
			EnableFileDrop:     true,
			DisableWebViewDrop: true, // stop the WebView from navigating to dropped files
		},
		// OnStartup hands the runtime context to App so bound methods
		// can later use Wails runtime features (dialogs, events).
		OnStartup: app.Startup,
		// Bind exposes App's exported methods to the frontend as
		// window.go.backend.App.<Method> — frontend/api.js is the only JS
		// file allowed to call them (CLAUDE.md §4).
		Bind: []interface{}{
			app,
		},
	})
	if err != nil {
		// If the window cannot even be created there is no UI to show
		// the error in, so log it for the terminal / event log.
		log.Fatalf("doc-anonymiser failed to start: %v, on Windows check that WebView2 is installed (it ships with Windows 10/11); on Linux install libgtk-3 and libwebkit2gtk-4.1", err)
	}
}
