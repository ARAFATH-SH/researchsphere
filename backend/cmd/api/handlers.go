package main

import (
	"log/slog"
	"net/http"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/api/gen"
	authhttp "github.com/ARAFATH-SH/researchsphere/backend/internal/auth/adapter/http"
	compilehttp "github.com/ARAFATH-SH/researchsphere/backend/internal/compile/adapter/http"
	compileusecase "github.com/ARAFATH-SH/researchsphere/backend/internal/compile/usecase"
	embeddinghttp "github.com/ARAFATH-SH/researchsphere/backend/internal/embedding/adapter/http"
	healthhttp "github.com/ARAFATH-SH/researchsphere/backend/internal/health/adapter/http"
	healthdomain "github.com/ARAFATH-SH/researchsphere/backend/internal/health/domain"
	healthusecase "github.com/ARAFATH-SH/researchsphere/backend/internal/health/usecase"
	mediahttp "github.com/ARAFATH-SH/researchsphere/backend/internal/media/adapter/http"
	mediadomain "github.com/ARAFATH-SH/researchsphere/backend/internal/media/domain"
	mediausecase "github.com/ARAFATH-SH/researchsphere/backend/internal/media/usecase"
	messaginghttp "github.com/ARAFATH-SH/researchsphere/backend/internal/messaging/adapter/http"
	moderationhttp "github.com/ARAFATH-SH/researchsphere/backend/internal/moderation/adapter/http"
	notificationhttp "github.com/ARAFATH-SH/researchsphere/backend/internal/notification/adapter/http"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/platform/authz"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/platform/httpserver"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/platform/httpx"
	profilehttp "github.com/ARAFATH-SH/researchsphere/backend/internal/profile/adapter/http"
	projecthttp "github.com/ARAFATH-SH/researchsphere/backend/internal/project/adapter/http"
	projectusecase "github.com/ARAFATH-SH/researchsphere/backend/internal/project/usecase"
	realtimehttp "github.com/ARAFATH-SH/researchsphere/backend/internal/realtime/adapter/http"
	recommendationhttp "github.com/ARAFATH-SH/researchsphere/backend/internal/recommendation/adapter/http"
	socialhttp "github.com/ARAFATH-SH/researchsphere/backend/internal/social/adapter/http"
)

// Every module's adapter type is called Handler; the aliases give the embedded fields
// different names.
type (
	healthHandler         = healthhttp.Handler
	authHandler           = authhttp.Handler
	profileHandler        = profilehttp.Handler
	mediaHandler          = mediahttp.Handler
	socialHandler         = socialhttp.Handler
	notificationHandler   = notificationhttp.Handler
	messagingHandler      = messaginghttp.Handler
	moderationHandler     = moderationhttp.Handler
	realtimeHandler       = realtimehttp.Handler
	embeddingHandler      = embeddinghttp.Handler
	recommendationHandler = recommendationhttp.Handler
	projectHandler        = projecthttp.Handler
	compileHandler        = compilehttp.Handler
)

// handlers implements every operation in the contract by embedding each module's HTTP
// adapter. The assertion below fails to compile when an operation has no implementation.
type handlers struct {
	*healthHandler
	*authHandler
	*profileHandler
	*mediaHandler
	*socialHandler
	*notificationHandler
	*messagingHandler
	*moderationHandler
	*realtimeHandler
	*embeddingHandler
	*recommendationHandler
	*projectHandler
	*compileHandler
}

var _ gen.StrictServerInterface = handlers{}

