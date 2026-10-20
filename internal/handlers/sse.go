package handlers

import (
	"fmt"
	"html/template"
	"log"
	"net/http"
	"time"

	"arch_erp/internal/auth"
	"arch_erp/internal/db"
	"arch_erp/internal/models"
	"arch_erp/internal/services"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// SSEHandler manages Server-Sent Event streams for the real-time Kanban board.
type SSEHandler struct {
	hub   *services.Hub
	tmpls map[string]*template.Template
}

// NewSSEHandler creates an SSEHandler.
func NewSSEHandler(hub *services.Hub, tmpls map[string]*template.Template) *SSEHandler {
	return &SSEHandler{hub: hub, tmpls: tmpls}
}

// KanbanStream opens an SSE stream for the given project.
// When a KanbanEvent is published to the project's room, the handler fetches
// the latest task data and sends updated Kanban column HTML fragments via
// Datastar's data-merge-fragments SSE event format.
func (h *SSEHandler) KanbanStream(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	projectIDStr := r.PathValue("id")
	projectID, err := bson.ObjectIDFromHex(projectIDStr)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	// Verify the user has access to this project
	var projectFilter bson.M
	if user.Role == models.RoleAdmin {
		projectFilter = bson.M{"_id": projectID}
	} else {
		projectFilter = bson.M{"_id": projectID, "assigned_users": user.ID}
	}
	var project models.Project
	if err := db.Collections.Projects.FindOne(r.Context(), projectFilter).Decode(&project); err != nil {
		http.NotFound(w, r)
		return
	}

	// Set SSE response headers
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no") // Disable nginx buffering

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	// Subscribe to this project's event room
	ch, unsubscribe := h.hub.Subscribe(projectIDStr)
	defer unsubscribe()

	// Send initial board state immediately
	if err := h.sendBoardFragments(w, r, projectID, project, user); err != nil {
		log.Println("sse initial send error:", err)
		return
	}
	flusher.Flush()

	// Heartbeat ticker to keep the connection alive through proxies
	ticker := time.NewTicker(25 * time.Second)
	defer ticker.Stop()

	log.Printf("sse: client %s subscribed to project %s", user.UserID, projectIDStr)

	for {
		select {
		case <-r.Context().Done():
			log.Printf("sse: client %s disconnected from project %s", user.UserID, projectIDStr)
			return

		case <-ticker.C:
			// Send SSE heartbeat comment (keeps connection alive)
			fmt.Fprintf(w, ": heartbeat\n\n")
			flusher.Flush()

		case event, ok := <-ch:
			if !ok {
				return // Channel closed
			}
			log.Printf("sse: broadcasting event %s for project %s", event.Type, projectIDStr)

			// Re-fetch and send all four column fragments
			if err := h.sendBoardFragments(w, r, projectID, project, user); err != nil {
				log.Println("sse fragment send error:", err)
				return
			}
			flusher.Flush()
		}
	}
}

// sendBoardFragments fetches fresh task data and writes a Datastar merge-fragments SSE event.
func (h *SSEHandler) sendBoardFragments(w http.ResponseWriter, r *http.Request, projectID bson.ObjectID, project models.Project, user *models.User) error {
	cursor, err := db.Collections.Tasks.Find(r.Context(), bson.M{"project_id": projectID},
		options.Find().SetSort(bson.D{{Key: "created_at", Value: 1}}))
	if err != nil {
		return fmt.Errorf("fetching tasks: %w", err)
	}
	defer cursor.Close(r.Context())

	var allTasks []models.Task
	cursor.All(r.Context(), &allTasks)

	tasksByStatus := map[models.TaskStatus][]models.Task{}
	for _, t := range allTasks {
		tasksByStatus[t.Status] = append(tasksByStatus[t.Status], t)
	}
	userMap := buildUserMap(r, project.AssignedUsers)

	// Write a single Datastar event with merged HTML for all columns
	fmt.Fprintf(w, "event: datastar-merge-fragments\n")
	fmt.Fprintf(w, "data: mergeMode morph\n")
	fmt.Fprintf(w, "data: fragments ")

	for _, col := range models.KanbanColumns {
		data := map[string]interface{}{
			"Status":      col,
			"Tasks":       tasksByStatus[col],
			"CurrentUser": user,
			"Project":     project,
			"UserMap":     userMap,
		}
		// Render the template to a string buffer
		var buf = &sseBuffer{w: w}
		if err := h.tmpls["project_detail.html"].ExecuteTemplate(buf, "kanban_column.html", data); err != nil {
			return fmt.Errorf("rendering column: %w", err)
		}
	}

	fmt.Fprintf(w, "\n\n")
	return nil
}

// sseBuffer is a simple writer that escapes newlines for SSE data lines.
type sseBuffer struct {
	w http.ResponseWriter
}

func (b *sseBuffer) Write(p []byte) (n int, err error) {
	return b.w.Write(p)
}
