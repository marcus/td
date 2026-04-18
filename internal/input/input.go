// Package input provides helpers for reading flag values from stdin and files.
package input

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/marcus/td/internal/output"
)

// ErrStdinAlreadyUsed is returned when multiple flags try to consume stdin.
var ErrStdinAlreadyUsed = errors.New("stdin already used")

// ExpandFlagValues expands flag values that use - (stdin) or @file syntax.
// Returns the expanded values and whether stdin was consumed.
func ExpandFlagValues(values []string, stdinUsed bool) ([]string, bool) {
	var result []string
	for _, v := range values {
		if v == "-" {
			if stdinUsed {
				output.Warning("stdin already used, ignoring additional - flag")
				continue
			}
			stdinUsed = true
			lines := ReadLinesFromReader(os.Stdin)
			result = append(result, lines...)
		} else if strings.HasPrefix(v, "@") {
			path := strings.TrimPrefix(v, "@")
			file, err := os.Open(path)
			if err != nil {
				output.Warning("failed to read %s: %v", path, err)
				continue
			}
			lines := ReadLinesFromReader(file)
			file.Close()
			result = append(result, lines...)
		} else {
			result = append(result, v)
		}
	}
	return result, stdinUsed
}

// ReadLinesFromReader reads non-empty lines from a reader.
func ReadLinesFromReader(r io.Reader) []string {
	var lines []string
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line != "" {
			lines = append(lines, line)
		}
	}
	return lines
}

// ReadText preserves the full contents of a file or stdin as a single block.
// The special source "-" reads from stdin exactly once.
func ReadText(source string, stdin io.Reader, stdinUsed bool) (string, bool, error) {
	if source == "" {
		return "", stdinUsed, fmt.Errorf("input source is required")
	}

	if source == "-" {
		if stdinUsed {
			return "", stdinUsed, ErrStdinAlreadyUsed
		}
		data, err := io.ReadAll(stdin)
		if err != nil {
			return "", true, fmt.Errorf("read stdin: %w", err)
		}
		return string(data), true, nil
	}

	data, err := os.ReadFile(source)
	if err != nil {
		return "", stdinUsed, fmt.Errorf("read %s: %w", source, err)
	}
	return string(data), stdinUsed, nil
}