// platformGuards names the platform action (SRS 2.3, FR-3.2) each route needs beyond a
// session, by the route's pattern. A route not listed needs only a session; every route
// under /api/v1/admin/ must be listed (routes_test.go).
var platformGuards = map[string]authz.Action{
	"GET /api/v1/admin/users": authz.SuspendUser, // listing users comes before suspending one (FR-15.3)

	// The AI matching model's embeddings (AI-1.6).
	"GET /api/v1/admin/ai/embeddings":          authz.ManageAI,
	"POST /api/v1/admin/ai/embeddings/rebuild": authz.ManageAI,

	// Moderation (FR-15.3, FR-15.5 to FR-15.9). Warnings are the user's own, so reading and
	// acknowledging one needs what reading the app needs.
	"POST /api/v1/admin/users/{userId}/warnings":           authz.ModerateUser,
	"POST /api/v1/admin/users/{userId}/restrictions":       authz.ModerateUser,
	"POST /api/v1/admin/restrictions/{restrictionId}/lift": authz.ModerateUser,
	"POST /api/v1/admin/users/{userId}/suspension":         authz.SuspendUser,
	"POST /api/v1/admin/users/{userId}/restore":            authz.SuspendUser,
	"GET /api/v1/admin/users/{userId}/moderation":          authz.ModerateUser,
	"GET /api/v1/me/warnings/pending":                      authz.ViewContent,
	"GET /api/v1/me/warnings/{warningId}":                  authz.ViewContent,
	"POST /api/v1/me/warnings/{warningId}/acknowledge":     authz.ViewContent,
	// Account settings (FR-16.11) stay open during a restriction, so a user can always stop
	// emails. The digest's unsubscribe is public (no session) and so has no guard.
	"GET /api/v1/me/settings":   authz.ViewContent,
	"PATCH /api/v1/me/settings": authz.ViewContent,

	// Reported conversations (FR-16.9, NFR-13): the only way an administrator reads messages.
	"GET /api/v1/admin/message-reports":                                       authz.ReviewMessageReports,
	"GET /api/v1/admin/message-reports/{reportId}":                            authz.ReviewMessageReports,
	"GET /api/v1/admin/message-reports/{reportId}/attachments/{attachmentId}": authz.ReviewMessageReports,
	"POST /api/v1/admin/message-reports/{reportId}/resolve":                   authz.ReviewMessageReports,

	// Reported projects (FR-15.11): reading and resolving; an administrator never edits or
	// deletes a project.
	"GET /api/v1/admin/project-reports":                     authz.ReviewProjectReports,
	"GET /api/v1/admin/project-reports/{reportId}":          authz.ReviewProjectReports,
	"POST /api/v1/admin/project-reports/{reportId}/resolve": authz.ReviewProjectReports,

	// Profiles (FR-4.1, FR-4.5): reading any profile, the caller's own included, is the
	// row "View profiles, ..."; editing is the row "Edit own profile, ...".
	"GET /api/v1/profiles/me":       authz.ViewContent,
	"PUT /api/v1/profiles/me":       authz.ActAsMember,
	"GET /api/v1/profiles/{userId}": authz.ViewContent,

	// Interests and skills (FR-4.2) are part of the own profile; suggestions (FR-4.3)
	// read the shared tags.
	"POST /api/v1/profiles/me/interests":           authz.ActAsMember,
	"DELETE /api/v1/profiles/me/interests/{tagId}": authz.ActAsMember,
	"POST /api/v1/profiles/me/skills":              authz.ActAsMember,
	"DELETE /api/v1/profiles/me/skills/{tagId}":    authz.ActAsMember,
	"GET /api/v1/tags/interests":                   authz.ViewContent,
	"GET /api/v1/tags/skills":                      authz.ViewContent,

	// Education entries (FR-4.4) are part of the user's own profile.
	"POST /api/v1/profiles/me/education":                 authz.ActAsMember,
	"PATCH /api/v1/profiles/me/education/{educationId}":  authz.ActAsMember,
	"DELETE /api/v1/profiles/me/education/{educationId}": authz.ActAsMember,

	// Work experience entries (FR-4.8) are part of the user's own profile.
	"POST /api/v1/profiles/me/experience":                  authz.ActAsMember,
	"PATCH /api/v1/profiles/me/experience/{experienceId}":  authz.ActAsMember,
	"DELETE /api/v1/profiles/me/experience/{experienceId}": authz.ActAsMember,

	// Languages (FR-4.9) are part of the user's own profile.
	"POST /api/v1/profiles/me/languages":                authz.ActAsMember,
	"PATCH /api/v1/profiles/me/languages/{languageId}":  authz.ActAsMember,
	"DELETE /api/v1/profiles/me/languages/{languageId}": authz.ActAsMember,

	// Publications (FR-5.1, FR-5.5, FR-5.7) are part of the user's own profile; reading
	// anyone's list, the caller's own included, is viewing content, like the profile.
	"GET /api/v1/profiles/me/publications":                    authz.ViewContent,
	"POST /api/v1/profiles/me/publications":                   authz.ActAsMember,
	"PATCH /api/v1/profiles/me/publications/{publicationId}":  authz.ActAsMember,
	"DELETE /api/v1/profiles/me/publications/{publicationId}": authz.ActAsMember,
	"GET /api/v1/profiles/{userId}/publications":              authz.ViewContent,
	// Adding from a DOI (FR-5.2) edits the own profile; the lookup is its first step and
	// spends the user's lookups, so it needs the same action.
	"POST /api/v1/profiles/me/publications/from-doi": authz.ActAsMember,
	"GET /api/v1/publications/lookup":                authz.ActAsMember,
	// The ORCID iD (FR-5.3) is part of the own profile; listing its works spends the
	// user's ORCID calls and is the first step of importing them, so all three need the
	// same action.
	"PUT /api/v1/profiles/me/orcid":                    authz.ActAsMember,
	"GET /api/v1/profiles/me/publications/orcid-works": authz.ActAsMember,
	"POST /api/v1/profiles/me/publications/from-orcid": authz.ActAsMember,

	// Contact info and links (FR-4.7) are part of the user's own profile.
	"GET /api/v1/profiles/me/contact":           authz.ViewContent,
	"PUT /api/v1/profiles/me/contact":           authz.ActAsMember,
	"POST /api/v1/profiles/me/links":            authz.ActAsMember,
	"DELETE /api/v1/profiles/me/links/{linkId}": authz.ActAsMember,

	// The profile photo and cover (FR-4.6) are part of the user's own profile; reading a
	// stored file is viewing content, and the usecase decides whose files the caller sees.
	"PUT /api/v1/profiles/me/photo":    authz.ActAsMember,
	"DELETE /api/v1/profiles/me/photo": authz.ActAsMember,
	"PUT /api/v1/profiles/me/cover":    authz.ActAsMember,
	"DELETE /api/v1/profiles/me/cover": authz.ActAsMember,
	"GET /api/v1/media/{fileId}":       authz.ViewContent,

	// Researcher search (FR-6) is the row "View profiles, search, ...".
	"GET /api/v1/search/people": authz.ViewContent,
	// Collaborator recommendations (AI-2) are the same row ("... and recommendations").
	"GET /api/v1/recommendations/people": authz.ViewContent,
	// Project matching (AI-3.1) is the same row. A project's recommended researchers
	// (AI-2.3) too; the usecase then checks the project role (owner only).
	"GET /api/v1/recommendations/projects":                     authz.ViewContent,
	"GET /api/v1/projects/{projectId}/recommended-researchers": authz.ViewContent,

	// Research projects (FR-10). Creating is the row "Create a project". Reading, editing and
	// deleting need what reading the app needs; the usecase then checks the project role
	// (SRS 2.3, project level), so an administrator gets no extra right inside a project.
	// Reporting one is the row "Edit own profile, connect, ...", like reporting a
	// conversation. Project search (FR-10.3) is the row "View profiles, search, ...".
	"POST /api/v1/projects":                     authz.CreateProject,
	"GET /api/v1/projects/{projectId}":          authz.ViewContent,
	"PATCH /api/v1/projects/{projectId}":        authz.ViewContent,
	"DELETE /api/v1/projects/{projectId}":       authz.ViewContent,
	"POST /api/v1/projects/{projectId}/reports": authz.ActAsMember,
	"GET /api/v1/me/projects":                   authz.ViewContent,
	"GET /api/v1/search/projects":               authz.ViewContent,

	// Project membership (FR-11, FR-10.5). Asking, inviting, answering, deciding, changing
	// roles, removing, leaving and transferring are the row "Edit own profile, connect, ...",
	// like connections; the lists are viewing content. The usecase then checks the project
	// action ("Request to join", "Invite, decide join requests, ...", "Transfer ownership,
	// ...") or, for answering an invitation and leaving, that the caller is its subject.
	"POST /api/v1/projects/{projectId}/join-requests":                     authz.ActAsMember,
	"GET /api/v1/projects/{projectId}/join-requests":                      authz.ViewContent,
	"DELETE /api/v1/projects/{projectId}/join-requests/{requestId}":       authz.ActAsMember,
	"POST /api/v1/projects/{projectId}/join-requests/{requestId}/accept":  authz.ActAsMember,
	"POST /api/v1/projects/{projectId}/join-requests/{requestId}/reject":  authz.ActAsMember,
	"POST /api/v1/projects/{projectId}/invitations":                       authz.ActAsMember,
	"GET /api/v1/projects/{projectId}/invitations":                        authz.ViewContent,
	"DELETE /api/v1/projects/{projectId}/invitations/{invitationId}":      authz.ActAsMember,
	"POST /api/v1/projects/{projectId}/invitations/{invitationId}/accept": authz.ActAsMember,
	"POST /api/v1/projects/{projectId}/invitations/{invitationId}/reject": authz.ActAsMember,
	"GET /api/v1/projects/{projectId}/members":                            authz.ViewContent,
	"PATCH /api/v1/projects/{projectId}/members/{userId}":                 authz.ActAsMember,
	"DELETE /api/v1/projects/{projectId}/members/{userId}":                authz.ActAsMember,
	"POST /api/v1/projects/{projectId}/leave":                             authz.ActAsMember,
	"POST /api/v1/projects/{projectId}/transfer":                          authz.ActAsMember,
	// Project files (FR-12.1, FR-12.2, FR-12.6): the usecase then checks the project role
	// (members only; editors and the owner write).
	"GET /api/v1/projects/{projectId}/files":                              authz.ViewContent,
	"POST /api/v1/projects/{projectId}/files":                             authz.ActAsMember,
	"POST /api/v1/projects/{projectId}/files/uploads":                     authz.ActAsMember,
	"POST /api/v1/projects/{projectId}/files/uploads/{uploadId}/complete": authz.ActAsMember,
	"GET /api/v1/projects/{projectId}/files/{fileId}":                     authz.ViewContent,
	"PATCH /api/v1/projects/{projectId}/files/{fileId}":                   authz.ActAsMember,
	"DELETE /api/v1/projects/{projectId}/files/{fileId}":                  authz.ActAsMember,
	"PUT /api/v1/projects/{projectId}/files/{fileId}/content":             authz.ActAsMember,
	// Real-time editing (slice 22): the ticket only opens what the role already allows; the
	// sync server makes a viewer read-only (FR-12.7).
	"POST /api/v1/projects/{projectId}/files/{fileId}/collab-ticket": authz.ViewContent,
	// Compile (FR-13): starting one is writing, reading results and the PDF viewing; the
	// usecase then checks the project row ("Compile, ..." or "Open the workspace, ...").
	// The archive (FR-13.7) is reading files.
	"POST /api/v1/projects/{projectId}/compiles":                authz.ActAsMember,
	"GET /api/v1/projects/{projectId}/compiles/latest":          authz.ViewContent,
	"GET /api/v1/projects/{projectId}/compiles/{compileId}":     authz.ViewContent,
	"GET /api/v1/projects/{projectId}/compiles/{compileId}/log": authz.ViewContent,
	"GET /api/v1/projects/{projectId}/pdf":                      authz.ViewContent,
	"GET /api/v1/projects/{projectId}/archive":                  authz.ViewContent,
	"GET /api/v1/me/join-requests":                              authz.ViewContent,
	"GET /api/v1/me/invitations":                                authz.ViewContent,

	// Connections (FR-7): sending and answering requests and removing a connection are
	// the row "Edit own profile, connect, ..."; the lists are viewing content.
	"POST /api/v1/connections/requests":                    authz.ActAsMember,
	"POST /api/v1/connections/requests/{requestId}/accept": authz.ActAsMember,
	"POST /api/v1/connections/requests/{requestId}/reject": authz.ActAsMember,
	"DELETE /api/v1/connections/requests/{requestId}":      authz.ActAsMember,
	"DELETE /api/v1/connections/{userId}":                  authz.ActAsMember,
	"GET /api/v1/connections":                              authz.ViewContent,
	"GET /api/v1/connections/requests/received":            authz.ViewContent,
	"GET /api/v1/connections/requests/sent":                authz.ViewContent,
	"GET /api/v1/profiles/{userId}/connections":            authz.ViewContent,

	// Notifications (FR-9.4 to FR-9.6): SRS 2.3 has no row of their own; reading and
	// clearing one's own notifications is closest to "View profiles, ...". The usecase
	// shows each user only their own. No rate limit: the client polls the count.
	"GET /api/v1/notifications":                        authz.ViewContent,
	"GET /api/v1/notifications/unread-count":           authz.ViewContent,
	"POST /api/v1/notifications/{notificationId}/read": authz.ViewContent,
	"POST /api/v1/notifications/read-all":              authz.ViewContent,

	// Messaging (FR-16): SRS 2.3 has no row for it. Reading one's own conversations is
	// closest to "View ...", writing to "Edit own profile, connect, ...". Membership is
	// checked on every operation by the usecase (NFR-13); no role gets more access on these
	// routes. An administrator reads a reported conversation through /admin/message-reports.
	"POST /api/v1/messages/direct":                                       authz.ActAsMember,
	"GET /api/v1/messages/unread-count":                                  authz.ViewContent,
	"GET /api/v1/messages/search":                                        authz.ViewContent,
	"GET /api/v1/conversations":                                          authz.ViewContent,
	"GET /api/v1/conversations/{conversationId}":                         authz.ViewContent,
	"GET /api/v1/conversations/{conversationId}/messages":                authz.ViewContent,
	"POST /api/v1/conversations/{conversationId}/messages":               authz.ActAsMember,
	"POST /api/v1/conversations/{conversationId}/attachments":            authz.ActAsMember,
	"GET /api/v1/attachments/{attachmentId}":                             authz.ViewContent,
	"POST /api/v1/conversations/{conversationId}/read":                   authz.ActAsMember,
	"PATCH /api/v1/conversations/{conversationId}/messages/{messageId}":  authz.ActAsMember,
	"DELETE /api/v1/conversations/{conversationId}/messages/{messageId}": authz.ActAsMember,
	"POST /api/v1/conversations/{conversationId}/accept":                 authz.ActAsMember,
	"POST /api/v1/conversations/{conversationId}/decline":                authz.ActAsMember,
	"POST /api/v1/conversations/{conversationId}/reports":                authz.ActAsMember,
	// Groups (FR-16.6): writing, as for a message. Ownership is checked by the usecase.
	"POST /api/v1/conversations/groups":                              authz.ActAsMember,
	"PATCH /api/v1/conversations/{conversationId}":                   authz.ActAsMember,
	"POST /api/v1/conversations/{conversationId}/members":            authz.ActAsMember,
	"DELETE /api/v1/conversations/{conversationId}/members/{userId}": authz.ActAsMember,
	"POST /api/v1/conversations/{conversationId}/leave":              authz.ActAsMember,

	// Blocks (FR-16.9): SRS 2.3 has no row for them. Blocking is closest to "Edit own
	// profile, connect, ...", and reading one's own list to "View ...".
	"POST /api/v1/blocks":            authz.ActAsMember,
	"DELETE /api/v1/blocks/{userId}": authz.ActAsMember,
	"GET /api/v1/blocks":             authz.ViewContent,

	// Real-time (FR-16.1, FR-16.4): the socket only pushes the user's own events, and typing
	// and presence follow the rules of the usecase, so both are the row "View ...". Writing
	// stays on the REST routes above.
	"GET /api/v1/ws":       authz.ViewContent,
	"GET /api/v1/presence": authz.ViewContent,
}

