package handlers

import (
	"html/template"
	"log"
	"net/http"
	"sort"
	"strings"
	"time"

	"arch_erp/internal/auth"
	"arch_erp/internal/db"
	"arch_erp/internal/models"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// ProjectHandler handles project CRUD and user assignment.
type ProjectHandler struct {
	tmpls map[string]*template.Template
}

// NewProjectHandler creates a ProjectHandler.
func NewProjectHandler(tmpls map[string]*template.Template) *ProjectHandler {
	return &ProjectHandler{tmpls: tmpls}
}

// List renders the project list page filtered by the user's role.
// Supports ?sort=name|date_asc|date_desc (default: date_desc).
func (h *ProjectHandler) List(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())

	var filter bson.M
	if user.Role == models.RoleAdmin {
		filter = bson.M{} // Admin sees all projects
	} else {
		filter = bson.M{"assigned_users": user.ID}
	}

	cursor, err := db.Collections.Projects.Find(r.Context(), filter,
		options.Find().SetSort(bson.D{{Key: "created_at", Value: -1}}))
	if err != nil {
		log.Println("error fetching projects:", err)
		http.Error(w, "server error", http.StatusInternalServerError)
		return
	}
	defer cursor.Close(r.Context())

	var projects []models.Project
	if err := cursor.All(r.Context(), &projects); err != nil {
		http.Error(w, "server error", http.StatusInternalServerError)
		return
	}

	// Apply client-side sort preference
	sortBy := r.URL.Query().Get("sort")
	switch sortBy {
	case "name":
		sort.Slice(projects, func(i, j int) bool {
			return strings.ToLower(projects[i].ProjectName) < strings.ToLower(projects[j].ProjectName)
		})
	case "date_asc":
		sort.Slice(projects, func(i, j int) bool {
			return projects[i].CreatedAt.Before(projects[j].CreatedAt)
		})
	default: // date_desc (already sorted by DB)
	}

	// Group projects by phase
	grouped := map[models.Phase][]models.Project{
		models.PhaseOngoing:    {},
		models.PhaseDiscussion: {},
		models.PhaseCompleted:  {},
	}
	for _, p := range projects {
		grouped[p.Phase] = append(grouped[p.Phase], p)
	}

	data := map[string]interface{}{
		"Title":       "Projects — Arch ERP",
		"ActivePage":  "projects",
		"CurrentUser": user,
		"Projects":    projects,
		"Ongoing":     grouped[models.PhaseOngoing],
		"Discussion":  grouped[models.PhaseDiscussion],
		"Completed":   grouped[models.PhaseCompleted],
		"SortBy":      sortBy,
	}
	if err := h.tmpls["project_list.html"].ExecuteTemplate(w, "project_list.html", data); err != nil {
		log.Println("template error:", err)
	}
}

// Dashboard renders the dashboard summary page.
func (h *ProjectHandler) Dashboard(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())

	var projectFilter bson.M
	if user.Role == models.RoleAdmin {
		projectFilter = bson.M{}
	} else {
		projectFilter = bson.M{"assigned_users": user.ID}
	}

	// Count active (ongoing) projects
	activeFilter := bson.M{"phase": models.PhaseOngoing}
	if user.Role != models.RoleAdmin {
		activeFilter["assigned_users"] = user.ID
	}
	projectCount, _ := db.Collections.Projects.CountDocuments(r.Context(), activeFilter)

	_ = projectFilter // kept for future use

	var taskFilter bson.M
	if user.Role == models.RoleAdmin {
		taskFilter = bson.M{}
	} else {
		taskFilter = bson.M{"assignee_id": user.ID}
	}
	taskCount, _ := db.Collections.Tasks.CountDocuments(r.Context(), taskFilter)
	pendingCount, _ := db.Collections.Tasks.CountDocuments(r.Context(), bson.M{
		"status": models.StatusNeedsApproval,
	})

	data := map[string]interface{}{
		"Title":        "Dashboard — Arch ERP",
		"ActivePage":   "dashboard",
		"CurrentUser":  user,
		"ProjectCount": projectCount,
		"TaskCount":    taskCount,
		"PendingCount": pendingCount,
	}
	if err := h.tmpls["dashboard.html"].ExecuteTemplate(w, "dashboard.html", data); err != nil {
		log.Println("template error:", err)
	}
}

