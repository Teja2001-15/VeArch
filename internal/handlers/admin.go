package handlers

import (
	"html/template"
	"log"
	"net/http"
	"time"

	"arch_erp/internal/auth"
	"arch_erp/internal/db"
	"arch_erp/internal/models"

	"go.mongodb.org/mongo-driver/v2/bson"
)

// AdminHandler manages user administration (Admin role only).
type AdminHandler struct {
	tmpls map[string]*template.Template
}

// NewAdminHandler creates an AdminHandler.
func NewAdminHandler(tmpls map[string]*template.Template) *AdminHandler {
	return &AdminHandler{tmpls: tmpls}
}

// UserList renders the user management page.
func (h *AdminHandler) UserList(w http.ResponseWriter, r *http.Request) {
	currentUser := auth.UserFromContext(r.Context())

	cursor, err := db.Collections.Users.Find(r.Context(), bson.M{})
	if err != nil {
		http.Error(w, "server error", http.StatusInternalServerError)
		return
	}
	defer cursor.Close(r.Context())

	var users []models.User
	if err := cursor.All(r.Context(), &users); err != nil {
		http.Error(w, "server error", http.StatusInternalServerError)
		return
	}

	data := map[string]interface{}{
		"Title":       "User Management — Arch ERP",
		"ActivePage":  "admin-users",
		"CurrentUser": currentUser,
		"Users":       users,
		"Roles":       []models.Role{models.RoleAdmin, models.RoleSeniorArchitect, models.RoleJuniorArchitect, models.RoleIntern},
		"Error":       r.URL.Query().Get("error"),
		"Success":     r.URL.Query().Get("success"),
	}
	if err := h.tmpls["admin_users.html"].ExecuteTemplate(w, "admin_users.html", data); err != nil {
		log.Println("template error:", err)
	}
}

// CreateUser handles new user creation.
func (h *AdminHandler) CreateUser(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Redirect(w, r, "/admin/users?error=bad+request", http.StatusSeeOther)
		return
	}

	userID := r.FormValue("user_id")
	name := r.FormValue("name")
	email := r.FormValue("email")
	password := r.FormValue("password")
	role := models.Role(r.FormValue("role"))

	if userID == "" || name == "" || password == "" {
		http.Redirect(w, r, "/admin/users?error=user_id+name+and+password+required", http.StatusSeeOther)
		return
	}

	hash, err := auth.HashPassword(password)
	if err != nil {
		log.Println("error hashing password:", err)
		http.Redirect(w, r, "/admin/users?error=server+error", http.StatusSeeOther)
		return
	}

	user := models.User{
		UserID:       userID,
		PasswordHash: hash,
		Name:         name,
		Email:        email,
		Role:         role,
		TokenVersion: 0,
		CreatedAt:    time.Now(),
	}

	_, err = db.Collections.Users.InsertOne(r.Context(), user)
	if err != nil {
		log.Println("error inserting user:", err)
		http.Redirect(w, r, "/admin/users?error=user_id+already+exists", http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/admin/users?success=user+created", http.StatusSeeOther)
}

// UpdateUser handles role changes for an existing user.
func (h *AdminHandler) UpdateUser(w http.ResponseWriter, r *http.Request) {
	targetIDStr := r.PathValue("id")
	targetID, err := bson.ObjectIDFromHex(targetIDStr)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	if err := r.ParseForm(); err != nil {
		http.Redirect(w, r, "/admin/users?error=bad+request", http.StatusSeeOther)
		return
	}

	newRole := models.Role(r.FormValue("role"))
	newName := r.FormValue("name")
	newEmail := r.FormValue("email")

	_, err = db.Collections.Users.UpdateOne(r.Context(),
		bson.M{"_id": targetID},
		bson.M{"$set": bson.M{
			"role":  newRole,
			"name":  newName,
			"email": newEmail,
		}},
	)
	if err != nil {
		log.Println("error updating user:", err)
		http.Redirect(w, r, "/admin/users?error=server+error", http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/admin/users?success=user+updated", http.StatusSeeOther)
}

// ResetPassword sets a new password for a user and revokes all their sessions.
func (h *AdminHandler) ResetPassword(w http.ResponseWriter, r *http.Request) {
	targetIDStr := r.PathValue("id")
	targetID, err := bson.ObjectIDFromHex(targetIDStr)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	if err := r.ParseForm(); err != nil {
		http.Redirect(w, r, "/admin/users?error=bad+request", http.StatusSeeOther)
		return
	}

	newPass := r.FormValue("new_password")
	if len(newPass) < 8 {
		http.Redirect(w, r, "/admin/users?error=password+too+short", http.StatusSeeOther)
		return
	}

	hash, err := auth.HashPassword(newPass)
	if err != nil {
		http.Redirect(w, r, "/admin/users?error=server+error", http.StatusSeeOther)
		return
	}

	// Increment TokenVersion to revoke all existing sessions for this user
	_, err = db.Collections.Users.UpdateOne(r.Context(),
		bson.M{"_id": targetID},
		bson.M{"$set": bson.M{"password_hash": hash}, "$inc": bson.M{"token_version": 1}},
	)
	if err != nil {
		log.Println("error resetting password:", err)
		http.Redirect(w, r, "/admin/users?error=server+error", http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/admin/users?success=password+reset", http.StatusSeeOther)
}

// DeleteUser removes a user account (cannot delete self).
func (h *AdminHandler) DeleteUser(w http.ResponseWriter, r *http.Request) {
	currentUser := auth.UserFromContext(r.Context())
	targetIDStr := r.PathValue("id")
	targetID, err := bson.ObjectIDFromHex(targetIDStr)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	if targetID == currentUser.ID {
		http.Redirect(w, r, "/admin/users?error=cannot+delete+yourself", http.StatusSeeOther)
		return
	}

	_, err = db.Collections.Users.DeleteOne(r.Context(), bson.M{"_id": targetID})
	if err != nil {
		log.Println("error deleting user:", err)
		http.Redirect(w, r, "/admin/users?error=server+error", http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/admin/users?success=user+deleted", http.StatusSeeOther)
}