// unverifiedAllowed lists the routes a session whose email address is not verified may use
// (FR-17.3); every other route that needs a session answers 403 email_not_verified, before
// the role is checked. Deny by default: a new route needs a verified email until it is
// added here. No route under /api/v1/admin/ may be listed (routes_test.go). The reasons for
// the grey areas are in docs/decisions.md (2026-11-12).
var unverifiedAllowed = map[string]bool{
	// Account settings: who am I, change password, delete account.
	"GET /api/v1/auth/me":         true,
	"POST /api/v1/auth/password":  true,
	"POST /api/v1/account/delete": true,

	// Read and edit the own profile, photo and cover included, and the tag suggestions the
	// interest and skill fields use.
	"GET /api/v1/profiles/me":                              true,
	"PUT /api/v1/profiles/me":                              true,
	"POST /api/v1/profiles/me/interests":                   true,
	"DELETE /api/v1/profiles/me/interests/{tagId}":         true,
	"POST /api/v1/profiles/me/skills":                      true,
	"DELETE /api/v1/profiles/me/skills/{tagId}":            true,
	"GET /api/v1/tags/interests":                           true,
	"GET /api/v1/tags/skills":                              true,
	"POST /api/v1/profiles/me/education":                   true,
	"PATCH /api/v1/profiles/me/education/{educationId}":    true,
	"DELETE /api/v1/profiles/me/education/{educationId}":   true,
	"POST /api/v1/profiles/me/experience":                  true,
	"PATCH /api/v1/profiles/me/experience/{experienceId}":  true,
	"DELETE /api/v1/profiles/me/experience/{experienceId}": true,
	"POST /api/v1/profiles/me/languages":                   true,
	"PATCH /api/v1/profiles/me/languages/{languageId}":     true,
	"DELETE /api/v1/profiles/me/languages/{languageId}":    true,
	"GET /api/v1/profiles/me/contact":                      true,
	"PUT /api/v1/profiles/me/contact":                      true,
	"POST /api/v1/profiles/me/links":                       true,
	"DELETE /api/v1/profiles/me/links/{linkId}":            true,
	"PUT /api/v1/profiles/me/orcid":                        true,
	"PUT /api/v1/profiles/me/photo":                        true,
	"DELETE /api/v1/profiles/me/photo":                     true,
	"PUT /api/v1/profiles/me/cover":                        true,
	"DELETE /api/v1/profiles/me/cover":                     true,
	// Publications entered by hand. The DOI lookup and the ORCID works list and import call
	// outside services on the user's quota, so they need a verified email.
	"GET /api/v1/profiles/me/publications":                    true,
	"POST /api/v1/profiles/me/publications":                   true,
	"PATCH /api/v1/profiles/me/publications/{publicationId}":  true,
	"DELETE /api/v1/profiles/me/publications/{publicationId}": true,

	// The user's own notifications, marking them read included.
	"GET /api/v1/notifications":                        true,
	"GET /api/v1/notifications/unread-count":           true,
	"POST /api/v1/notifications/{notificationId}/read": true,
	"POST /api/v1/notifications/read-all":              true,

	// The user's own warnings: a notice about their account, fetched on every page load.
	"GET /api/v1/me/warnings/pending":                  true,
	"GET /api/v1/me/warnings/{warningId}":              true,
	"POST /api/v1/me/warnings/{warningId}/acknowledge": true,
}