// NewPage renders the create project form (Admin/Senior Architect only).
func (h *ProjectHandler) NewPage(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	// Fetch all users for the assignees picker
	cursor, _ := db.Collections.Users.Find(r.Context(), bson.M{})
	var allUsers []models.User
	cursor.All(r.Context(), &allUsers)

	data := map[string]interface{}{
		"Title":       "New Project — Arch ERP",
		"ActivePage":  "projects",
		"CurrentUser": user,
		"AllUsers":    allUsers,
		"Error":       r.URL.Query().Get("error"),
	}
	if err := h.tmpls["project_new.html"].ExecuteTemplate(w, "project_new.html", data); err != nil {
		log.Println("template error:", err)
	}
}

// Create processes the new project form.
func (h *ProjectHandler) Create(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	if err := r.ParseForm(); err != nil {
		http.Redirect(w, r, "/projects/new?error=bad+request", http.StatusSeeOther)
		return
	}

	name := r.FormValue("project_name")
	client := r.FormValue("client_name")
	phase := models.Phase(r.FormValue("phase"))

	if name == "" || client == "" {
		http.Redirect(w, r, "/projects/new?error=name+and+client+required", http.StatusSeeOther)
		return
	}

	// Parse assigned users
	assignedIDs := []bson.ObjectID{}
	for _, idStr := range r.Form["assigned_users"] {
		oid, err := bson.ObjectIDFromHex(idStr)
		if err == nil {
			assignedIDs = append(assignedIDs, oid)
		}
	}

	// Parse GDrive links (label:url pairs submitted as separate arrays)
	labels := r.Form["gdrive_label"]
	urls := r.Form["gdrive_url"]
	var links []models.GDriveLink
	for i := 0; i < len(labels) && i < len(urls); i++ {
		if labels[i] != "" && urls[i] != "" {
			links = append(links, models.GDriveLink{Label: labels[i], URL: urls[i]})
		}
	}

	project := models.Project{
		ProjectName:   name,
		ClientName:    client,
		Phase:         phase,
		GDriveLinks:   links,
		AssignedUsers: assignedIDs,
		Comments:      []models.ProjectComment{},
		CreatedBy:     user.ID,
		CreatedAt:     time.Now(),
		UpdatedAt:     time.Now(),
	}

	res, err := db.Collections.Projects.InsertOne(r.Context(), project)
	if err != nil {
		log.Println("error inserting project:", err)
		http.Redirect(w, r, "/projects/new?error=server+error", http.StatusSeeOther)
		return
	}

	projectID := res.InsertedID.(bson.ObjectID).Hex()
	http.Redirect(w, r, "/projects/"+projectID, http.StatusSeeOther)
}

// Detail renders the Kanban board for a single project.
func (h *ProjectHandler) Detail(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	projectIDStr := r.PathValue("id")

	projectID, err := bson.ObjectIDFromHex(projectIDStr)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	// Check access
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

	// Fetch tasks for this project
	taskFilter := bson.M{"project_id": projectID}
	
	// Non-admin/senior users only see tasks assigned to them (or unassigned tasks they can pick up)
	// We'll just show them tasks assigned to them for simplicity based on the request.
	if user.Role == models.RoleJuniorArchitect || user.Role == models.RoleIntern {
		taskFilter["assignee_id"] = user.ID
	}

	cursor, err := db.Collections.Tasks.Find(r.Context(), taskFilter,
		options.Find().SetSort(bson.D{{Key: "created_at", Value: 1}}))
	if err != nil {
		http.Error(w, "server error", http.StatusInternalServerError)
		return
	}
	defer cursor.Close(r.Context())

	var allTasks []models.Task
	cursor.All(r.Context(), &allTasks)

	// Group tasks by status
	tasksByStatus := map[models.TaskStatus][]models.Task{}
	for _, t := range allTasks {
		tasksByStatus[t.Status] = append(tasksByStatus[t.Status], t)
	}

	// Fetch user map for display names
	userMap := buildUserMap(r, project.AssignedUsers)

	// Fetch all users for the new-task modal assignee picker
	allUsersCursor, _ := db.Collections.Users.Find(r.Context(), bson.M{})
	var allUsers []models.User
	if allUsersCursor != nil {
		allUsersCursor.All(r.Context(), &allUsers)
	}

	data := map[string]interface{}{
		"Title":         project.ProjectName + " — Arch ERP",
		"ActivePage":    "projects",
		"CurrentUser":   user,
		"Project":       project,
		"TasksByStatus": tasksByStatus,
		"Columns":       models.KanbanColumns,
		"UserMap":       userMap,
		"AllUsers":      allUsers,
		"Error":         r.URL.Query().Get("error"),
		"Success":       r.URL.Query().Get("success"),
	}
	if err := h.tmpls["project_detail.html"].ExecuteTemplate(w, "project_detail.html", data); err != nil {
		log.Println("template error:", err)
	}
}

