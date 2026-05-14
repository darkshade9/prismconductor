package main

import (
	"embed"
	"os"
	"runtime"
	"strings"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
)

//go:embed all:frontend/dist
var assets embed.FS

// ensureUserToolPath augments PATH with common user-tool locations that macOS
// strips when the app is launched from Finder/Dock/Spotlight. Without this,
// child processes (workers spawned by the session manager) cannot find tools
// like `gh`, `node`, or homebrew-installed binaries that live outside the
// bare system PATH (`/usr/bin:/bin:/usr/sbin:/sbin`). Workers inherit PATH
// from the conductor process via os.Environ(), so fixing it here is enough.
func ensureUserToolPath() {
	if runtime.GOOS != "darwin" {
		return
	}
	current := os.Getenv("PATH")
	parts := strings.Split(current, ":")
	have := make(map[string]bool, len(parts))
	for _, p := range parts {
		have[p] = true
	}
	candidates := []string{
		"/opt/homebrew/bin",
		"/opt/homebrew/sbin",
		"/usr/local/bin",
		"/usr/local/sbin",
	}
	if home, err := os.UserHomeDir(); err == nil {
		candidates = append(candidates, home+"/.local/bin")
	}
	var prepend []string
	for _, c := range candidates {
		if have[c] {
			continue
		}
		if info, err := os.Stat(c); err != nil || !info.IsDir() {
			continue
		}
		prepend = append(prepend, c)
	}
	if len(prepend) == 0 {
		return
	}
	_ = os.Setenv("PATH", strings.Join(prepend, ":")+":"+current)
}

func main() {
	ensureUserToolPath()

	// Create an instance of the app structure
	app := NewApp()

	// Create application with options
	err := wails.Run(&options.App{
		Title:     "prismconductor",
		Width:     1600,
		Height:    980,
		MinWidth:  1280,
		MinHeight: 720,
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		BackgroundColour: &options.RGBA{R: 27, G: 38, B: 54, A: 1},
		OnStartup:        app.startup,
		Bind: []interface{}{
			app,
		},
	})

	if err != nil {
		println("Error:", err.Error())
	}
}