// multipartOverhead is what a multipart body may hold besides the file: boundaries, part
// headers and small fields.
const multipartOverhead = 64 << 10

// bodyLimits caps the request body of the upload routes, by route pattern, while it is
// read (FR-4.6): a larger body fails with http.MaxBytesError, which the media handler
// answers with 413. The usecase also stops reading the file part at its exact limit.
var bodyLimits = map[string]int64{
	"PUT /api/v1/profiles/me/photo": mediausecase.Specs[mediadomain.PurposePhoto].MaxBytes + multipartOverhead,
	"PUT /api/v1/profiles/me/cover": mediausecase.Specs[mediadomain.PurposeCover].MaxBytes + multipartOverhead,
	// FR-16.7: 25 MB.
	"POST /api/v1/conversations/{conversationId}/attachments": mediausecase.MaxAttachmentBytes + multipartOverhead,
	// FR-12.2: only the path and size; the figure goes straight to storage.
	"POST /api/v1/projects/{projectId}/files/uploads": 4 << 10,
	// FR-12.1, FR-12.6: text of 1 MiB, which JSON escaping can make up to 3 times longer.
	"POST /api/v1/projects/{projectId}/files":                 maxFileJSONBody,
	"PUT /api/v1/projects/{projectId}/files/{fileId}/content": maxFileJSONBody,
	// The sync server's internal operations (slice 22): a store carries the text and up to
	// 8 MiB of Yjs state in base64; the others are small.
	"PUT /api/v1/internal/sync/files/{fileId}/state": maxFileJSONBody + 12<<20,
	"POST /api/v1/internal/sync/tickets/redeem":      4 << 10,
	"POST /api/v1/internal/sync/access":              64 << 10,
	// The compile worker's (slice 23): the PDF is checked against COMPILE_MAX_PDF_BYTES as it
	// is read; the result carries the logs, which JSON escaping can make 6 times longer.
	"PUT /api/v1/internal/compile/jobs/{compileId}/pdf":     compileusecase.MaxPDFCeiling + 1,
	"POST /api/v1/internal/compile/jobs/{compileId}/finish": 6*(compileusecase.MaxLogBytes+compileusecase.MaxBibtexLogBytes) + multipartOverhead,
	"POST /api/v1/internal/compile/claim":                   4 << 10,
}

