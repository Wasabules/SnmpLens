package main

import (
	"context"
	"embed"
	"log"
	"os"
	"path/filepath"

	"SnmpLens/pkg/service"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

//go:embed all:frontend/dist
var assets embed.FS

//go:embed mibs
var mibs embed.FS

// Tray artwork. Windows needs an .ico; the other desktops want a .png. Both
// are a couple of kilobytes, so embedding both beats a build-tagged file.
//
//go:embed build/appicon.png
var iconPNG []byte

//go:embed build/windows/icon.ico
var iconICO []byte

func main() {
	// The background-mode preferences have to be read before the window
	// exists, so they live in a small JSON file rather than localStorage.
	cfgDir := ""
	if dir, err := os.UserConfigDir(); err == nil {
		cfgDir = filepath.Join(dir, "SnmpLens")
	}
	svcCfg, err := service.Load(cfgDir)
	if err != nil {
		log.Printf("WARNING: could not read %s, using defaults: %v", service.Path(cfgDir), err)
	}

	app := NewApp(mibs)
	app.configDir = cfgDir
	app.serviceCfg = svcCfg
	app.trayIcons = trayIcons{png: iconPNG, ico: iconICO}

	err = wails.Run(&options.App{
		Title:  "SnmpLens",
		Width:  1024,
		Height: 768,
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		BackgroundColour: &options.RGBA{R: 27, G: 38, B: 54, A: 1},
		OnStartup:        app.startup,
		OnShutdown:       app.shutdown,
		// Launching straight to the tray, for a login item. If no tray
		// materialises, startup shows the window anyway rather than leaving a
		// process the user cannot reach.
		StartHidden: svcCfg.StartHidden,
		// HideWindowOnClose is deliberately NOT set. It is fixed here, before
		// we know whether a tray icon actually appeared, and an app that
		// refuses to close with no tray to quit from is unusable. OnBeforeClose
		// makes the same decision later, when the answer is known.
		OnBeforeClose: func(ctx context.Context) bool {
			if app.hideInsteadOfClosing() {
				wruntime.WindowHide(ctx)
				return true // swallow the close
			}
			return false
		},
		SingleInstanceLock: &options.SingleInstanceLock{
			UniqueId: "com.wasabules.snmplens",
			// Relaunching is how you get back to a hidden instance when the
			// desktop has no usable tray.
			OnSecondInstanceLaunch: func(options.SecondInstanceData) {
				app.RevealWindow()
			},
		},
		DragAndDrop: &options.DragAndDrop{
			EnableFileDrop:     true,
			DisableWebViewDrop: false,
		},
		Bind: []interface{}{
			app,
		},
	})

	if err != nil {
		println("Error:", err.Error())
	}
}
