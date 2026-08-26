package kernel

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/mattn/go-isatty"
)

const (
	ansiReset  = "\x1b[0m"
	ansiDim    = "\x1b[2m"
	ansiBold   = "\x1b[1m"
	ansiRed    = "\x1b[31m"
	ansiGreen  = "\x1b[32m"
	ansiYellow = "\x1b[33m"
	ansiBlue   = "\x1b[34m"
	ansiCyan   = "\x1b[36m"
)

var colourise = os.Getenv("NO_COLOR") == "" && isatty.IsTerminal(os.Stdout.Fd())

func paint(colour, text string) string {
	if !colourise {
		return text
	}
	return colour + text + ansiReset
}

func statusColour(status int) string {
	switch {
	case status >= 500:
		return ansiRed
	case status >= 400:
		return ansiYellow
	case status >= 300:
		return ansiCyan
	default:
		return ansiGreen
	}
}

func humanDuration(d time.Duration) string {
	switch {
	case d < time.Microsecond:
		return fmt.Sprintf("%dns", d.Nanoseconds())
	case d < time.Millisecond:
		return fmt.Sprintf("%.0fµs", float64(d.Microseconds()))
	case d < time.Second:
		return fmt.Sprintf("%.1fms", float64(d.Microseconds())/1000)
	default:
		return fmt.Sprintf("%.2fs", d.Seconds())
	}
}

func requestLine(method, path string, status int, elapsed time.Duration) string {
	verb := method
	if len(path) > 56 {
		path = path[:55] + "…"
	}

	return fmt.Sprintf("%s %s %s %s",
		paint(statusColour(status), fmt.Sprintf("%3d", status)),
		paint(ansiDim, fmt.Sprintf("%-7s", verb)),
		fmt.Sprintf("%-56s", path),
		paint(ansiDim, humanDuration(elapsed)))
}

func banner(name, version, env, driver, addr string) string {
	url := "http://" + strings.Replace(addr, "0.0.0.0", "127.0.0.1", 1)

	var b strings.Builder
	b.WriteString("\n  ")
	b.WriteString(paint(ansiBold+ansiBlue, "Ω "+name))
	b.WriteString(paint(ansiDim, " "+version))
	b.WriteString("\n  ")
	b.WriteString(paint(ansiDim, env+"  ·  "+driver))
	b.WriteString("\n\n  ")
	b.WriteString(paint(ansiCyan, url))
	b.WriteString("\n  ")
	b.WriteString(paint(ansiDim, "ctrl-c to stop"))
	b.WriteString("\n\n")
	return b.String()
}

func writeConsole(text string) {
	_, _ = fmt.Fprint(os.Stdout, text)
}

func consoleLevel(value any) string {
	switch value {
	case "info", "debug":
		return " "
	case "warn":
		return paint(ansiYellow, "warn")
	case "error", "fatal", "panic":
		return paint(ansiRed, "error")
	default:
		return fmt.Sprint(value)
	}
}
