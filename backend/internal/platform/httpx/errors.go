// Package httpx holds the HTTP plumbing shared by every module: the error shape,
// request IDs, access logs, panic recovery and JSON replies for unknown routes.
// Module adapters use WriteError and the error codes.
package httpx

import (
	"encoding/json"
	"net/http"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/api/gen"
)

// Error codes used by the plumbing. Modules add their own.
const (
	CodeBadRequest         = "bad_request"
	CodeNotFound           = "not_found"
	CodeMethodNotAllowed   = "method_not_allowed"
	CodeInternal           = "internal_error"
	CodeServiceUnavailable = "service_unavailable"
)

// WriteError writes the error shape from SRS 5.3: {"code": "...", "message": "..."}.
func WriteError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(status)
	// The status line is already sent; a failed write cannot be reported to the client.
	_ = json.NewEncoder(w).Encode(gen.Error{Code: code, Message: message})
}
