package services

import (
	"context"
	"fmt"
	"time"

	"arch_erp/internal/db"
	"arch_erp/internal/models"

	"go.mongodb.org/mongo-driver/v2/bson"
)

// MoveError is returned when a role-based permission check fails.
type MoveError struct {
	Message string
}

func (e *MoveError) Error() string { return e.Message }

// KanbanService handles task status transitions and audit logging.
type KanbanService struct {
	hub *Hub
}

// NewKanbanService creates a KanbanService backed by the given Hub.
func NewKanbanService(hub *Hub) *KanbanService {
	return &KanbanService{hub: hub}
}

// MoveTask transitions a task to a new status.
// It validates the actor's RBAC permissions, executes the update + audit log
// inside a MongoDB multi-document transaction, and publishes a Kanban event.
func (s *KanbanService) MoveTask(ctx context.Context, taskID bson.ObjectID, newStatus models.TaskStatus, actor *models.User) error {
	// --- Fetch the current task ---
	var task models.Task
	err := db.Collections.Tasks.FindOne(ctx, bson.M{"_id": taskID}).Decode(&task)
	if err != nil {
		return fmt.Errorf("task not found: %w", err)
	}

	// --- RBAC: check whether this role may perform this transition ---
	if err := validateTransition(&task, newStatus, actor); err != nil {
		return err
	}

	// --- Build audit log entry ---
	logEntry := models.ActivityLog{
		ActorID:   actor.ID,
		ActorName: actor.Name,
		Action:    fmt.Sprintf("moved from \"%s\" to \"%s\"", task.Status.StatusDisplayName(), newStatus.StatusDisplayName()),
		Timestamp: time.Now(),
	}

	// A single-document update is atomic in MongoDB. Using it instead of a
	// transaction also works with the standalone MongoDB deployment in compose.
	update := bson.M{
		"$set":  bson.M{"status": newStatus, "updated_at": time.Now()},
		"$push": bson.M{"activity_log": logEntry},
	}
	res, err := db.Collections.Tasks.UpdateOne(ctx, bson.M{"_id": taskID}, update)
	if err != nil {
		return fmt.Errorf("updating task: %w", err)
	}
	if res.MatchedCount == 0 {
		return fmt.Errorf("task not found")
	}

	// --- Publish real-time event to all Kanban board subscribers ---
	s.hub.Publish(task.ProjectID.Hex(), KanbanEvent{
		Type:      EventCardMoved,
		ProjectID: task.ProjectID.Hex(),
		TaskID:    taskID.Hex(),
	})

	return nil
}

// validateTransition enforces the RBAC move matrix.
func validateTransition(task *models.Task, to models.TaskStatus, actor *models.User) error {
	if task.Status == to {
		return &MoveError{Message: "task is already in that status"}
	}
	if actor.Role == models.RoleAdmin || actor.Role == models.RoleSeniorArchitect {
		return nil
	}
	if task.AssigneeID != actor.ID {
		return &MoveError{Message: "only the assignee can advance this task"}
	}

	switch task.Status {
	case models.StatusTodo:
		if to == models.StatusInProgress {
			return nil
		}
	case models.StatusInProgress:
		if to == models.StatusNeedsApproval || to == models.StatusDone {
			return nil
		}
	case models.StatusNeedsApproval:
		if to == models.StatusDone {
			return nil
		}
	}
	return &MoveError{Message: "this task cannot be moved to that status"}
}
