package main

import (
	"bytes"
	"embed"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"omega/internal/kernel"
)

const defaultAPI = "http://127.0.0.1:3000"

// The project's real port, so the development proxy aims at the right one.
func apiURL() string {
	cfg, err := kernel.LoadConfig("config")
	if err != nil {
		return defaultAPI
	}

	port := cfg.IntOr("app.port", 3000)
	if port == 0 {
		return defaultAPI
	}
	return fmt.Sprintf("http://127.0.0.1:%d", port)
}

//go:embed all:webkit
var webkit embed.FS

type stack struct {
	name       string
	short      string
	cli        string
	components []string

	// Script to run between install and `shadcn add`, when the stack needs
	// generated types before components can land.
	prepare string

	// Where the framework-agnostic files land in this stack's tree.
	shared map[string]string
}

var stacks = []stack{
	{
		name:    "nuxt",
		short:   "Scaffold the Nuxt 4 interface — home, docs, auth, dashboard, users",
		cli:     "shadcn-vue@latest",
		prepare: "postinstall",
		shared: map[string]string{
			"locales/fr.docs.json": filepath.Join("app", "locales", "fr.docs.json"),
			"locales/en.docs.json": filepath.Join("app", "locales", "en.docs.json"),
			"locales/es.docs.json": filepath.Join("app", "locales", "es.docs.json"),
			"api.ts":               filepath.Join("app", "lib", "api.ts"),
			"tokens.css":           filepath.Join("app", "assets", "css", "tokens.css"),
			"api-types.ts":         filepath.Join("app", "lib", "api-types.ts"),
			"docs.ts":              filepath.Join("app", "lib", "docs.ts"),
			"locales/fr.json":      filepath.Join("app", "locales", "fr.json"),
			"locales/en.json":      filepath.Join("app", "locales", "en.json"),
			"locales/es.json":      filepath.Join("app", "locales", "es.json"),
		},
		components: []string{"button", "card", "input", "label", "badge", "table", "tabs", "dialog", "dropdown-menu", "avatar", "separator", "skeleton", "sonner"},
	},
	{
		name:  "next",
		short: "Scaffold the Next.js 15 interface (App Router) — home, docs, auth, dashboard, users",
		cli:   "shadcn@latest",
		shared: map[string]string{
			"locales/fr.docs.json":      filepath.Join("locales", "fr.docs.json"),
			"locales/en.docs.json":      filepath.Join("locales", "en.docs.json"),
			"locales/es.docs.json":      filepath.Join("locales", "es.docs.json"),
			"reactlib/theme-toggle.tsx": filepath.Join("components", "theme-toggle.tsx"),
			"api.ts":                    filepath.Join("lib", "api.ts"),
			"reactlib/i18n.tsx":         filepath.Join("lib", "i18n.tsx"),
			"reactlib/theme.tsx":        filepath.Join("lib", "theme.tsx"),
			"reactlib/query.ts":         filepath.Join("lib", "query.ts"),
			"reactlib/toast.tsx":        filepath.Join("lib", "toast.tsx"),
			"tokens.css":                filepath.Join("app", "tokens.css"),
			"api-types.ts":              filepath.Join("lib", "api-types.ts"),
			"docs.ts":                   filepath.Join("lib", "docs.ts"),
			"locales/fr.json":           filepath.Join("locales", "fr.json"),
			"locales/en.json":           filepath.Join("locales", "en.json"),
			"locales/es.json":           filepath.Join("locales", "es.json"),
		},
		components: []string{"button", "card", "input", "label", "badge", "table", "tabs", "dialog", "dropdown-menu", "avatar", "separator", "skeleton", "sonner"},
	},
	{
		name:  "react",
		short: "Scaffold the React 19 interface (Vite) — home, docs, auth, dashboard, users",
		cli:   "shadcn@latest",
		shared: map[string]string{
			"locales/fr.docs.json":      filepath.Join("src", "locales", "fr.docs.json"),
			"locales/en.docs.json":      filepath.Join("src", "locales", "en.docs.json"),
			"locales/es.docs.json":      filepath.Join("src", "locales", "es.docs.json"),
			"reactlib/theme-toggle.tsx": filepath.Join("src", "components", "theme-toggle.tsx"),
			"api.ts":                    filepath.Join("src", "lib", "api.ts"),
			"reactlib/i18n.tsx":         filepath.Join("src", "lib", "i18n.tsx"),
			"reactlib/theme.tsx":        filepath.Join("src", "lib", "theme.tsx"),
			"reactlib/query.ts":         filepath.Join("src", "lib", "query.ts"),
			"reactlib/toast.tsx":        filepath.Join("src", "lib", "toast.tsx"),
			"tokens.css":                filepath.Join("src", "tokens.css"),
			"api-types.ts":              filepath.Join("src", "lib", "api-types.ts"),
			"docs.ts":                   filepath.Join("src", "lib", "docs.ts"),
			"locales/fr.json":           filepath.Join("src", "locales", "fr.json"),
			"locales/en.json":           filepath.Join("src", "locales", "en.json"),
			"locales/es.json":           filepath.Join("src", "locales", "es.json"),
		},
		components: []string{"button", "card", "input", "label", "badge", "table", "tabs", "dialog", "dropdown-menu", "avatar", "separator", "skeleton", "sonner"},
	},
}

