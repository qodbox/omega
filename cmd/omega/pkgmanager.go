package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type manager struct {
	name string
	lock string

	install []string
	dlx     []string
	run     []string
}

// Preference order when nothing else decides: the fastest first.
var managers = []manager{
	{
		name: "bun", lock: "bun.lock",
		install: []string{"bun", "install"},
		dlx:     []string{"bunx", "--bun"},
		run:     []string{"bun", "run"},
	},
	{
		name: "pnpm", lock: "pnpm-lock.yaml",
		install: []string{"pnpm", "install"},
		dlx:     []string{"pnpm", "dlx"},
		run:     []string{"pnpm", "run"},
	},
	{
		name: "yarn", lock: "yarn.lock",
		install: []string{"yarn", "install"},
		dlx:     []string{"yarn", "dlx"},
		run:     []string{"yarn", "run"},
	},
	{
		name: "npm", lock: "package-lock.json",
		install: []string{"npm", "install"},
		dlx:     []string{"npx", "--yes"},
		run:     []string{"npm", "run"},
	},
}

func managerNames() []string {
	names := make([]string, 0, len(managers))
	for _, candidate := range managers {
		names = append(names, candidate.name)
	}
	return names
}

func byName(name string) (manager, error) {
	for _, candidate := range managers {
		if candidate.name == name {
			return candidate, nil
		}
	}
	return manager{}, fmt.Errorf("unknown package manager %q (want %s)", name, strings.Join(managerNames(), ", "))
}

func installed(candidate manager) bool {
	_, err := exec.LookPath(candidate.install[0])
	return err == nil
}

// Yarn gained `dlx` in v2. Classic still needs npx, which ships with Node.
func adjust(candidate manager) manager {
	if candidate.name != "yarn" {
		return candidate
	}

	out, err := exec.Command("yarn", "--version").Output()
	if err != nil || strings.HasPrefix(strings.TrimSpace(string(out)), "1.") {
		candidate.dlx = []string{"npx", "--yes"}
	}
	return candidate
}

// A lockfile beside package.json is the project's own answer; otherwise take
// the first manager actually installed.
func pickManager(chosen, dir string) (manager, error) {
	if chosen != "" {
		candidate, err := byName(chosen)
		if err != nil {
			return manager{}, err
		}
		if !installed(candidate) {
			return manager{}, fmt.Errorf("%s is not installed", candidate.name)
		}
		return adjust(candidate), nil
	}

	for _, candidate := range managers {
		if _, err := os.Stat(filepath.Join(dir, candidate.lock)); err == nil && installed(candidate) {
			return adjust(candidate), nil
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "bun.lockb")); err == nil && installed(managers[0]) {
		return adjust(managers[0]), nil
	}

	for _, candidate := range managers {
		if installed(candidate) {
			return adjust(candidate), nil
		}
	}

	return manager{}, fmt.Errorf("no package manager found — install one of %s", strings.Join(managerNames(), ", "))
}
