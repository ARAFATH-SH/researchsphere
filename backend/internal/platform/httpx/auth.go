package httpx

import (
	"context"
	"net/http"
	"time"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/api/gen"
)

// Cookies set at login (docs/openapi.yaml, loginUser).
const (
	AccessCookie  = "rs_access"
	RefreshCookie = "rs_refresh"
)

// CodeUnauthenticated is the error code for a request without a valid access token.
const CodeUnauthenticated = "unauthenticated"

// TokenVerifier checks an access token and returns the user ID it was issued to.
// accesstoken.Signer implements it.
type TokenVerifier interface {
	Verify(token string, now time.Time) (string, error)
}

type userIDKey struct{}

// WithUserID returns a context that carries the logged-in user's ID.
func WithUserID(ctx context.Context, userID string) context.Context {
	return context.WithValue(ctx, userIDKey{}, userID)
}

// UserID returns the logged-in user's ID, which RequireAuth stored in the context.
func UserID(ctx context.Context) (string, bool) {
	id, ok := ctx.Value(userIDKey{}).(string)
	return id, ok && id != ""
}

// RequireAuth checks the access cookie on every operation that the contract secures
// (FR-3.1). The generated wrapper marks those operations by putting gen.CookieAuthScopes
// in the context, so the middleware must run inside it (StdHTTPServerOptions.Middlewares).
// A missing, malformed or expired token gets 401; a valid one puts the user ID in the
// context. Public operations pass through untouched.
func RequireAuth(v TokenVerifier, now func() time.Time) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Context().Value(gen.CookieAuthScopes) == nil {
				next.ServeHTTP(w, r)
				return
			}
			c, err := r.Cookie(AccessCookie)
			if err != nil {
				WriteError(w, http.StatusUnauthorized, CodeUnauthenticated, "Log in to continue.")
				return
			}
			userID, err := v.Verify(c.Value, now())
			if err != nil {
				WriteError(w, http.StatusUnauthorized, CodeUnauthenticated, "Your session has ended. Log in again.")
				return
			}
			next.ServeHTTP(w, r.WithContext(WithUserID(r.Context(), userID)))
		})
	}
}