func webCommands() []*cobra.Command {
	commands := make([]*cobra.Command, 0, len(stacks))
	for _, target := range stacks {
		commands = append(commands, webCommand(target))
	}
	return commands
}

func webCommand(target stack) *cobra.Command {
	var force, install bool
	var dir, pm string

	cmd := &cobra.Command{
		Use:   "web:" + target.name,
		Short: target.short,
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if _, _, err := kernel.EnterRoot(); err != nil {
				return err
			}

			// Refuse before writing anything: a half-written interface is
			// worse than none.
			chosen, err := pickManager(pm, dir)
			if err != nil && (install || pm != "") {
				return err
			}
			if err != nil {
				chosen = managers[0]
			}

			if _, err := os.Stat(dir); err == nil && !force {
				return fmt.Errorf("%s already exists (use --force to replace it)", dir)
			}
			if force {
				if err := os.RemoveAll(dir); err != nil {
					return err
				}
			}

			written, err := unpack(target, dir, apiURL())
			if err != nil {
				return err
			}
			for _, path := range written {
				fmt.Println("created", path)
			}

			if install {
				return provision(target, dir, chosen)
			}
			printWebNext(target, dir, chosen)
			return nil
		},
	}

	cmd.Flags().BoolVar(&force, "force", false, "replace an existing interface")
	cmd.Flags().BoolVar(&install, "install", false, "run bun install and add the shadcn components")
	cmd.Flags().StringVar(&dir, "dir", "web", "where to write the interface")
	cmd.Flags().StringVar(&pm, "pm", "", "package manager: "+strings.Join(managerNames(), ", ")+" (detected by default)")
	return cmd
}

func unpack(target stack, dir, api string) ([]string, error) {
	written := []string{}

	write := func(raw []byte, relative string) error {
		// The template targets 3000; this project may listen elsewhere.
		if api != defaultAPI {
			raw = bytes.ReplaceAll(raw, []byte(defaultAPI), []byte(api))
		}

		destination := filepath.Join(dir, relative)
		if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(destination, raw, 0o644); err != nil {
			return err
		}
		written = append(written, destination)
		return nil
	}

	root := "webkit/" + target.name
	err := fs.WalkDir(webkit, root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		raw, readErr := webkit.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		return write(raw, strings.TrimPrefix(path, root+"/"))
	})
	if err != nil {
		return nil, err
	}

	for source, destination := range target.shared {
		raw, readErr := webkit.ReadFile("webkit/shared/" + source)
		if readErr != nil {
			return nil, readErr
		}
		if err := write(raw, destination); err != nil {
			return nil, err
		}
	}

	sort.Strings(written)
	return written, nil
}

func provision(target stack, dir string, chosen manager) error {
	required := [][]string{chosen.install}
	if target.prepare != "" {
		required = append(required, append(append([]string{}, chosen.run...), target.prepare))
	}

	for _, step := range required {
		if err := runIn(dir, step); err != nil {
			return err
		}
	}

	// The interface already ships its own components, so a shadcn registry
	// that refuses to install is a missed extra, not a broken scaffold.
	add := append(append([]string{}, chosen.dlx...),
		append([]string{target.cli, "add", "--yes", "--overwrite"}, target.components...)...)

	if err := runIn(dir, add); err != nil {
		fmt.Printf(`
The interface is installed and ready, but the shadcn components did not land:
    %v

Add them later with:
    cd %s && %s
`, err, dir, strings.Join(add, " "))
		return nil
	}

	fmt.Printf("\nReady. cd %s && %s dev\n", dir, strings.Join(chosen.run, " "))
	return nil
}

func runIn(dir string, step []string) error {
	fmt.Println("\n$", strings.Join(step, " "))

	run := exec.Command(step[0], step[1:]...)
	run.Dir = dir
	run.Stdout, run.Stderr, run.Stdin = os.Stdout, os.Stderr, os.Stdin

	if err := run.Run(); err != nil {
		return fmt.Errorf("%s: %w", strings.Join(step, " "), err)
	}
	return nil
}

func printWebNext(target stack, dir string, chosen manager) {
	fmt.Printf(`
Next (using %s — pass --pm to change):

    cd %s
    %s
    %s %s add %s
    %s dev

Or let Omega do it: omega web:%s --force --install
`,
		chosen.name,
		dir,
		strings.Join(chosen.install, " "),
		strings.Join(chosen.dlx, " "), target.cli, strings.Join(target.components, " "),
		strings.Join(chosen.run, " "),
		target.name)
}
