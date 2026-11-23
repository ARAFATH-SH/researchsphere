package httpx

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"net/http"
	"strings"
	"time"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/api/gen"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/platform/accesstoken"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/platform/authz"
)

// Cookies set at login (docs/openapi.yaml, loginUser).
const (
	AccessCookie  = "rs_access"
	RefreshCookie = "rs_refresh"
)

// CodeUnauthenticated is the error code for a request without a valid access token.
const CodeUnauthenticated = "unauthenticated"

// TokenVerifier checks an access token and returns its claims: the user ID it was
// issued to, that user's platform role and whether their email address is verified.
// accesstoken.Signer implements it.
type TokenVerifier interface {
	Verify(token string, now time.Time) (accesstoken.Claims, error)
}

type (
	userIDKey      struct{}
	roleKey        struct{}
	verifiedKey    struct{}
	tokenExpiryKey struct{}
	requestKey     struct{}
	serviceKey     struct{}
)

// WithTokenExpiry returns a context that carries when the caller's access token expires.
func WithTokenExpiry(ctx context.Context, at time.Time) context.Context {
	return context.WithValue(ctx, tokenExpiryKey{}, at)
}

// TokenExpiry returns when the caller's access token expires, which RequireAuth stored in the
// context.
func TokenExpiry(ctx context.Context) (time.Time, bool) {
	at, ok := ctx.Value(tokenExpiryKey{}).(time.Time)
	return at, ok
}

// WithRequest returns a context that carries the HTTP request. The router puts it there for
// the one operation that needs more than the generated request object: a WebSocket upgrade
// reads the headers.
func WithRequest(ctx context.Context, r *http.Request) context.Context {
	return context.WithValue(ctx, requestKey{}, r)
}

// Request returns the HTTP request WithRequest stored.
func Request(ctx context.Context) (*http.Request, bool) {
	r, ok := ctx.Value(requestKey{}).(*http.Request)
	return r, ok
}

// WithUserID returns a context that carries the logged-in user's ID.
func WithUserID(ctx context.Context, userID string) context.Context {
	return context.WithValue(ctx, userIDKey{}, userID)
}

// UserID returns the logged-in user's ID, which RequireAuth stored in the context.
func UserID(ctx context.Context) (string, bool) {
	id, ok := ctx.Value(userIDKey{}).(string)
	return id, ok && id != ""
}

// WithEmailVerified returns a context that carries whether the logged-in user's email
// address is verified (FR-17.3).
func WithEmailVerified(ctx context.Context, verified bool) context.Context {
	return context.WithValue(ctx, verifiedKey{}, verified)
}

// EmailVerified reports whether the logged-in user's email address was verified when the
// access token was signed. A context without the value counts as unverified (deny by
// default).
func EmailVerified(ctx context.Context) bool {
	v, _ := ctx.Value(verifiedKey{}).(bool)
	return v
}

// WithRole returns a context that carries the logged-in user's platform role.
func WithRole(ctx context.Context, role authz.Role) context.Context {
	return context.WithValue(ctx, roleKey{}, role)
}

// Role returns the logged-in user's platform role, which RequireAuth took from the
// signed access token. It never comes from the request body or a header.
func Role(ctx context.Context) (authz.Role, bool) {
	role, ok := ctx.Value(roleKey{}).(authz.Role)
	return role, ok && role != ""
}

// RequireAuth checks the access cookie on every operation that the contract secures
// (FR-3.1). The generated wrapper marks those operations by putting gen.CookieAuthScopes
// in the context, so the middleware must run inside it (StdHTTPServerOptions.Middlewares).
// A missing, malformed or expired token gets 401; a valid one puts the user ID in the
// context, with the role and the email verification state the token carries. Public operations pass through untouched.
func RequireAuth(v TokenVerifier, now func() time.Time) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Context().Value(gen.CookieAuthScopes) == nil {
				// Deny by default: an operation for the sync service passes only when
				// RequireServiceToken checked its token first (docs/sync.md).
				if r.Context().Value(gen.SyncTokenScopes) != nil && !serviceVerified(r.Context()) {
					WriteError(w, http.StatusUnauthorized, CodeUnauthenticated, "A service token is required.")
					return
				}
				next.ServeHTTP(w, r)
				return
			}
			c, err := r.Cookie(AccessCookie)
			if err != nil {
				WriteError(w, http.StatusUnauthorized, CodeUnauthenticated, "Log in to continue.")
				return
			}
			claims, err := v.Verify(c.Value, now())
			if err != nil {
				WriteError(w, http.StatusUnauthorized, CodeUnauthenticated, "Your session has ended. Log in again.")
				return
			}
			ctx := WithUserID(r.Context(), claims.UserID)
			ctx = WithRole(ctx, authz.Role(claims.Role))
			ctx = WithEmailVerified(ctx, claims.Verified)
			ctx = WithTokenExpiry(ctx, claims.ExpiresAt)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// RequireServiceToken checks the bearer token of the operations the contract gives the
// `syncToken` scheme: the internal operations only the sync server calls (docs/sync.md). The
// generated wrapper marks them with gen.SyncTokenScopes; it must run outside RequireAuth,
// which refuses such an operation unless this middleware passed it. A missing or wrong token
// gets 401; the comparison takes the same time whatever the token. An empty token refuses
// every such request, so a server without SYNC_SERVICE_TOKEN serves no internal operation.
func RequireServiceToken(token []byte) func(http.Handler) http.Handler {
	want := sha256.Sum256(token)
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Context().Value(gen.SyncTokenScopes) == nil {
				next.ServeHTTP(w, r)
				return
			}
			got, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
			sum := sha256.Sum256([]byte(got))
			if !ok || len(token) == 0 || subtle.ConstantTimeCompare(sum[:], want[:]) != 1 {
				WriteError(w, http.StatusUnauthorized, CodeUnauthenticated, "A service token is required.")
				return
			}
			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), serviceKey{}, true)))
		})
	}
}

func serviceVerified(ctx context.Context) bool {
	v, _ := ctx.Value(serviceKey{}).(bool)
	return v
}
