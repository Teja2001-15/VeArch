package handlers

import (
	"html/template"
	"log"
	"net/http"
	"time"

	"arch_erp/internal/auth"
	"arch_erp/internal/config"
	"arch_erp/internal/db"
	"arch_erp/internal/models"

	"go.mongodb.org/mongo-driver/v2/bson"
)

// AuthHandler handles login and logout.
type AuthHandler struct {
	cfg   *config.Config
	tmpls map[string]*template.Template
}

// NewAuthHandler creates an AuthHandler.
func NewAuthHandler(cfg *config.Config, tmpls map[string]*template.Template) *AuthHandler {
	return &AuthHandler{cfg: cfg, tmpls: tmpls}
}

// LoginPage renders the login form.
func (h *AuthHandler) LoginPage(w http.ResponseWriter, r *http.Request) {
	// If already authenticated, redirect to dashboard
	if cookie, err := r.Cookie("auth_token"); err == nil && cookie.Value != "" {
		if _, err := auth.VerifyTokenWithDB(r.Context(), cookie.Value, h.cfg.JWTSecret, db.Collections.Users); err == nil {
			http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
			return
		}
	}
	data := map[string]interface{}{
		"Title": "Sign In — Arch ERP",
		"Error": r.URL.Query().Get("error"),
	}
	if err := h.tmpls["login.html"].ExecuteTemplate(w, "login.html", data); err != nil {
		log.Println("template error:", err)
	}
}

// Login processes the login form submission.
func (h *AuthHandler) Login(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Redirect(w, r, "/login?error=bad+request", http.StatusSeeOther)
		return
	}

	userID := r.FormValue("user_id")
	password := r.FormValue("password")

	if userID == "" || password == "" {
		http.Redirect(w, r, "/login?error=credentials+required", http.StatusSeeOther)
		return
	}

	var user models.User
	err := db.Collections.Users.FindOne(r.Context(), bson.M{"user_id": userID}).Decode(&user)
	if err != nil {
		http.Redirect(w, r, "/login?error=invalid+credentials", http.StatusSeeOther)
		return
	}

	if !auth.VerifyPassword(user.PasswordHash, password) {
		http.Redirect(w, r, "/login?error=invalid+credentials", http.StatusSeeOther)
		return
	}

	token, err := auth.SignToken(&user, h.cfg.JWTSecret, h.cfg.JWTExpiryHours)
	if err != nil {
		log.Println("error signing token:", err)
		http.Redirect(w, r, "/login?error=server+error", http.StatusSeeOther)
		return
	}

	auth.SetAuthCookie(w, token, h.cfg.JWTExpiryHours)
	http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
}

// Logout clears the auth cookie and redirects to login.
func (h *AuthHandler) Logout(w http.ResponseWriter, r *http.Request) {
	auth.ClearAuthCookie(w)
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

// ChangePasswordPage renders the change-password form.
func (h *AuthHandler) ChangePasswordPage(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	data := map[string]interface{}{
		"Title":       "Change Password — Arch ERP",
		"ActivePage":  "",
		"CurrentUser": user,
		"Error":       r.URL.Query().Get("error"),
		"Success":     r.URL.Query().Get("success"),
	}
	if err := h.tmpls["change_password.html"].ExecuteTemplate(w, "change_password.html", data); err != nil {
		log.Println("template error:", err)
	}
}

// ChangePassword processes the password change form.
func (h *AuthHandler) ChangePassword(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	if err := r.ParseForm(); err != nil {
		http.Redirect(w, r, "/profile/password?error=bad+request", http.StatusSeeOther)
		return
	}

	current := r.FormValue("current_password")
	newPass := r.FormValue("new_password")
	confirm := r.FormValue("confirm_password")

	if !auth.VerifyPassword(user.PasswordHash, current) {
		http.Redirect(w, r, "/profile/password?error=current+password+incorrect", http.StatusSeeOther)
		return
	}
	if newPass != confirm {
		http.Redirect(w, r, "/profile/password?error=passwords+do+not+match", http.StatusSeeOther)
		return
	}
	if len(newPass) < 8 {
		http.Redirect(w, r, "/profile/password?error=password+must+be+at+least+8+characters", http.StatusSeeOther)
		return
	}

	hash, err := auth.HashPassword(newPass)
	if err != nil {
		log.Println("error hashing password:", err)
		http.Redirect(w, r, "/profile/password?error=server+error", http.StatusSeeOther)
		return
	}

	// Increment TokenVersion to invalidate all existing sessions
	_, err = db.Collections.Users.UpdateOne(r.Context(),
		bson.M{"_id": user.ID},
		bson.M{"$set": bson.M{
			"password_hash": hash,
			"token_version": user.TokenVersion + 1,
		}},
	)
	if err != nil {
		log.Println("error updating password:", err)
		http.Redirect(w, r, "/profile/password?error=server+error", http.StatusSeeOther)
		return
	}

	// Issue new token for the current session
	user.PasswordHash = hash
	user.TokenVersion++
	token, err := auth.SignToken(user, h.cfg.JWTSecret, h.cfg.JWTExpiryHours)
	if err != nil {
		auth.ClearAuthCookie(w)
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	auth.SetAuthCookie(w, token, h.cfg.JWTExpiryHours)

	// Store last changed time (not critical — ignore error)
	db.Collections.Users.UpdateOne(r.Context(), bson.M{"_id": user.ID}, bson.M{
		"$set": bson.M{"password_changed_at": time.Now()},
	})

	http.Redirect(w, r, "/profile/password?success=password+changed", http.StatusSeeOther)
}
