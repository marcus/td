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

const (
	formButtonFocusForm   = -1
	formButtonFocusSubmit = 0
	formButtonFocusCancel = 1
)

const (
	formKeyTitle        = "title"
	formKeyType         = "type"
	formKeyPriority     = "priority"
	formKeyDescription  = "description"
	formKeyLabels       = "labels"
	formKeyParent       = "parent"
	formKeyPoints       = "points"
	formKeyAcceptance   = "acceptance"
	formKeyMinor        = "minor"
	formKeyDependencies = "dependencies"
	formKeyStatus       = "status"
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
	Status      string // Only used in edit mode

	// Extended fields (toggled with Tab)
	ShowExtended bool
	Parent       string // Parent epic ID
	Points       string // String for select options
	Acceptance   string
	Minor        bool
	Dependencies string // Comma-separated issue IDs

	// Button focus: -1 = form fields focused, 0 = submit, 1 = cancel
	ButtonFocus int
	ButtonHover int // 0 = none, 1 = submit, 2 = cancel

	// Width for form fields (set from modal dimensions, reapplied on rebuild)
	Width int

	// Autofill state for Parent Epic and Dependencies fields
	Autofill      *AutofillState // Active dropdown state (nil when not showing)
	AutofillEpics []AutofillItem // Cached epics (for parent field, type=epic only)
	AutofillAll   []AutofillItem // Cached all open issues (for dependencies field)
}

// NewFormState creates a new form state for creating an issue
func NewFormState(mode FormMode, parentID string) *FormState {
	state := &FormState{
		Mode:        mode,
		ParentID:    parentID,
		Parent:      parentID,
		Type:        string(models.TypeTask),
		Priority:    string(models.PriorityP2),
		Points:      "0",
		ButtonFocus: formButtonFocusForm,
		ButtonHover: 0,
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
		Status:      string(issue.Status),
		Parent:      issue.ParentID,
		Points:      pointsToString(issue.Points),
		Acceptance:  issue.Acceptance,
		Minor:       issue.Minor,
		ButtonFocus: formButtonFocusForm,
		ButtonHover: 0,
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

	// Status options (edit mode only)
	statusOptions := []huh.Option[string]{
		huh.NewOption("Open", string(models.StatusOpen)),
		huh.NewOption("In Progress", string(models.StatusInProgress)),
		huh.NewOption("Blocked", string(models.StatusBlocked)),
		huh.NewOption("In Review", string(models.StatusInReview)),
		huh.NewOption("Closed", string(models.StatusClosed)),
	}

	titleStr := "New Issue"
	if fs.Mode == FormModeEdit {
		titleStr = "Edit Issue: " + fs.IssueID
	}

	// Standard fields group
	standardGroup := huh.NewGroup(
		huh.NewInput().
			Key(formKeyTitle).
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
			Key(formKeyType).
			Title("Type").
			Options(typeOptions...).
			Value(&fs.Type),
		huh.NewSelect[string]().
			Key(formKeyPriority).
			Title("Priority").
			Options(priorityOptions...).
			Value(&fs.Priority),
		huh.NewText().
			Key(formKeyDescription).
			Title("Description").
			Value(&fs.Description).
			Placeholder("Optional description...").
			Lines(3),
		huh.NewInput().
			Key(formKeyLabels).
			Title("Labels").
			Value(&fs.Labels).
			Placeholder("label1, label2, ..."),
	).Title(titleStr)

	// Extended fields — split across two pages so each fits in the modal.
	// Page 2: detail fields
	detailsGroup := huh.NewGroup(
		huh.NewInput().
			Key(formKeyParent).
			Title("Parent Epic").
			Value(&fs.Parent).
			Placeholder("td-xxxxxxxx"),
		huh.NewSelect[string]().
			Key(formKeyPoints).
			Title("Story Points").
			Options(pointsOptions...).
			Value(&fs.Points),
		huh.NewText().
			Key(formKeyAcceptance).
			Title("Acceptance Criteria").
			Value(&fs.Acceptance).
			Placeholder("- [ ] Criterion 1\n- [ ] Criterion 2").
			Lines(3),
	).Title("Details")

	// Page 3: workflow fields
	workflowFields := []huh.Field{
		huh.NewConfirm().
			Key(formKeyMinor).
			Title("Minor Issue").
			Description("Minor issues can be self-reviewed").
			Value(&fs.Minor),
		huh.NewInput().
			Key(formKeyDependencies).
			Title("Dependencies").
			Value(&fs.Dependencies).
			Placeholder("td-xxx, td-yyy"),
	}

	// Add status select at the very bottom for edit mode
	if fs.Mode == FormModeEdit {
		workflowFields = append(workflowFields,
			huh.NewSelect[string]().
				Key(formKeyStatus).
				Title("Status").
				Options(statusOptions...).
				Value(&fs.Status),
		)
	}

	workflowGroup := huh.NewGroup(workflowFields...).Title("Workflow")

	// Build the form
	if fs.ShowExtended {
		fs.Form = huh.NewForm(standardGroup, detailsGroup, workflowGroup)
	} else {
		fs.Form = huh.NewForm(standardGroup)
	}

	// Configure form appearance
	fs.Form.WithTheme(huh.ThemeDracula())

	// Apply width if set (ensures text wrapping works after form rebuild)
	if fs.Width > 0 {
		fs.Form.WithWidth(fs.Width)
	}
}

// ToggleExtended toggles the extended fields visibility and rebuilds the form
func (fs *FormState) ToggleExtended() {
	fs.ShowExtended = !fs.ShowExtended
	fs.buildForm()
}

func (fs *FormState) focusedFieldKey() string {
	if fs == nil || fs.Form == nil {
		return ""
	}
	field := fs.Form.GetFocusedField()
	if field == nil {
		return ""
	}
	return field.GetKey()
}

func (fs *FormState) firstFieldKey() string {
	return formKeyTitle
}

func (fs *FormState) lastFieldKey() string {
	if fs.ShowExtended {
		if fs.Mode == FormModeEdit {
			return formKeyStatus
		}
		return formKeyDependencies
	}
	return formKeyLabels
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
		Status:      models.Status(fs.Status),
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
