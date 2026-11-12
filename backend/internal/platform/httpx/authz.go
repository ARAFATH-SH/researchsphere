package httpx

import (
	"net/http"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/platform/authz"
)

// CodeForbidden is the error code for an action the user's role does not permit
// (FR-3.3).
const CodeForbidden = "forbidden"

// forbiddenMessage is the same for every refusal: it says nothing about the resource
// or about why the request was refused (FR-3.3).
const forbiddenMessage = "You do not have permission to do this."

// WriteForbidden writes the one answer to a refused action: 403 with
// {"code":"forbidden","message":"You do not have permission to do this."} and nothing
// else (FR-3.3). Handlers that check authz.CanInProject use it too.
func WriteForbidden(w http.ResponseWriter) {
	WriteError(w, http.StatusForbidden, CodeForbidden, forbiddenMessage)
}

// CodeEmailNotVerified is the error code for an operation that needs a verified email
// address, asked for by a session whose address is not verified (FR-17.3).
const CodeEmailNotVerified = "email_not_verified"

// WriteEmailNotVerified writes the one answer to an operation an unverified session may
// not use: 403 with {"code":"email_not_verified","message":"Verify your email address to
// do this."} (FR-17.3).
func WriteEmailNotVerified(w http.ResponseWriter) {
	WriteError(w, http.StatusForbidden, CodeEmailNotVerified, "Verify your email address to do this.")
}

// RequirePlatform lets a request through only when the user's platform role permits
// action (FR-3.2, SRS 2.3). The role is the one RequireAuth took from the signed access
// token, so it must run inside RequireAuth. A request without a role in its context is
// refused like any other.
func RequirePlatform(action authz.Action) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			role, _ := Role(r.Context())
			if !authz.Can(role, action) {
				WriteForbidden(w)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
