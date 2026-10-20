package handlers

import (
	"context"
	"html/template"
	"log"
	"net/http"
	"strings"
	"time"

	"arch_erp/internal/auth"
	"arch_erp/internal/db"
	"arch_erp/internal/models"
	"arch_erp/internal/services"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// TaskHandler handles task CRUD and Kanban card moves.
type TaskHandler struct {
	tmpls  map[string]*template.Template
	kanban *services.KanbanService
	notif  *services.NotificationService
}

// NewTaskHandler creates a TaskHandler.
func NewTaskHandler(tmpls map[string]*template.Template, kanban *services.KanbanService, notif *services.NotificationService) *TaskHandler {
	return &TaskHandler{tmpls: tmpls, kanban: kanban, notif: notif}
}

// Detail renders a task's detail/sidebar view.
func (h *TaskHandler) Detail(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	taskIDStr := r.PathValue("id")
	taskID, err := bson.ObjectIDFromHex(taskIDStr)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	var task models.Task
	if err := db.Collections.Tasks.FindOne(r.Context(), bson.M{"_id": taskID}).Decode(&task); err != nil {
		http.NotFound(w, r)
		return
	}

	// Fetch assignee details
	var assignee models.User
	db.Collections.Users.FindOne(r.Context(), bson.M{"_id": task.AssigneeID}).Decode(&assignee)

	// Fetch project
	var project models.Project
	db.Collections.Projects.FindOne(r.Context(), bson.M{"_id": task.ProjectID}).Decode(&project)

	// Fetch all users for re-assignment picker
	cursor, _ := db.Collections.Users.Find(r.Context(), bson.M{})
	var allUsers []models.User
	cursor.All(r.Context(), &allUsers)

	// Default severity if not set
	if task.Severity == "" {
		task.Severity = models.SeverityMedium
	}

	// Determine available status transitions for this user/task
	nextStatuses := availableTransitions(&task, user)

	data := map[string]interface{}{
		"Title":          task.Title + " — Arch ERP",
		"ActivePage":     "projects",
		"CurrentUser":    user,
		"Task":           task,
		"Assignee":       assignee,
		"Project":        project,
		"AllUsers":       allUsers,
		"Columns":        models.KanbanColumns,
		"NextStatuses":   nextStatuses,
		"IsAssignee":     task.AssigneeID == user.ID,
		"CanForward":     task.AssigneeID == user.ID || user.Role == models.RoleAdmin || user.Role == models.RoleSeniorArchitect,
		"SeverityLevels": []models.TaskSeverity{models.SeverityLow, models.SeverityMedium, models.SeverityHigh, models.SeverityCritical},
		"Error":          r.URL.Query().Get("error"),
		"Success":        r.URL.Query().Get("success"),
	}
	if err := h.tmpls["task_detail.html"].ExecuteTemplate(w, "task_detail.html", data); err != nil {
		log.Println("template error:", err)
	}
}

// availableTransitions returns the list of statuses the user can move to from current status.
func availableTransitions(task *models.Task, user *models.User) []models.TaskStatus {
	var next []models.TaskStatus

	// Admin and Senior Architect can move to any status
	if user.Role == models.RoleAdmin || user.Role == models.RoleSeniorArchitect {
		for _, s := range models.KanbanColumns {
			if s != task.Status {
				next = append(next, s)
			}
		}
		return next
	}

	// Assigned users can advance their own work through the delivery flow.
	if task.AssigneeID == user.ID {
		switch task.Status {
		case models.StatusTodo:
			next = []models.TaskStatus{models.StatusInProgress}
		case models.StatusInProgress:
			next = []models.TaskStatus{models.StatusNeedsApproval, models.StatusDone}
		case models.StatusNeedsApproval:
			next = []models.TaskStatus{models.StatusDone}
		}
	}
	return next
}

// Forward assigns a task to another user. Managers may forward any task;
// an assignee may forward their own task when work needs to be handed over.
func (h *TaskHandler) Forward(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	taskIDStr := r.PathValue("id")
	taskID, err := bson.ObjectIDFromHex(taskIDStr)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Redirect(w, r, "/tasks/"+taskIDStr+"?error=bad+request", http.StatusSeeOther)
		return
	}

	recipientID, err := bson.ObjectIDFromHex(r.FormValue("assignee_id"))
	if err != nil {
		http.Redirect(w, r, "/tasks/"+taskIDStr+"?error=select+a+valid+recipient", http.StatusSeeOther)
		return
	}
	var task models.Task
	if err := db.Collections.Tasks.FindOne(r.Context(), bson.M{"_id": taskID}).Decode(&task); err != nil {
		http.NotFound(w, r)
		return
	}
	canManage := user.Role == models.RoleAdmin || user.Role == models.RoleSeniorArchitect
	if !canManage && task.AssigneeID != user.ID {
		http.Error(w, "only the assignee or a manager can forward this task", http.StatusForbidden)
		return
	}
	if recipientID == task.AssigneeID {
		http.Redirect(w, r, "/tasks/"+taskIDStr+"?error=choose+a+different+recipient", http.StatusSeeOther)
		return
	}

	var recipient models.User
	if err := db.Collections.Users.FindOne(r.Context(), bson.M{"_id": recipientID}).Decode(&recipient); err != nil {
		http.Redirect(w, r, "/tasks/"+taskIDStr+"?error=recipient+not+found", http.StatusSeeOther)
		return
	}
	entry := models.ActivityLog{ActorID: user.ID, ActorName: user.Name, Action: "forwarded task to " + recipient.Name, Timestamp: time.Now()}
	_, err = db.Collections.Tasks.UpdateOne(r.Context(), bson.M{"_id": taskID}, bson.M{"$set": bson.M{"assignee_id": recipientID, "updated_at": time.Now()}, "$push": bson.M{"activity_log": entry}})
	if err != nil {
		log.Println("error forwarding task:", err)
		http.Redirect(w, r, "/tasks/"+taskIDStr+"?error=server+error", http.StatusSeeOther)
		return
	}
	go h.sendAssignmentEmail(context.Background(), recipientID, task.Title, task.ProjectID)
	http.Redirect(w, r, "/tasks/"+taskIDStr+"?success=task+forwarded", http.StatusSeeOther)
}

