package monitor

import (
	"errors"
	"strings"

	"github.com/charmbracelet/huh"
	"github.com/marcus/td/internal/models"
)

var errTitleRequired = errors.New("title is required")

// FormMode represents the mode of the form
type FormMode string

const (
	FormModeCreate FormMode = "create"
	FormModeEdit   FormMode = "edit"
)

// FormState holds the state for the issue form modal
type FormState struct {
	Mode     FormMode
	Form     *huh.Form
	IssueID  string // For edit mode - the issue being edited
	ParentID string // For create mode - auto-populated parent epic

	// Bound form values (standard fields)
	Title       string
	Type        string
	Priority    string
	Description string
	Labels      string // Comma-separated

	// Extended fields (toggled with Tab)
	ShowExtended bool
	Parent       string // Parent epic ID
	Points       string // String for select options
	Acceptance   string
	Minor        bool
	Dependencies string // Comma-separated issue IDs
}

// NewFormState creates a new form state for creating an issue
func NewFormState(mode FormMode, parentID string) *FormState {
	state := &FormState{
		Mode:     mode,
		ParentID: parentID,
		Parent:   parentID,
		Type:     string(models.TypeTask),
		Priority: string(models.PriorityP2),
		Points:   "0",
	}
	state.buildForm()
	return state
}

// NewFormStateForEdit creates a form state populated with existing issue data
func NewFormStateForEdit(issue *models.Issue) *FormState {
	state := &FormState{
		Mode:        FormModeEdit,
		IssueID:     issue.ID,
		Title:       issue.Title,
		Type:        string(issue.Type),
		Priority:    string(issue.Priority),
		Description: issue.Description,
		Labels:      strings.Join(issue.Labels, ", "),
		Parent:      issue.ParentID,
		Points:      pointsToString(issue.Points),
		Acceptance:  issue.Acceptance,
		Minor:       issue.Minor,
	}
	state.buildForm()
	return state
}

// buildForm constructs the huh.Form based on current state
func (fs *FormState) buildForm() {
	// Type options
	typeOptions := []huh.Option[string]{
		huh.NewOption("Task", string(models.TypeTask)),
		huh.NewOption("Bug", string(models.TypeBug)),
		huh.NewOption("Feature", string(models.TypeFeature)),
		huh.NewOption("Chore", string(models.TypeChore)),
		huh.NewOption("Epic", string(models.TypeEpic)),
	}

	// Priority options
	priorityOptions := []huh.Option[string]{
		huh.NewOption("P0 - Critical", string(models.PriorityP0)),
		huh.NewOption("P1 - High", string(models.PriorityP1)),
		huh.NewOption("P2 - Medium", string(models.PriorityP2)),
		huh.NewOption("P3 - Low", string(models.PriorityP3)),
		huh.NewOption("P4 - None", string(models.PriorityP4)),
	}

	// Points options
	pointsOptions := []huh.Option[string]{
		huh.NewOption("None", "0"),
		huh.NewOption("1", "1"),
		huh.NewOption("2", "2"),
		huh.NewOption("3", "3"),
		huh.NewOption("5", "5"),
		huh.NewOption("8", "8"),
		huh.NewOption("13", "13"),
		huh.NewOption("21", "21"),
	}

	titleStr := "New Issue"
	if fs.Mode == FormModeEdit {
		titleStr = "Edit Issue: " + fs.IssueID
	}

	// Standard fields group
	standardGroup := huh.NewGroup(
		huh.NewInput().
			Title("Title").
			Value(&fs.Title).
			Placeholder("Issue title...").
			Validate(func(s string) error {
				if strings.TrimSpace(s) == "" {
					return errTitleRequired
				}
				return nil
			}),
		huh.NewSelect[string]().
			Title("Type").
			Options(typeOptions...).
			Value(&fs.Type),
		huh.NewSelect[string]().
			Title("Priority").
			Options(priorityOptions...).
			Value(&fs.Priority),
		huh.NewText().
			Title("Description").
			Value(&fs.Description).
			Placeholder("Optional description...").
			Lines(3),
		huh.NewInput().
			Title("Labels").
			Value(&fs.Labels).
			Placeholder("label1, label2, ..."),
	).Title(titleStr)

	// Extended fields group
	extendedGroup := huh.NewGroup(
		huh.NewInput().
			Title("Parent Epic").
			Value(&fs.Parent).
			Placeholder("td-xxxxxxxx"),
		huh.NewSelect[string]().
			Title("Story Points").
			Options(pointsOptions...).
			Value(&fs.Points),
		huh.NewText().
			Title("Acceptance Criteria").
			Value(&fs.Acceptance).
			Placeholder("- [ ] Criterion 1\n- [ ] Criterion 2").
			Lines(3),
		huh.NewConfirm().
			Title("Minor Issue").
			Description("Minor issues can be self-reviewed").
			Value(&fs.Minor),
		huh.NewInput().
			Title("Dependencies").
			Value(&fs.Dependencies).
			Placeholder("td-xxx, td-yyy"),
	).Title("Extended Fields")

	// Build the form
	if fs.ShowExtended {
		fs.Form = huh.NewForm(standardGroup, extendedGroup)
	} else {
		fs.Form = huh.NewForm(standardGroup)
	}

	// Configure form appearance
	fs.Form.WithTheme(huh.ThemeDracula())
}

// ToggleExtended toggles the extended fields visibility and rebuilds the form
func (fs *FormState) ToggleExtended() {
	fs.ShowExtended = !fs.ShowExtended
	fs.buildForm()
}

// ToIssue converts form values to an Issue model
func (fs *FormState) ToIssue() *models.Issue {
	labels := parseLabels(fs.Labels)
	points := stringToPoints(fs.Points)
	deps := parseLabels(fs.Dependencies) // Same parsing for dependencies

	issue := &models.Issue{
		Title:       strings.TrimSpace(fs.Title),
		Type:        models.Type(fs.Type),
		Priority:    models.Priority(fs.Priority),
		Description: fs.Description,
		Labels:      labels,
		ParentID:    strings.TrimSpace(fs.Parent),
		Points:      points,
		Acceptance:  fs.Acceptance,
		Minor:       fs.Minor,
	}

	// Store dependencies separately - they're handled differently
	_ = deps

	return issue
}

// GetDependencies returns parsed dependency IDs
func (fs *FormState) GetDependencies() []string {
	return parseLabels(fs.Dependencies)
}

// parseLabels parses a comma-separated string into a slice of trimmed strings
func parseLabels(s string) []string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	var result []string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			result = append(result, p)
		}
	}
	return result
}

// pointsToString converts points int to string for form
func pointsToString(p int) string {
	switch p {
	case 0:
		return "0"
	case 1:
		return "1"
	case 2:
		return "2"
	case 3:
		return "3"
	case 5:
		return "5"
	case 8:
		return "8"
	case 13:
		return "13"
	case 21:
		return "21"
	default:
		return "0"
	}
}

// stringToPoints converts form string to points int
func stringToPoints(s string) int {
	switch s {
	case "1":
		return 1
	case "2":
		return 2
	case "3":
		return 3
	case "5":
		return 5
	case "8":
		return 8
	case "13":
		return 13
	case "21":
		return 21
	default:
		return 0
	}
}
