package main

import (
	"context"
	"html/template"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"arch_erp/internal/auth"
	"arch_erp/internal/config"
	"arch_erp/internal/db"
	"arch_erp/internal/handlers"
	"arch_erp/internal/models"
	"arch_erp/internal/services"

	_ "github.com/joho/godotenv/autoload"
)

func main() {
	// ── Load configuration ──────────────────────────────────────
	cfg, err := config.Load()
	if err != nil {
		log.Fatal("config error:", err)
	}

	// ── Connect to MongoDB ──────────────────────────────────────
	if err := db.Connect(cfg); err != nil {
		log.Fatal("db connect:", err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		db.Disconnect(ctx)
	}()

	ctx := context.Background()
	if err := db.EnsureIndexes(ctx); err != nil {
		log.Fatal("db indexes:", err)
	}
	adminHash, err := auth.HashPassword(cfg.AdminPassword)
	if err != nil {
		log.Fatal("hashing admin password:", err)
	}
	if err := db.SeedAdmin(ctx, cfg, adminHash); err != nil {
		log.Fatal("db seed:", err)
	}

	// ── Parse HTML templates ────────────────────────────────────
	// Build a per-page template cache to avoid {{define "content"}} collisions.
	tmplCache, err := buildTemplateCache()
	if err != nil {
		log.Fatal("template parse:", err)
	}

	// ── Services ────────────────────────────────────────────────
	hub := services.NewHub()
	kanban := services.NewKanbanService(hub)
	notif := services.NewNotificationService(cfg)

	// ── Handlers ────────────────────────────────────────────────
	authH := handlers.NewAuthHandler(cfg, tmplCache)
	projectH := handlers.NewProjectHandler(tmplCache)
	taskH := handlers.NewTaskHandler(tmplCache, kanban, notif)
	adminH := handlers.NewAdminHandler(tmplCache)
	sseH := handlers.NewSSEHandler(hub, tmplCache)
	calendarH := handlers.NewCalendarHandler(tmplCache)

	// ── Router ──────────────────────────────────────────────────
	mux := http.NewServeMux()

	// Static files
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServer(http.Dir("static"))))

	// Public (no auth required)
	mux.HandleFunc("GET /login", authH.LoginPage)
	mux.HandleFunc("POST /login", authH.Login)
	mux.HandleFunc("GET /logout", authH.Logout)

	// ── Authenticated routes ─────────────────────────────────────
	authMw := auth.Middleware(cfg.JWTSecret)

	protected := http.NewServeMux()

	// Root redirect → dashboard
	protected.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
	})

	// Dashboard
	protected.HandleFunc("GET /dashboard", projectH.Dashboard)

	// Projects
	protected.HandleFunc("GET /projects", projectH.List)
	protected.HandleFunc("GET /projects/{id}", projectH.Detail)

	// Calendar
	protected.HandleFunc("GET /calendar", calendarH.CalendarView)

	// Project create (Admin + Senior Architect)
	protected.Handle("GET /projects/new",
		requireRole(http.HandlerFunc(projectH.NewPage), models.RoleAdmin, models.RoleSeniorArchitect))
	protected.Handle("POST /projects",
		requireRole(http.HandlerFunc(projectH.Create), models.RoleAdmin, models.RoleSeniorArchitect))

	// Project edit (Admin + Senior Architect)
	protected.Handle("GET /projects/{id}/edit",
		requireRole(http.HandlerFunc(projectH.EditPage), models.RoleAdmin, models.RoleSeniorArchitect))
	protected.Handle("POST /projects/{id}/update",
		requireRole(http.HandlerFunc(projectH.Update), models.RoleAdmin, models.RoleSeniorArchitect))

	// Task create (all except Intern)
	protected.Handle("POST /projects/{id}/tasks",
		requireRole(http.HandlerFunc(taskH.Create), models.RoleAdmin, models.RoleSeniorArchitect, models.RoleJuniorArchitect))

	// Tasks
	protected.HandleFunc("GET /tasks/{id}", taskH.Detail)
	protected.HandleFunc("POST /tasks/{id}/move", taskH.Move)
	protected.HandleFunc("POST /tasks/{id}/forward", taskH.Forward)
	protected.HandleFunc("POST /tasks/{id}/update", taskH.Update)
	protected.HandleFunc("POST /tasks/{id}/comment", taskH.AddComment)

	// Task delete (Admin + Senior Architect)
	protected.Handle("POST /tasks/{id}/delete",
		requireRole(http.HandlerFunc(taskH.Delete), models.RoleAdmin, models.RoleSeniorArchitect))

	// Project discussion comments
	protected.HandleFunc("POST /projects/{id}/comment", projectH.AddComment)

	// SSE stream for Kanban board
	protected.HandleFunc("GET /projects/{id}/stream", sseH.KanbanStream)

	// Admin routes (Admin only)
	adminOnly := roleMw(models.RoleAdmin)
	protected.Handle("GET /admin/users", adminOnly(http.HandlerFunc(adminH.UserList)))
	protected.Handle("POST /admin/users", adminOnly(http.HandlerFunc(adminH.CreateUser)))
	protected.Handle("POST /admin/users/{id}/update", adminOnly(http.HandlerFunc(adminH.UpdateUser)))
	protected.Handle("POST /admin/users/{id}/reset-password", adminOnly(http.HandlerFunc(adminH.ResetPassword)))
	protected.Handle("POST /admin/users/{id}/delete", adminOnly(http.HandlerFunc(adminH.DeleteUser)))

	// Profile
	protected.HandleFunc("GET /profile/password", authH.ChangePasswordPage)
	protected.HandleFunc("POST /profile/password", authH.ChangePassword)

	// Wrap all protected routes with auth middleware
	mux.Handle("/", authMw(protected))

	// ── Start server ─────────────────────────────────────────────
	addr := ":" + cfg.Port
	log.Printf("🏛 Arch ERP starting on http://localhost%s", addr)
	srv := &http.Server{
		Addr:         addr,
		Handler:      mux,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 0, // SSE streams need unlimited write time
		IdleTimeout:  120 * time.Second,
	}
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal("server error:", err)
	}
}