// Create handles new task creation (POST /projects/{id}/tasks).
func (h *TaskHandler) Create(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	projectIDStr := r.PathValue("id")
	projectID, err := bson.ObjectIDFromHex(projectIDStr)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	if err := r.ParseForm(); err != nil {
		http.Redirect(w, r, "/projects/"+projectIDStr+"?error=bad+request", http.StatusSeeOther)
		return
	}

	title := r.FormValue("title")
	if title == "" {
		http.Redirect(w, r, "/projects/"+projectIDStr+"?error=title+required", http.StatusSeeOther)
		return
	}

	assigneeIDStr := r.FormValue("assignee_id")
	var assigneeID bson.ObjectID
	if assigneeIDStr != "" {
		assigneeID, _ = bson.ObjectIDFromHex(assigneeIDStr)
	}

	severity := models.TaskSeverity(r.FormValue("severity"))
	if severity == "" {
		severity = models.SeverityMedium
	}

	var deadline *time.Time
	if dlStr := r.FormValue("deadline"); dlStr != "" {
		if t, err := time.Parse("2006-01-02", dlStr); err == nil {
			deadline = &t
		}
	}

	now := time.Now()
	task := models.Task{
		ProjectID:   projectID,
		Title:       title,
		Description: r.FormValue("description"),
		Status:      models.StatusTodo,
		Severity:    severity,
		AssigneeID:  assigneeID,
		Deadline:    deadline,
		ActivityLog: []models.ActivityLog{{
			ActorID:   user.ID,
			ActorName: user.Name,
			Action:    "created task",
			Timestamp: now,
		}},
		Comments:  []models.TaskComment{},
		CreatedBy: user.ID,
		CreatedAt: now,
		UpdatedAt: now,
	}

	_, err = db.Collections.Tasks.InsertOne(r.Context(), task)
	if err != nil {
		log.Println("error inserting task:", err)
		http.Redirect(w, r, "/projects/"+projectIDStr+"?error=server+error", http.StatusSeeOther)
		return
	}

	// Notify assignee via email (non-blocking)
	if !assigneeID.IsZero() {
		go h.sendAssignmentEmail(context.Background(), assigneeID, title, projectID)
	}

	http.Redirect(w, r, "/projects/"+projectIDStr, http.StatusSeeOther)
}

// Move handles a Kanban card move (POST /tasks/{id}/move).
// This is called by SortableJS onEnd via a plain fetch POST.
func (h *TaskHandler) Move(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	taskIDStr := r.PathValue("id")
	taskID, err := bson.ObjectIDFromHex(taskIDStr)
	if err != nil {
		http.Error(w, "invalid task id", http.StatusBadRequest)
		return
	}

	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	newStatus := models.TaskStatus(r.FormValue("status"))
	if !models.IsValidStatus(string(newStatus)) {
		http.Error(w, "invalid status", http.StatusBadRequest)
		return
	}

	if err := h.kanban.MoveTask(r.Context(), taskID, newStatus, user); err != nil {
		if moveErr, ok := err.(*services.MoveError); ok {
			http.Error(w, moveErr.Message, http.StatusForbidden)
			return
		}
		log.Println("error moving task:", err)
		http.Error(w, "server error", http.StatusInternalServerError)
		return
	}

	// Send email notifications for key transitions (non-blocking)
	go h.sendMoveNotifications(context.Background(), taskID, newStatus, user)

	// If it's a standard form submission (e.g. not fetch), redirect back.
	if strings.Contains(r.Header.Get("Accept"), "text/html") {
		http.Redirect(w, r, "/tasks/"+taskIDStr, http.StatusSeeOther)
		return
	}

	w.WriteHeader(http.StatusOK)
}