// maxFileJSONBody caps a JSON body that carries a text file: 3 MiB and some room for the
// other fields. Larger bodies get 413 request_too_large.
const maxFileJSONBody = 3*projectusecase.MaxTextBytes + multipartOverhead

// limitBody wraps the request body of the routes in bodyLimits in http.MaxBytesReader.
// It runs after the session and permission checks, so no body is read for a request
// that is refused.
func limitBody(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if n, ok := bodyLimits[r.Pattern]; ok {
			r.Body = http.MaxBytesReader(w, r.Body, n)
		}
		next.ServeHTTP(w, r)
	})
}

// guardPlatform is the one permission check of every route. A session whose email address
// is not verified may use only the routes in unverifiedAllowed and gets 403
// email_not_verified on any other (FR-17.3). Then httpx.RequirePlatform checks the action
// platformGuards gives the matched route. It runs inside RequireAuth, so a request without
// a session gets 401 before any 403, and a public route (no session in the context) passes
// to it untouched. A request that matched no route pattern is refused: every API route is
// registered on the ServeMux, so that would mean the guard cannot tell which route it is
// guarding.
func guardPlatform(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Pattern == "" {
			httpx.WriteForbidden(w)
			return
		}
		if _, signedIn := httpx.UserID(r.Context()); signedIn && !httpx.EmailVerified(r.Context()) && !unverifiedAllowed[r.Pattern] {
			httpx.WriteEmailNotVerified(w)
			return
		}
		action, ok := platformGuards[r.Pattern]
		if !ok {
			next.ServeHTTP(w, r)
			return
		}
		httpx.RequirePlatform(action)(next).ServeHTTP(w, r)
	})
}