// templateDir returns the path to the templates directory.
func templateDir() string {
	var dir string
	_, filename, _, ok := runtime.Caller(0)
	if ok {
		dir = filepath.Join(filepath.Dir(filename), "internal", "templates")
	} else {
		dir = filepath.Join("internal", "templates")
	}
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		dir = filepath.Join("internal", "templates")
	}
	return dir
}

// buildTemplateCache creates a map of page-name → *template.Template.
//
// Each page template is parsed together with base.html and kanban_column.html
// in its own isolated template set. This prevents {{define "content"}} blocks
// from different pages overwriting each other when all files are parsed into a
// single template.Template (the classic Go template "last-one-wins" pitfall).
func buildTemplateCache() (map[string]*template.Template, error) {
	dir := templateDir()

	// Shared layout files needed by every page-specific template.
	sharedFiles := []string{
		filepath.Join(dir, "base.html"),
		filepath.Join(dir, "kanban_column.html"),
	}

	// Page entry-point files for ExecuteTemplate calls.
	// login.html is standalone (no base layout) so it is handled separately.
	pageFiles := []string{
		"dashboard.html",
		"project_list.html",
		"project_detail.html",
		"project_new.html",
		"project_edit.html",
		"task_detail.html",
		"admin_users.html",
		"change_password.html",
		"calendar.html",
	}

	cache := make(map[string]*template.Template, len(pageFiles)+1)

	for _, page := range pageFiles {
		pagePath := filepath.Join(dir, page)
		files := append([]string{pagePath}, sharedFiles...)
		t, err := template.ParseFiles(files...)
		if err != nil {
			return nil, err
		}
		cache[page] = t
	}

	// login.html is a standalone page (no base.html wrapper).
	loginPath := filepath.Join(dir, "login.html")
	loginTmpl, err := template.ParseFiles(loginPath)
	if err != nil {
		return nil, err
	}
	cache["login.html"] = loginTmpl

	return cache, nil
}

// requireRole wraps next with role-based access control and returns an http.Handler.
// Use this for inline route registration: protected.Handle("...", requireRole(handler, roles...))
func requireRole(next http.Handler, roles ...models.Role) http.Handler {
	return auth.RequireRole(roles...)(next)
}

// roleMw returns a middleware factory for role-based access control.
// Use this to build a reusable middleware: mw := roleMw(roles...); protected.Handle("...", mw(handler))
func roleMw(roles ...models.Role) func(http.Handler) http.Handler {
	return auth.RequireRole(roles...)
}
