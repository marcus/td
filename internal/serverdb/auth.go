package serverdb

import "fmt"

// Role constants
const (
	RoleOwner  = "owner"
	RoleWriter = "writer"
	RoleReader = "reader"
)

// roleLevel returns the numeric level for a role (higher = more permissions).
func roleLevel(role string) int {
	switch role {
	case RoleOwner:
		return 3
	case RoleWriter:
		return 2
	case RoleReader:
		return 1
	default:
		return 0
	}
}

// Authorize checks that the user has at least the required role in the project.
func (db *ServerDB) Authorize(projectID, userID, requiredRole string) error {
	m, err := db.GetMembership(projectID, userID)
	if err != nil {
		return fmt.Errorf("check membership: %w", err)
	}
	if m == nil {
		return fmt.Errorf("not a member of project %s", projectID)
	}

	if roleLevel(m.Role) < roleLevel(requiredRole) {
		return fmt.Errorf("insufficient permissions: have %s, need %s", m.Role, requiredRole)
	}
	return nil
}

// CanPushEvents checks if the user can push events (requires writer role).
func (db *ServerDB) CanPushEvents(projectID, userID string) error {
	return db.Authorize(projectID, userID, RoleWriter)
}

// CanPullEvents checks if the user can pull events (requires reader role).
func (db *ServerDB) CanPullEvents(projectID, userID string) error {
	return db.Authorize(projectID, userID, RoleReader)
}

// CanViewProject checks if the user can view the project (requires reader role).
func (db *ServerDB) CanViewProject(projectID, userID string) error {
	return db.Authorize(projectID, userID, RoleReader)
}

// CanManageMembers checks if the user can manage members (requires owner role).
func (db *ServerDB) CanManageMembers(projectID, userID string) error {
	return db.Authorize(projectID, userID, RoleOwner)
}

// CanDeleteProject checks if the user can delete the project (requires owner role).
func (db *ServerDB) CanDeleteProject(projectID, userID string) error {
	return db.Authorize(projectID, userID, RoleOwner)
}