// newRouter wires the modules to their dependencies and returns the API's root handler.
// Tests pass fakes; run passes the real PostgreSQL, Redis, SMTP and AI service adapters. requireAuth
// guards the operations the contract secures (httpx.RequireAuth); guardPlatform runs
// inside it, and limitBody inside that.
func newRouter(logger *slog.Logger, db, redis, storage, ai healthdomain.Pinger, auth *authhttp.Handler, profile *profilehttp.Handler, media *mediahttp.Handler, social *socialhttp.Handler, notifications *notificationhttp.Handler, messaging *messaginghttp.Handler, moderation *moderationhttp.Handler, realtime *realtimehttp.Handler, embedding *embeddinghttp.Handler, recommendation *recommendationhttp.Handler, projects *projecthttp.Handler, compile *compilehttp.Handler, requireAuth func(http.Handler) http.Handler) http.Handler {
	checker := healthusecase.New(healthusecase.DefaultTimeout,
		healthdomain.Dependency{Name: "database", Pinger: db},
		healthdomain.Dependency{Name: "redis", Pinger: redis},
		healthdomain.Dependency{Name: "storage", Pinger: storage},
		// Optional (NFR-10.2): without it the API is degraded, not down.
		healthdomain.Dependency{Name: healthhttp.AIDependency, Pinger: ai, Optional: true},
	)
	return httpserver.NewRouter(logger, handlers{
		healthHandler:         healthhttp.NewHandler(checker, logger),
		authHandler:           auth,
		profileHandler:        profile,
		mediaHandler:          media,
		socialHandler:         social,
		notificationHandler:   notifications,
		messagingHandler:      messaging,
		moderationHandler:     moderation,
		realtimeHandler:       realtime,
		embeddingHandler:      embedding,
		recommendationHandler: recommendation,
		projectHandler:        projects,
		compileHandler:        compile,
	}, func(next http.Handler) http.Handler { return requireAuth(guardPlatform(limitBody(next))) })
}
