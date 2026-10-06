package main

import (
	"bufio"
	"fmt"
	"math"
	"os"
	"strings"
	"time"
)

var (
	colorOn = true
	stdin   = bufio.NewReader(os.Stdin)
)

const (
	cReset  = "\x1b[0m"
	cGreen  = "\x1b[32m"
	cYellow = "\x1b[33m"
	cRed    = "\x1b[31m"
	cCyan   = "\x1b[36m"
	cGray   = "\x1b[90m"
	cBold   = "\x1b[1m"
)

func paint(c, s string) string {
	if !colorOn {
		return s
	}
	return c + s + cReset
}

func ok(msg string)   { fmt.Println(paint(cGreen, "✓ "+msg)) }
func warn(msg string) { fmt.Println(paint(cYellow, "! "+msg)) }
func fail(msg string) { fmt.Println(paint(cRed, "✗ "+msg)) }
func dim(msg string)  { fmt.Println(paint(cGray, msg)) }

func title(text string) {
	fmt.Println()
	fmt.Println(paint(cCyan, text))
	fmt.Println(paint(cCyan, strings.Repeat("-", len(text))))
	fmt.Println()
}

// readLine reads one line from stdin (works with a terminal or piped input).
func readLine(prompt string) string {
	fmt.Print(prompt)
	line, err := stdin.ReadString('\n')
	if err != nil && line == "" {
		fmt.Println()
		return ""
	}
	line = strings.TrimPrefix(line, "\xEF\xBB\xBF") // UTF-8 BOM that some shells prepend to piped input
	return strings.TrimRight(line, "\r\n")
}

func yesNo(prompt string, def bool) bool {
	suffix := "[y/N]"
	if def {
		suffix = "[Y/n]"
	}
	for {
		a := strings.ToLower(strings.TrimSpace(readLine(prompt + " " + suffix + ": ")))
		switch a {
		case "":
			return def
		case "y", "yes":
			return true
		case "n", "no":
			return false
		}
	}
}

// formatLeft turns a duration into "12 days", "1 day" or "5 hours".
func formatLeft(d time.Duration) string {
	if d < 0 {
		d = -d
	}
	if days := int(d.Hours() / 24); days >= 1 {
		if days == 1 {
			return "1 day"
		}
		return fmt.Sprintf("%d days", days)
	}
	hours := int(math.Ceil(d.Hours()))
	if hours <= 1 {
		return "1 hour"
	}
	return fmt.Sprintf("%d hours", hours)
}

func formatWhen(t time.Time) string {
	if t.IsZero() {
		return "unknown"
	}
	span := time.Until(t)
	abs := math.Abs(span.Hours())
	var unit string
	if abs >= 24 {
		unit = fmt.Sprintf("%.1f days", abs/24)
	} else {
		unit = fmt.Sprintf("%.0f hours", abs)
	}
	stamp := t.Format("2006-01-02 15:04")
	if span < 0 {
		return fmt.Sprintf("expired %s ago (%s)", unit, stamp)
	}
	return fmt.Sprintf("valid for %s (until %s)", unit, stamp)
}
