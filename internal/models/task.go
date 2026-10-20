package models

import (
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
)

// TaskStatus represents a Kanban column.
type TaskStatus string

const (
	StatusTodo          TaskStatus = "todo"
	StatusInProgress    TaskStatus = "in_progress"
	StatusNeedsApproval TaskStatus = "needs_approval"
	StatusDone          TaskStatus = "done"
)

// KanbanColumns defines the ordered list of Kanban columns.
var KanbanColumns = []TaskStatus{
	StatusTodo,
	StatusInProgress,
	StatusNeedsApproval,
	StatusDone,
}

// TaskSeverity represents the priority/severity level of a task.
type TaskSeverity string

const (
	SeverityLow      TaskSeverity = "low"
	SeverityMedium   TaskSeverity = "medium"
	SeverityHigh     TaskSeverity = "high"
	SeverityCritical TaskSeverity = "critical"
)

// SeverityDisplayName returns a human-friendly label.
func (s TaskSeverity) SeverityDisplayName() string {
	switch s {
	case SeverityLow:
		return "Low"
	case SeverityMedium:
		return "Medium"
	case SeverityHigh:
		return "High"
	case SeverityCritical:
		return "Critical"
	default:
		return "Medium"
	}
}

// SeverityClass returns a CSS class suffix for the severity badge colour.
func (s TaskSeverity) SeverityClass() string {
	switch s {
	case SeverityLow:
		return "low"
	case SeverityMedium:
		return "medium"
	case SeverityHigh:
		return "high"
	case SeverityCritical:
		return "critical"
	default:
		return "medium"
	}
}

// ActivityLog records a single auditable event on a task.
type ActivityLog struct {
	ActorID   bson.ObjectID `bson:"actor_id"   json:"actor_id"`
	ActorName string        `bson:"actor_name" json:"actor_name"`
	Action    string        `bson:"action"     json:"action"`
	Timestamp time.Time     `bson:"timestamp"  json:"timestamp"`
}

// TaskComment represents a single threaded comment on a task.
type TaskComment struct {
	ID         bson.ObjectID `bson:"_id,omitempty" json:"id"`
	AuthorID   bson.ObjectID `bson:"author_id"     json:"author_id"`
	AuthorName string        `bson:"author_name"   json:"author_name"`
	Body       string        `bson:"body"          json:"body"`
	Timestamp  time.Time     `bson:"timestamp"     json:"timestamp"`
}

// Task represents a Kanban card within a project.
type Task struct {
	ID              bson.ObjectID `bson:"_id,omitempty"       json:"id"`
	ProjectID       bson.ObjectID `bson:"project_id"          json:"project_id"`
	Title           string        `bson:"title"               json:"title"`
	Description     string        `bson:"description"         json:"description"`
	Status          TaskStatus    `bson:"status"              json:"status"`
	Severity        TaskSeverity  `bson:"severity"            json:"severity"`
	AssigneeID      bson.ObjectID `bson:"assignee_id"         json:"assignee_id"`
	GDriveFileLinks []string      `bson:"gdrive_file_links"   json:"gdrive_file_links"`
	ActivityLog     []ActivityLog `bson:"activity_log"        json:"activity_log"`
	Comments        []TaskComment `bson:"comments"            json:"comments"`
	Deadline        *time.Time    `bson:"deadline,omitempty"  json:"deadline"`
	CreatedBy       bson.ObjectID `bson:"created_by"          json:"created_by"`
	CreatedAt       time.Time     `bson:"created_at"          json:"created_at"`
	UpdatedAt       time.Time     `bson:"updated_at"          json:"updated_at"`
}

// StatusDisplayName returns a human-friendly column label.
func (s TaskStatus) StatusDisplayName() string {
	switch s {
	case StatusTodo:
		return "To-Do"
	case StatusInProgress:
		return "In Progress"
	case StatusNeedsApproval:
		return "Needs Approval"
	case StatusDone:
		return "Done"
	default:
		return string(s)
	}
}

// StatusClass returns a CSS class suffix for the column badge colour.
func (s TaskStatus) StatusClass() string {
	switch s {
	case StatusTodo:
		return "todo"
	case StatusInProgress:
		return "in-progress"
	case StatusNeedsApproval:
		return "needs-approval"
	case StatusDone:
		return "done"
	default:
		return "todo"
	}
}

// IsValidStatus returns true if s is one of the four defined statuses.
func IsValidStatus(s string) bool {
	switch TaskStatus(s) {
	case StatusTodo, StatusInProgress, StatusNeedsApproval, StatusDone:
		return true
	}
	return false
}
