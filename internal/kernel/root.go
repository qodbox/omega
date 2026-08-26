package kernel

import (
	"errors"
	"os"
	"path/filepath"
)

var markers = []string{"config/app.yaml", "go.mod"}

func FindRoot(start string) (string, bool) {
	current, err := filepath.Abs(start)
	if err != nil {
		return "", false
	}

	for {
		for _, marker := range markers {
			if _, err := os.Stat(filepath.Join(current, marker)); err == nil {
				return current, true
			}
		}

		parent := filepath.Dir(current)
		if parent == current {
			return "", false
		}
		current = parent
	}
}

var ErrNoProject = errors.New(
	"no Omega project here: run this from a directory that contains config/app.yaml, or one of its subdirectories")

func EnterRoot() (string, bool, error) {
	working, err := os.Getwd()
	if err != nil {
		return "", false, err
	}

	root, found := FindRoot(working)
	if !found {
		return working, false, ErrNoProject
	}
	if root == working {
		return working, false, nil
	}
	if err := os.Chdir(root); err != nil {
		return working, false, err
	}
	return root, true, nil
}
