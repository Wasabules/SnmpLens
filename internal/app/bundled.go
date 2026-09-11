package app

import "io/fs"

// bundledDir and bundledFile read what main embedded (App.mibs, App.presets).
//
// An App built without them — every test builds one that way — reads as EMPTY,
// as it did when these were embed.FS values: a zero embed.FS answers "does not
// exist", while a nil fs.FS makes fs.ReadFile panic.
func bundledDir(fsys fs.FS, name string) ([]fs.DirEntry, error) {
	if fsys == nil {
		return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrNotExist}
	}
	return fs.ReadDir(fsys, name)
}

func bundledFile(fsys fs.FS, name string) ([]byte, error) {
	if fsys == nil {
		return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrNotExist}
	}
	return fs.ReadFile(fsys, name)
}
