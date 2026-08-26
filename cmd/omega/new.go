package main

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
)

var skipped = map[string]bool{
	".git":         true,
	"tmp":          true,
	"bin":          true,
	"node_modules": true,
	"dist":         true,
	".claude":      true,
}

func newCommand() *cobra.Command {
	var (
		module  string
		withWeb bool
		noGit   bool
	)

	cmd := &cobra.Command{
		Use:   "new <name>",
		Short: "Create a new project from this one",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			target, err := filepath.Abs(name)
			if err != nil {
				return err
			}
			if _, err := os.Stat(target); err == nil {
				return fmt.Errorf("%s already exists", name)
			}

			source, found := projectRoot()
			if !found {
				return fmt.Errorf("run this from an Omega project, or install the binary from one")
			}
			if module == "" {
				module = filepath.Base(target)
			}

			if err := copyTree(source, target, withWeb); err != nil {
				return err
			}
			if err := rename(target, module); err != nil {
				return err
			}
			if err := writeEnv(target); err != nil {
				return err
			}
			if !noGit {
				initGit(target)
			}

			fmt.Printf(`
  %s is ready.

    cd %s
    go run ./cmd/omega migrate:fresh --seed
    go run ./cmd/omega serve
`, name, name)
			if withWeb {
				fmt.Printf("    cd web && bun install && bun run dev\n")
			}
			fmt.Println()
			return nil
		},
	}

	cmd.Flags().StringVar(&module, "module", "", "the Go module path, defaults to the project name")
	cmd.Flags().BoolVar(&withWeb, "web", false, "include the Nuxt interface")
	cmd.Flags().BoolVar(&noGit, "no-git", false, "skip git init")
	return cmd
}

func projectRoot() (string, bool) {
	working, err := os.Getwd()
	if err != nil {
		return "", false
	}
	current := working

	for {
		if _, err := os.Stat(filepath.Join(current, "config", "app.yaml")); err == nil {
			if _, err := os.Stat(filepath.Join(current, "go.mod")); err == nil {
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

func copyTree(source, target string, withWeb bool) error {
	return filepath.WalkDir(source, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		relative, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		if relative == "." {
			return os.MkdirAll(target, 0o755)
		}

		first, _, _ := strings.Cut(relative, string(os.PathSeparator))
		if skipped[first] || skipped[entry.Name()] {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if !withWeb && first == "web" {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(entry.Name(), ".db") ||
			strings.HasSuffix(entry.Name(), ".db-wal") ||
			strings.HasSuffix(entry.Name(), ".db-shm") ||
			entry.Name() == ".env" ||
			strings.HasSuffix(entry.Name(), ".log") {
			return nil
		}

		destination := filepath.Join(target, relative)
		if entry.IsDir() {
			return os.MkdirAll(destination, 0o755)
		}

		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		return os.WriteFile(destination, content, info.Mode())
	})
}

func rename(target, module string) error {
	if module == "omega" {
		return nil
	}

	return filepath.WalkDir(target, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		if !strings.HasSuffix(entry.Name(), ".go") &&
			entry.Name() != "go.mod" &&
			!strings.HasSuffix(entry.Name(), ".stub") {
			return nil
		}

		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}

		text := string(content)
		text = strings.ReplaceAll(text, `"omega/`, `"`+module+`/`)
		text = strings.ReplaceAll(text, `"omega"`, `"`+module+`"`)
		text = strings.ReplaceAll(text, "module omega", "module "+module)

		info, err := entry.Info()
		if err != nil {
			return err
		}
		return os.WriteFile(path, []byte(text), info.Mode())
	})
}

func writeEnv(target string) error {
	example, err := os.ReadFile(filepath.Join(target, ".env.example"))
	if err != nil {
		return nil
	}

	raw := make([]byte, 48)
	if _, err := rand.Read(raw); err != nil {
		return err
	}
	secret := base64.StdEncoding.EncodeToString(raw)

	content := strings.Replace(string(example), "AUTH_SECRET=", "AUTH_SECRET="+secret, 1)
	return os.WriteFile(filepath.Join(target, ".env"), []byte(content), 0o600)
}

func initGit(target string) {
	if _, err := exec.LookPath("git"); err != nil {
		return
	}
	command := exec.Command("git", "init", "--quiet")
	command.Dir = target
	_ = command.Run()
}