// EditPage renders the edit project form.
func (h *ProjectHandler) EditPage(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	projectIDStr := r.PathValue("id")
	projectID, err := bson.ObjectIDFromHex(projectIDStr)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	var project models.Project
	if err := db.Collections.Projects.FindOne(r.Context(), bson.M{"_id": projectID}).Decode(&project); err != nil {
		http.NotFound(w, r)
		return
	}

	cursor, _ := db.Collections.Users.Find(r.Context(), bson.M{})
	var allUsers []models.User
	cursor.All(r.Context(), &allUsers)

	assignedSet := map[string]bool{}
	for _, uid := range project.AssignedUsers {
		assignedSet[uid.Hex()] = true
	}

	data := map[string]interface{}{
		"Title":       "Edit Project — Arch ERP",
		"ActivePage":  "projects",
		"CurrentUser": user,
		"Project":     project,
		"AllUsers":    allUsers,
		"AssignedSet": assignedSet,
		"Error":       r.URL.Query().Get("error"),
	}
	if err := h.tmpls["project_edit.html"].ExecuteTemplate(w, "project_edit.html", data); err != nil {
		log.Println("template error:", err)
	}
}

// Update processes the edit project form.
func (h *ProjectHandler) Update(w http.ResponseWriter, r *http.Request) {
	projectIDStr := r.PathValue("id")
	projectID, err := bson.ObjectIDFromHex(projectIDStr)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	if err := r.ParseForm(); err != nil {
		http.Redirect(w, r, "/projects/"+projectIDStr+"/edit?error=bad+request", http.StatusSeeOther)
		return
	}

	assignedIDs := []bson.ObjectID{}
	for _, idStr := range r.Form["assigned_users"] {
		if oid, err := bson.ObjectIDFromHex(idStr); err == nil {
			assignedIDs = append(assignedIDs, oid)
		}
	}

	labels := r.Form["gdrive_label"]
	urls := r.Form["gdrive_url"]
	var links []models.GDriveLink
	for i := 0; i < len(labels) && i < len(urls); i++ {
		if labels[i] != "" && urls[i] != "" {
			links = append(links, models.GDriveLink{Label: labels[i], URL: urls[i]})
		}
	}

	_, err = db.Collections.Projects.UpdateOne(r.Context(),
		bson.M{"_id": projectID},
		bson.M{"$set": bson.M{
			"project_name":   r.FormValue("project_name"),
			"client_name":    r.FormValue("client_name"),
			"phase":          r.FormValue("phase"),
			"gdrive_links":   links,
			"assigned_users": assignedIDs,
			"updated_at":     time.Now(),
		}},
	)
	if err != nil {
		log.Println("error updating project:", err)
		http.Redirect(w, r, "/projects/"+projectIDStr+"/edit?error=server+error", http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/projects/"+projectIDStr, http.StatusSeeOther)
}

// AddComment appends a discussion comment to a project (POST /projects/{id}/comment).
func (h *ProjectHandler) AddComment(w http.ResponseWriter, r *http.Request) {
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

	body := strings.TrimSpace(r.FormValue("body"))
	if body == "" {
		http.Redirect(w, r, "/projects/"+projectIDStr+"?error=comment+cannot+be+empty", http.StatusSeeOther)
		return
	}

	comment := models.ProjectComment{
		ID:         bson.NewObjectID(),
		AuthorID:   user.ID,
		AuthorName: user.Name,
		Body:       body,
		Timestamp:  time.Now(),
	}

	_, err = db.Collections.Projects.UpdateOne(r.Context(),
		bson.M{"_id": projectID},
		bson.M{"$push": bson.M{"comments": comment}},
	)
	if err != nil {
		log.Println("error adding project comment:", err)
		http.Redirect(w, r, "/projects/"+projectIDStr+"?error=server+error", http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/projects/"+projectIDStr+"?success=comment+added#chat-modal", http.StatusSeeOther)
}

// buildUserMap fetches users by their IDs and returns a map[hex_id]User for template lookups.
func buildUserMap(r *http.Request, ids []bson.ObjectID) map[string]models.User {
	if len(ids) == 0 {
		return map[string]models.User{}
	}
	cursor, err := db.Collections.Users.Find(r.Context(), bson.M{"_id": bson.M{"$in": ids}})
	if err != nil {
		return map[string]models.User{}
	}
	var users []models.User
	cursor.All(r.Context(), &users)
	m := make(map[string]models.User, len(users))
	for _, u := range users {
		m[u.ID.Hex()] = u
	}
	return m
}