// Update handles task detail updates (POST /tasks/{id}/update).
// Role-based field restrictions:
//   - title, description: admin + senior_architect only
//   - assignee_id: admin + senior_architect only
//   - severity, gdrive_file_links: all roles except intern
func (h *TaskHandler) Update(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	taskIDStr := r.PathValue("id")
	taskID, err := bson.ObjectIDFromHex(taskIDStr)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	if err := r.ParseForm(); err != nil {
		http.Redirect(w, r, "/tasks/"+taskIDStr+"?error=bad+request", http.StatusSeeOther)
		return
	}

	var task models.Task
	if err := db.Collections.Tasks.FindOne(r.Context(), bson.M{"_id": taskID}).Decode(&task); err != nil {
		http.NotFound(w, r)
		return
	}

	setFields := bson.M{
		"updated_at": time.Now(),
	}

	// Admin / Senior Architect: can update all fields
	canEditCore := user.Role == models.RoleAdmin || user.Role == models.RoleSeniorArchitect
	// Junior Architect + Intern: can update severity and gdrive links only
	canEditMeta := user.Role == models.RoleJuniorArchitect

	if canEditCore {
		newTitle := strings.TrimSpace(r.FormValue("title"))
		if newTitle != "" {
			setFields["title"] = newTitle
		}
		setFields["description"] = r.FormValue("description")

		assigneeIDStr := r.FormValue("assignee_id")
		newAssigneeID := task.AssigneeID
		if assigneeIDStr != "" {
			if oid, err := bson.ObjectIDFromHex(assigneeIDStr); err == nil {
				newAssigneeID = oid
			}
		} else {
			newAssigneeID = bson.NilObjectID
		}
		setFields["assignee_id"] = newAssigneeID
	}

	if canEditCore || canEditMeta {
		// Parse GDrive file links
		var fileLinks []string
		for _, link := range r.Form["gdrive_file_links"] {
			if link != "" {
				fileLinks = append(fileLinks, link)
			}
		}
		setFields["gdrive_file_links"] = fileLinks

		// Severity
		sev := models.TaskSeverity(r.FormValue("severity"))
		if sev == "" {
			sev = models.SeverityMedium
		}
		setFields["severity"] = sev

		// Deadline
		if dlStr := r.FormValue("deadline"); dlStr != "" {
			if t, err := time.Parse("2006-01-02", dlStr); err == nil {
				setFields["deadline"] = t
			}
		} else {
			setFields["deadline"] = nil
		}
	}

	logEntry := models.ActivityLog{
		ActorID:   user.ID,
		ActorName: user.Name,
		Action:    "updated task details",
		Timestamp: time.Now(),
	}

	_, err = db.Collections.Tasks.UpdateOne(r.Context(),
		bson.M{"_id": taskID},
		bson.M{
			"$set":  setFields,
			"$push": bson.M{"activity_log": logEntry},
		},
	)
	if err != nil {
		log.Println("error updating task:", err)
		http.Redirect(w, r, "/tasks/"+taskIDStr+"?error=server+error", http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/tasks/"+taskIDStr+"?success=updated", http.StatusSeeOther)
}

// AddComment appends a new comment to a task (POST /tasks/{id}/comment).
func (h *TaskHandler) AddComment(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	taskIDStr := r.PathValue("id")
	taskID, err := bson.ObjectIDFromHex(taskIDStr)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	if err := r.ParseForm(); err != nil {
		http.Redirect(w, r, "/tasks/"+taskIDStr+"?error=bad+request", http.StatusSeeOther)
		return
	}

	body := strings.TrimSpace(r.FormValue("body"))
	if body == "" {
		http.Redirect(w, r, "/tasks/"+taskIDStr+"?error=comment+cannot+be+empty", http.StatusSeeOther)
		return
	}

	comment := models.TaskComment{
		ID:         bson.NewObjectID(),
		AuthorID:   user.ID,
		AuthorName: user.Name,
		Body:       body,
		Timestamp:  time.Now(),
	}

	_, err = db.Collections.Tasks.UpdateOne(r.Context(),
		bson.M{"_id": taskID},
		bson.M{"$push": bson.M{"comments": comment}},
	)
	if err != nil {
		log.Println("error adding comment:", err)
		http.Redirect(w, r, "/tasks/"+taskIDStr+"?error=server+error", http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/tasks/"+taskIDStr+"?success=comment+added#comments", http.StatusSeeOther)
}

// Delete removes a task (Admin/Senior Architect only).
func (h *TaskHandler) Delete(w http.ResponseWriter, r *http.Request) {
	taskIDStr := r.PathValue("id")
	taskID, err := bson.ObjectIDFromHex(taskIDStr)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	var task models.Task
	if err := db.Collections.Tasks.FindOne(r.Context(), bson.M{"_id": taskID}).Decode(&task); err != nil {
		http.NotFound(w, r)
		return
	}
	projectIDStr := task.ProjectID.Hex()

	if _, err := db.Collections.Tasks.DeleteOne(r.Context(), bson.M{"_id": taskID}); err != nil {
		log.Println("error deleting task:", err)
		http.Redirect(w, r, "/projects/"+projectIDStr+"?error=server+error", http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/projects/"+projectIDStr, http.StatusSeeOther)
}

// ListByProject returns JSON list of tasks for a project (used by SSE re-render).
func (h *TaskHandler) ListByProject(w http.ResponseWriter, r *http.Request) {
	projectIDStr := r.PathValue("id")
	projectID, err := bson.ObjectIDFromHex(projectIDStr)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	cursor, err := db.Collections.Tasks.Find(r.Context(), bson.M{"project_id": projectID},
		options.Find().SetSort(bson.D{{Key: "created_at", Value: 1}}))
	if err != nil {
		http.Error(w, "server error", http.StatusInternalServerError)
		return
	}
	var tasks []models.Task
	cursor.All(r.Context(), &tasks)

	tasksByStatus := map[models.TaskStatus][]models.Task{}
	for _, t := range tasks {
		tasksByStatus[t.Status] = append(tasksByStatus[t.Status], t)
	}

	user := auth.UserFromContext(r.Context())
	var project models.Project
	db.Collections.Projects.FindOne(r.Context(), bson.M{"_id": projectID}).Decode(&project)
	userMap := buildUserMap(r, project.AssignedUsers)

	// Render all four column fragments merged via Datastar
	w.Header().Set("Content-Type", "text/html")
	for _, col := range models.KanbanColumns {
		colTasks := tasksByStatus[col]
		data := map[string]interface{}{
			"Status":      col,
			"Tasks":       colTasks,
			"CurrentUser": user,
			"Project":     project,
			"UserMap":     userMap,
		}
		if err := h.tmpls["task_detail.html"].ExecuteTemplate(w, "kanban_column.html", data); err != nil {
			log.Println("template error:", err)
		}
	}
}

// ── Private helpers ──────────────────────────────────────────────────────────

func (h *TaskHandler) sendAssignmentEmail(ctx context.Context, assigneeID bson.ObjectID, taskTitle string, projectID bson.ObjectID) {
	var assignee models.User
	if err := db.Collections.Users.FindOne(ctx, bson.M{"_id": assigneeID}).Decode(&assignee); err != nil {
		return
	}
	var project models.Project
	db.Collections.Projects.FindOne(ctx, bson.M{"_id": projectID}).Decode(&project)
	h.notif.NotifyTaskAssigned(ctx, assignee.Email, assignee.Name, taskTitle, project.ProjectName)
}

func (h *TaskHandler) sendMoveNotifications(ctx context.Context, taskID bson.ObjectID, newStatus models.TaskStatus, actor *models.User) {
	var task models.Task
	if err := db.Collections.Tasks.FindOne(ctx, bson.M{"_id": taskID}).Decode(&task); err != nil {
		return
	}
	var project models.Project
	db.Collections.Projects.FindOne(ctx, bson.M{"_id": task.ProjectID}).Decode(&project)

	switch newStatus {
	case models.StatusNeedsApproval:
		// Notify all Senior Architects and Admins
		cursor, err := db.Collections.Users.Find(ctx, bson.M{
			"role": bson.M{"$in": []models.Role{models.RoleAdmin, models.RoleSeniorArchitect}},
		})
		if err != nil {
			return
		}
		var seniors []models.User
		cursor.All(ctx, &seniors)
		emails := make([]string, 0, len(seniors))
		for _, s := range seniors {
			if s.ID != actor.ID {
				emails = append(emails, s.Email)
			}
		}
		h.notif.NotifyNeedsApproval(ctx, emails, task.Title, project.ProjectName, actor.Name)

	case models.StatusDone:
		// Notify the assignee
		if task.AssigneeID.IsZero() {
			return
		}
		var assignee models.User
		if err := db.Collections.Users.FindOne(ctx, bson.M{"_id": task.AssigneeID}).Decode(&assignee); err != nil {
			return
		}
		h.notif.NotifyTaskDone(ctx, assignee.Email, assignee.Name, task.Title, project.ProjectName, actor.Name)
	}
}
