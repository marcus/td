package cmd

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/marcus/td/internal/input"
	"github.com/spf13/cobra"
)

func resolveRichTextField(cmd *cobra.Command, inlineFlags []string, fileFlag string, stdinUsed bool) (string, bool, bool, error) {
	inlineProvided := false
	inlineValue := ""
	for _, name := range inlineFlags {
		if cmd.Flags().Changed(name) {
			inlineProvided = true
		}
		value, err := cmd.Flags().GetString(name)
		if err != nil {
			return "", false, stdinUsed, err
		}
		if inlineValue == "" && value != "" {
			inlineValue = value
		}
	}

	if !cmd.Flags().Changed(fileFlag) {
		if inlineValue == "" {
			return "", false, stdinUsed, nil
		}
		return inlineValue, true, stdinUsed, nil
	}

	if inlineProvided {
		return "", false, stdinUsed, fmt.Errorf(
			"cannot use %s with %s; choose one source",
			formatLongFlag(fileFlag),
			formatLongFlags(inlineFlags),
		)
	}

	source, err := cmd.Flags().GetString(fileFlag)
	if err != nil {
		return "", false, stdinUsed, err
	}
	if source == "" {
		return "", false, stdinUsed, fmt.Errorf("%s requires a path or - for stdin", formatLongFlag(fileFlag))
	}

	value, stdinUsed, err := input.ReadText(source, os.Stdin, stdinUsed)
	if err != nil {
		if errors.Is(err, input.ErrStdinAlreadyUsed) {
			return "", false, stdinUsed, fmt.Errorf("%s cannot read from stdin more than once in a single command", formatLongFlag(fileFlag))
		}
		if source == "-" {
			return "", false, stdinUsed, fmt.Errorf("failed to read %s from stdin: %w", formatLongFlag(fileFlag), err)
		}
		return "", false, stdinUsed, fmt.Errorf("failed to read %s from %q: %w", formatLongFlag(fileFlag), source, err)
	}

	return value, true, stdinUsed, nil
}

func formatLongFlag(name string) string {
	return "--" + name
}

func formatLongFlags(names []string) string {
	formatted := make([]string, 0, len(names))
	for _, name := range names {
		formatted = append(formatted, formatLongFlag(name))
	}
	return strings.Join(formatted, ", ")
}
