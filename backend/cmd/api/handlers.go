package main

import (
	"log/slog"
	"net/http"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/api/gen"
	authhttp "github.com/ARAFATH-SH/researchsphere/backend/internal/auth/adapter/http"
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
	realtimehttp "github.com/ARAFATH-SH/researchsphere/backend/internal/realtime/adapter/http"
	socialhttp "github.com/ARAFATH-SH/researchsphere/backend/internal/social/adapter/http"
)

// Every module's adapter type is called Handler; the aliases give the embedded fields
// different names.
type (
	healthHandler       = healthhttp.Handler
	authHandler         = authhttp.Handler
	profileHandler      = profilehttp.Handler
	mediaHandler        = mediahttp.Handler
	socialHandler       = socialhttp.Handler
	notificationHandler = notificationhttp.Handler
	messagingHandler    = messaginghttp.Handler
	moderationHandler   = moderationhttp.Handler
	realtimeHandler     = realtimehttp.Handler
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
}

var _ gen.StrictServerInterface = handlers{}

// platformGuards names the platform action (SRS 2.3, FR-3.2) each route needs beyond a
// session, by the route's pattern. A route not listed needs only a session; every route
// under /api/v1/admin/ must be listed (routes_test.go).
var platformGuards = map[string]authz.Action{
	"GET /api/v1/admin/users": authz.SuspendUser, // listing users comes before suspending one (FR-15.3)

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

	// Reported conversations (FR-16.9, NFR-14): the only way an administrator reads messages.
	"GET /api/v1/admin/message-reports":                     authz.ReviewMessageReports,
	"GET /api/v1/admin/message-reports/{reportId}":          authz.ReviewMessageReports,
	"POST /api/v1/admin/message-reports/{reportId}/resolve": authz.ReviewMessageReports,

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
	// checked on every operation by the usecase (NFR-14); no role gets more access on these
	// routes. An administrator reads a reported conversation through /admin/message-reports.
	"POST /api/v1/messages/direct":                                       authz.ActAsMember,
	"GET /api/v1/messages/unread-count":                                  authz.ViewContent,
	"GET /api/v1/conversations":                                          authz.ViewContent,
	"GET /api/v1/conversations/{conversationId}":                         authz.ViewContent,
	"GET /api/v1/conversations/{conversationId}/messages":                authz.ViewContent,
	"POST /api/v1/conversations/{conversationId}/messages":               authz.ActAsMember,
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

// multipartOverhead is what a multipart body may hold besides the file: boundaries, part
// headers and small fields.
const multipartOverhead = 64 << 10

// bodyLimits caps the request body of the upload routes, by route pattern, while it is
// read (FR-4.6): a larger body fails with http.MaxBytesError, which the media handler
// answers with 413. The usecase also stops reading the file part at its exact limit.
var bodyLimits = map[string]int64{
	"PUT /api/v1/profiles/me/photo": mediausecase.Specs[mediadomain.PurposePhoto].MaxBytes + multipartOverhead,
	"PUT /api/v1/profiles/me/cover": mediausecase.Specs[mediadomain.PurposeCover].MaxBytes + multipartOverhead,
}

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

// guardPlatform runs httpx.RequirePlatform with the action platformGuards gives the
// matched route. It runs inside RequireAuth, so a request without a session gets 401
// before any 403. A request that matched no route pattern is refused: every API route
// is registered on the ServeMux, so that would mean the guard cannot tell which route
// it is guarding.
func guardPlatform(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Pattern == "" {
			httpx.WriteForbidden(w)
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
// Tests pass fakes; run passes the real PostgreSQL, Redis and SMTP adapters. requireAuth
// guards the operations the contract secures (httpx.RequireAuth); guardPlatform runs
// inside it, and limitBody inside that.
func newRouter(logger *slog.Logger, db, redis, storage healthdomain.Pinger, auth *authhttp.Handler, profile *profilehttp.Handler, media *mediahttp.Handler, social *socialhttp.Handler, notifications *notificationhttp.Handler, messaging *messaginghttp.Handler, moderation *moderationhttp.Handler, realtime *realtimehttp.Handler, requireAuth func(http.Handler) http.Handler) http.Handler {
	checker := healthusecase.New(healthusecase.DefaultTimeout,
		healthdomain.Dependency{Name: "database", Pinger: db},
		healthdomain.Dependency{Name: "redis", Pinger: redis},
		healthdomain.Dependency{Name: "storage", Pinger: storage},
	)
	return httpserver.NewRouter(logger, handlers{
		healthHandler:       healthhttp.NewHandler(checker, logger),
		authHandler:         auth,
		profileHandler:      profile,
		mediaHandler:        media,
		socialHandler:       social,
		notificationHandler: notifications,
		messagingHandler:    messaging,
		moderationHandler:   moderation,
		realtimeHandler:     realtime,
	}, func(next http.Handler) http.Handler { return requireAuth(guardPlatform(limitBody(next))) })
}
