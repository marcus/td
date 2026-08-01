package cmd

import (
	"fmt"
	"strings"
	"unicode"

	"github.com/marcus/td/internal/models"
	"github.com/marcus/td/internal/output"
	"github.com/marcus/td/internal/workflow"
	"github.com/spf13/cobra"
)

// capitalizeFirst upper-cases the first rune, so the JSON mode names (lowercase
// identifiers) can also drive the human listing's title-cased labels.
func capitalizeFirst(s string) string {
	if s == "" {
		return s
	}
	r := []rune(s)
	r[0] = unicode.ToUpper(r[0])
	return string(r)
}

var workflowCmd = &cobra.Command{
	Use:   "workflow",
	Short: "Show issue status workflow",
	Long: `Displays the issue status workflow state machine.

Shows all valid status transitions and any guards applied.`,
	GroupID: "system",
	RunE: func(cmd *cobra.Command, args []string) error {
		showMermaid, _ := cmd.Flags().GetBool("mermaid")
		showDot, _ := cmd.Flags().GetBool("dot")

		if jsonMode(cmd) {
			// --json combined with a diagram flag still honours the diagram:
			// the text is carried as a string field rather than discarded, so
			// an agent can request either representation non-interactively.
			switch {
			case showMermaid:
				return output.JSON(map[string]interface{}{
					"format":  "mermaid",
					"diagram": renderMermaidDiagram(),
				})
			case showDot:
				return output.JSON(map[string]interface{}{
					"format":  "dot",
					"diagram": renderDotDiagram(),
				})
			}
			return output.JSON(workflowJSON())
		}

		if showMermaid {
			return printMermaidDiagram()
		}
		if showDot {
			return printDotDiagram()
		}
		return printWorkflow()
	},
}

// workflowGuardDoc describes a guard for both the human listing and --json, so
// the two surfaces cannot drift apart.
var workflowGuardDocs = []struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}{
	{"BlockedGuard", "Requires --force to start blocked issues"},
	{"DifferentReviewerGuard", "Prevents self-approval (except minor tasks)"},
}

// workflowModeDocs lists the guard-application modes.
var workflowModeDocs = []struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}{
	{"liberal", "Guards disabled (default)"},
	{"advisory", "Guards warn but allow"},
	{"strict", "Guards block transitions"},
}

// workflowJSON builds the machine-readable state machine: the same statuses,
// transitions, guards, and modes printWorkflow renders, in a shape an agent can
// walk without parsing the section headers.
func workflowJSON() map[string]interface{} {
	sm := workflow.DefaultMachine()

	statuses := make([]string, 0, len(workflow.AllStatuses()))
	transitions := make([]map[string]interface{}, 0)
	for _, from := range workflow.AllStatuses() {
		statuses = append(statuses, string(from))
		for _, to := range sm.GetAllowedTransitions(from) {
			guards := make([]string, 0)
			if t := sm.GetTransition(from, to); t != nil {
				for _, g := range t.Guards {
					guards = append(guards, g.Name())
				}
			}
			transitions = append(transitions, map[string]interface{}{
				"from":   string(from),
				"to":     string(to),
				"name":   workflow.TransitionName(from, to),
				"guards": guards,
			})
		}
	}

	return map[string]interface{}{
		"statuses":    statuses,
		"transitions": transitions,
		"modes":       workflowModeDocs,
		"guards":      workflowGuardDocs,
	}
}

func printWorkflow() error {
	sm := workflow.DefaultMachine()

	fmt.Println("ISSUE STATUS WORKFLOW")
	fmt.Println("=====================")
	fmt.Println()

	// Show statuses
	fmt.Println("STATUSES:")
	for _, s := range workflow.AllStatuses() {
		fmt.Printf("  • %s\n", s)
	}
	fmt.Println()

	// Show transitions by source
	fmt.Println("TRANSITIONS:")
	for _, from := range workflow.AllStatuses() {
		allowed := sm.GetAllowedTransitions(from)
		if len(allowed) > 0 {
			fmt.Printf("  %s →\n", from)
			for _, to := range allowed {
				name := workflow.TransitionName(from, to)
				t := sm.GetTransition(from, to)
				guardStr := ""
				if t != nil && len(t.Guards) > 0 {
					var guardNames []string
					for _, g := range t.Guards {
						guardNames = append(guardNames, g.Name())
					}
					guardStr = fmt.Sprintf(" [%s]", strings.Join(guardNames, ", "))
				}
				fmt.Printf("    %s (%s)%s\n", to, name, guardStr)
			}
		}
	}
	fmt.Println()

	// Show workflow modes
	fmt.Println("MODES:")
	for _, m := range workflowModeDocs {
		fmt.Printf("  • %-8s - %s\n", capitalizeFirst(m.Name), m.Description)
	}
	fmt.Println()

	// Show guards
	fmt.Println("GUARDS (applied in Advisory/Strict modes):")
	for _, g := range workflowGuardDocs {
		fmt.Printf("  • %-22s - %s\n", g.Name, g.Description)
	}
	fmt.Println()

	return nil
}

// renderMermaidDiagram returns the Mermaid source so both the human printer and
// the --json envelope emit byte-identical diagram text.
func renderMermaidDiagram() string {
	sm := workflow.DefaultMachine()

	var sb strings.Builder
	sb.WriteString("```mermaid\n")
	sb.WriteString("stateDiagram-v2\n")
	for _, from := range workflow.AllStatuses() {
		for _, to := range sm.GetAllowedTransitions(from) {
			name := workflow.TransitionName(from, to)
			fmt.Fprintf(&sb, "    %s --> %s: %s\n", from, to, name)
		}
	}
	sb.WriteString("```\n")
	return sb.String()
}

func printMermaidDiagram() error {
	fmt.Print(renderMermaidDiagram())
	return nil
}

// renderDotDiagram returns the GraphViz DOT source. See renderMermaidDiagram.
func renderDotDiagram() string {
	sm := workflow.DefaultMachine()

	var sb strings.Builder
	sb.WriteString("digraph workflow {\n")
	sb.WriteString("    rankdir=LR;\n")
	sb.WriteString("    node [shape=box];\n")
	sb.WriteString("\n")

	// Node styling
	fmt.Fprintf(&sb, "    %s [style=filled,fillcolor=lightblue];\n", models.StatusOpen)
	fmt.Fprintf(&sb, "    %s [style=filled,fillcolor=lightyellow];\n", models.StatusInProgress)
	fmt.Fprintf(&sb, "    %s [style=filled,fillcolor=lightpink];\n", models.StatusBlocked)
	fmt.Fprintf(&sb, "    %s [style=filled,fillcolor=lightorange];\n", models.StatusInReview)
	fmt.Fprintf(&sb, "    %s [style=filled,fillcolor=lightgreen];\n", models.StatusClosed)
	sb.WriteString("\n")

	// Transitions
	for _, from := range workflow.AllStatuses() {
		for _, to := range sm.GetAllowedTransitions(from) {
			name := workflow.TransitionName(from, to)
			fmt.Fprintf(&sb, "    %s -> %s [label=\"%s\"];\n", from, to, name)
		}
	}

	sb.WriteString("}\n")
	return sb.String()
}

func printDotDiagram() error {
	fmt.Print(renderDotDiagram())
	return nil
}

func init() {
	rootCmd.AddCommand(workflowCmd)

	workflowCmd.Flags().Bool("mermaid", false, "Output Mermaid diagram")
	workflowCmd.Flags().Bool("dot", false, "Output GraphViz DOT diagram")
}
