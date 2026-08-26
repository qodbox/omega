package main

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func staleBinary() (time.Duration, bool) {
	binary, err := os.Executable()
	if err != nil {
		return 0, false
	}

	info, err := os.Stat(binary)
	if err != nil {
		return 0, false
	}

	working, err := os.Getwd()
	if err != nil {
		return 0, false
	}
	if strings.HasPrefix(binary, working) {
		return 0, false
	}

	newest := info.ModTime()
	found := false

	_ = filepath.WalkDir(working, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if entry.IsDir() {
			switch entry.Name() {
			case "tmp", "bin", "web", "node_modules", ".git", "vendor":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(entry.Name(), ".go") {
			return nil
		}
		source, err := entry.Info()
		if err != nil {
			return nil
		}
		if source.ModTime().After(newest) {
			newest = source.ModTime()
			found = true
		}
		return nil
	})

	if !found {
		return 0, false
	}
	return newest.Sub(info.ModTime()), true
}
