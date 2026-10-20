package models

import (
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
)

// Phase represents a project's lifecycle stage.
type Phase string

const (
	PhaseDiscussion Phase = "discussion"
	PhaseOngoing    Phase = "ongoing"
	PhaseCompleted  Phase = "completed"
)

// GDriveLink is a labelled Google Drive URL attached to a project.
type GDriveLink struct {
	Label string `bson:"label" json:"label"`
	URL   string `bson:"url"   json:"url"`
}

// ProjectComment represents a single discussion message on a project.
type ProjectComment struct {
	ID         bson.ObjectID `bson:"_id,omitempty" json:"id"`
	AuthorID   bson.ObjectID `bson:"author_id"     json:"author_id"`
	AuthorName string        `bson:"author_name"   json:"author_name"`
	Body       string        `bson:"body"          json:"body"`
	Timestamp  time.Time     `bson:"timestamp"     json:"timestamp"`
}

// Project represents an architecture firm project.
type Project struct {
	ID            bson.ObjectID    `bson:"_id,omitempty"    json:"id"`
	ProjectName   string           `bson:"project_name"     json:"project_name"`
	ClientName    string           `bson:"client_name"      json:"client_name"`
	Phase         Phase            `bson:"phase"            json:"phase"`
	GDriveLinks   []GDriveLink     `bson:"gdrive_links"     json:"gdrive_links"`
	AssignedUsers []bson.ObjectID  `bson:"assigned_users"   json:"assigned_users"`
	Comments      []ProjectComment `bson:"comments"         json:"comments"`
	CreatedBy     bson.ObjectID    `bson:"created_by"       json:"created_by"`
	CreatedAt     time.Time        `bson:"created_at"       json:"created_at"`
	UpdatedAt     time.Time        `bson:"updated_at"       json:"updated_at"`
}

// PhaseDisplayName returns a human-friendly label for the phase.
func (p Phase) PhaseDisplayName() string {
	switch p {
	case PhaseDiscussion:
		return "In Discussion"
	case PhaseOngoing:
		return "Active"
	case PhaseCompleted:
		return "Completed"
	default:
		return string(p)
	}
}

// PhaseClass returns a CSS class suffix for styling.
func (p Phase) PhaseClass() string {
	switch p {
	case PhaseDiscussion:
		return "discussion"
	case PhaseOngoing:
		return "ongoing"
	case PhaseCompleted:
		return "completed"
	default:
		return "discussion"
	}
}
