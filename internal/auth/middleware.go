package auth

import (
	"context"
	"net/http"

	"arch_erp/internal/db"
	"arch_erp/internal/models"
)

type contextKey string

const (
	// UserContextKey is the key used to store the authenticated user in the request context.
	UserContextKey contextKey = "auth_user"
	// JWTSecretKey is the key used to pass the JWT secret through context (set at startup).
	JWTSecretKey contextKey = "jwt_secret"
)

// Middleware validates the JWT cookie and injects the authenticated user into the request context.
// Unauthenticated requests are redirected to /login.
func Middleware(jwtSecret string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			cookie, err := r.Cookie("auth_token")
			if err != nil {
				http.Redirect(w, r, "/login", http.StatusSeeOther)
				return
			}

			user, err := VerifyTokenWithDB(r.Context(), cookie.Value, jwtSecret, db.Collections.Users)
			if err != nil {
				// Clear the bad cookie and redirect
				http.SetCookie(w, &http.Cookie{
					Name:     "auth_token",
					Value:    "",
					Path:     "/",
					MaxAge:   -1,
					HttpOnly: true,
				})
				http.Redirect(w, r, "/login", http.StatusSeeOther)
				return
			}

			ctx := context.WithValue(r.Context(), UserContextKey, user)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// UserFromContext retrieves the authenticated user from the request context.
// Returns nil if no user is present (i.e. on unauthenticated routes).
func UserFromContext(ctx context.Context) *models.User {
	u, _ := ctx.Value(UserContextKey).(*models.User)
	return u
}

// RequireRole returns a middleware that enforces one or more allowed roles.
// If the user's role is not in the allowed list, a 403 page is rendered.
func RequireRole(roles ...models.Role) func(http.Handler) http.Handler {
	allowed := make(map[models.Role]bool, len(roles))
	for _, r := range roles {
		allowed[r] = true
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			user := UserFromContext(r.Context())
			if user == nil || !allowed[user.Role] {
				http.Error(w, "403 Forbidden — insufficient permissions", http.StatusForbidden)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// SetAuthCookie writes a signed JWT to an HTTP-only cookie.
func SetAuthCookie(w http.ResponseWriter, token string, expiryHours int) {
	http.SetCookie(w, &http.Cookie{
		Name:     "auth_token",
		Value:    token,
		Path:     "/",
		MaxAge:   expiryHours * 3600,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
}

// ClearAuthCookie removes the auth cookie from the client.
func ClearAuthCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     "auth_token",
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
	})
}
