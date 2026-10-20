package models

import (
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
)

// Role represents a user's access level within the system.
type Role string

const (
	RoleAdmin           Role = "admin"
	RoleSeniorArchitect Role = "senior_architect"
	RoleJuniorArchitect Role = "junior_architect"
	RoleIntern          Role = "intern"
)

// User represents a system user with authentication and role information.
type User struct {
	ID           bson.ObjectID `bson:"_id,omitempty" json:"id"`
	UserID       string             `bson:"user_id"       json:"user_id"`
	PasswordHash string             `bson:"password_hash" json:"-"`
	Name         string             `bson:"name"          json:"name"`
	Email        string             `bson:"email"         json:"email"`
	Role         Role               `bson:"role"          json:"role"`
	TokenVersion int                `bson:"token_version" json:"-"`
	CreatedAt    time.Time          `bson:"created_at"    json:"created_at"`
}

// RoleDisplayName returns a human-friendly label for the role.
func (r Role) RoleDisplayName() string {
	switch r {
	case RoleAdmin:
		return "Admin"
	case RoleSeniorArchitect:
		return "Senior Architect"
	case RoleJuniorArchitect:
		return "Junior Architect"
	case RoleIntern:
		return "Intern"
	default:
		return string(r)
	}
}

// CanManageUsers returns true if the role has user management privileges.
func (r Role) CanManageUsers() bool {
	return r == RoleAdmin
}

// CanCreateProject returns true if the role can create new projects.
func (r Role) CanCreateProject() bool {
	return r == RoleAdmin || r == RoleSeniorArchitect
}

// CanAssignUsers returns true if the role can assign users to projects.
func (r Role) CanAssignUsers() bool {
	return r == RoleAdmin || r == RoleSeniorArchitect
}

// CanMoveToDone returns true if the role can transition a task to Done status.
func (r Role) CanMoveToDone() bool {
	return r == RoleAdmin || r == RoleSeniorArchitect
}

// CanMoveToTodo returns true if the role can move a task back to To-Do.
func (r Role) CanMoveToTodo() bool {
	return r == RoleAdmin || r == RoleSeniorArchitect
}
