package app

import (
	"io/fs"
	"os"
)

// The tests run in internal/app, and what several of them read — the bundled
// MIBs and presets, the simulator's test data, the renderer's sources — sits
// at the root of the repository.
const repoRoot = "../.."

// mibs and presets stand in for what main embeds: the same directories, read
// from the tree rather than from the binary. Neither holds a dotfile or a file
// starting with an underscore, the two things //go:embed would leave out.
var (
	mibs    fs.FS = os.DirFS(repoRoot)
	presets fs.FS = os.DirFS(repoRoot)
)
