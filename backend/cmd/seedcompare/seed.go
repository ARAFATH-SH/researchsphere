package main

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type account struct {
	email    string
	name     string
	headline string
}

// The fake accounts. Alpha and Beta are connected and share the long conversation; Gamma is
// not connected to anyone, because a message request needs a pair who are not. Admin is an
// administrator, for the admin pages, and takes part in nothing.
var (
	alpha = account{"compare-alpha@compare.researchsphere.local", "Compare Alpha", "Fake account for UI comparison"}
	beta  = account{"compare-beta@compare.researchsphere.local", "Compare Beta", "Fake account for UI comparison"}
	gamma = account{"compare-gamma@compare.researchsphere.local", "Compare Gamma", "Fake account for UI comparison"}
	admin = account{"compare-admin@compare.researchsphere.local", "Compare Admin", "Fake account for UI comparison"}
)

// The lines of the long conversation; speaker says who writes each. Two of them show what
// plain text must keep: a line break and a link.
var script = []string{
	"Did the new figure compile? I pushed the method section.",
	"Not yet, one undefined command. Fixing it now.",
	"Thanks. Can you check the references too?",
	"Will do after lunch.",
	"The survey numbers changed, by the way.",
	"By how much?",
	"About two percent on the second cohort.",
	"Then table 3 needs a new run.",
	"I can do that tonight.",
	"Great, send me the CSV when it is ready.",
	"Uploading now.",
	"Got it, thanks.",
	"Does the abstract still fit the word limit?",
	"Just about: 248 of 250.",
	"Nice. Who writes the cover letter?",
	"I can draft it tomorrow.",
	"Draft is ready for you.\nI left two questions in the margin.",
	"Reading it now.",
	"First question: do we name the funder?",
	"Yes, they require it.",
	"Second: is the deadline the 14th or the 15th?",
	"The 15th, see https://example.org/call-for-papers.",
	"Good, that gives us the weekend.",
	"Should we ask for a third reviewer?",
	"Only if the editor agrees.",
	"I will write to the editor.",
	"Thanks, copy me in please.",
	"Done.",
	"The editor said yes.",
	"That was quick.",
	"Next step is the response letter.",
	"I started one from the template.",
	"Can you share it?",
	"Sent it by email.",
	"Looks good so far.",
	"I fixed two typos in section 2.",
	"Thanks. Ready to submit on Friday?",
	"Yes, unless the figures change again.",
	"They should not.",
	"Then Friday it is.",
}

const (
	messageGap = 3 * time.Hour // between two lines of the script
	// unreadByAlpha is how many of the last lines Alpha has not read, so the list shows an
	// unread row; seenByBeta how many lines from the end Beta's marker stops, so the thread
	// shows Seen on an earlier message of Alpha's.
	unreadByAlpha = 2
	seenByBeta    = 4
)

// speaker says who writes script line i: Beta on even lines, Alpha on odd ones, with a run
// of two by the same person every seventh line, so the thread has groups. Beta writes the
// lines Alpha has not read: Alpha's own message would have moved Alpha's marker past them.
func speaker(i int) (fromAlpha bool) {
	if i >= len(script)-unreadByAlpha {
		return false
	}
	if i%7 == 6 {
		return i%2 == 0
	}
	return i%2 == 1
}

// The SQL is parameterized and lives here, not in sqlc: this tool crosses the tables of
// three modules, and no module should expose a way to write them all.
const (
	upsertUser = `
INSERT INTO users (email, password_hash, email_verified_at) VALUES ($1, $2, now())
ON CONFLICT (email) DO UPDATE
SET password_hash = EXCLUDED.password_hash, suspended_at = NULL,
    email_verified_at = COALESCE(users.email_verified_at, now())
RETURNING id`
	upsertProfile = `
INSERT INTO profiles (user_id, full_name, headline) VALUES ($1, $2, $3)
ON CONFLICT (user_id) DO UPDATE SET full_name = EXCLUDED.full_name, headline = EXCLUDED.headline`
	deleteConversations = `DELETE FROM conversations WHERE user_low = ANY($1::uuid[]) AND user_high = ANY($1::uuid[])`
	deleteConnections   = `DELETE FROM connections WHERE requester_id = ANY($1::uuid[]) AND receiver_id = ANY($1::uuid[])`
	insertConnection    = `INSERT INTO connections (requester_id, receiver_id, status, responded_at) VALUES ($1, $2, 'accepted', now())`
	insertConversation  = `
INSERT INTO conversations (state, requested_by, user_low, user_high)
VALUES ($1, $2, LEAST($3::uuid, $4::uuid), GREATEST($3::uuid, $4::uuid))
RETURNING id`
	insertMember  = `INSERT INTO conversation_members (conversation_id, user_id) VALUES ($1, $2)`
	insertMessage = `
INSERT INTO messages (id, conversation_id, sender_id, body, client_id, created_at)
VALUES (uuid_v7_at($4), $1, $2, $3, gen_random_uuid(), $4)
RETURNING id`
	touchConversation = `UPDATE conversations SET last_message_at = $2 WHERE id = $1`
	moveMarker        = `UPDATE conversation_members SET last_read_message_id = $3, last_read_at = now() WHERE conversation_id = $1 AND user_id = $2`
	makeAdmin         = `UPDATE users SET role = 'admin' WHERE id = $1`
	insertReport      = `
INSERT INTO message_reports (conversation_id, reporter_id, reported_id, message_id, reason, details)
VALUES ($1, $2, $3, $4, 'harassment', 'Fake report for UI comparison.')`
)

// seed writes everything in one transaction, so a failed run changes nothing. hash is the
// password's hash, the same for every account.
func seed(ctx context.Context, pool *pgxpool.Pool, hash string) error {
	return pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		alphaID, err := addAccount(ctx, tx, alpha, hash)
		if err != nil {
			return err
		}
		betaID, err := addAccount(ctx, tx, beta, hash)
		if err != nil {
			return err
		}
		gammaID, err := addAccount(ctx, tx, gamma, hash)
		if err != nil {
			return err
		}
		adminID, err := addAccount(ctx, tx, admin, hash)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, makeAdmin, adminID); err != nil {
			return fmt.Errorf("make %s an administrator: %w", admin.email, err)
		}
		ids := []string{alphaID, betaID, gammaID}
		if _, err := tx.Exec(ctx, deleteConversations, ids); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, deleteConnections, ids); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, insertConnection, alphaID, betaID); err != nil {
			return err
		}
		if err := addLongConversation(ctx, tx, alphaID, betaID); err != nil {
			return err
		}
		return addRequest(ctx, tx, gammaID, alphaID)
	})
}

func addAccount(ctx context.Context, tx pgx.Tx, a account, hash string) (string, error) {
	var id string
	if err := tx.QueryRow(ctx, upsertUser, a.email, hash).Scan(&id); err != nil {
		return "", fmt.Errorf("add %s: %w", a.email, err)
	}
	if _, err := tx.Exec(ctx, upsertProfile, id, a.name, a.headline); err != nil {
		return "", fmt.Errorf("add profile of %s: %w", a.email, err)
	}
	return id, nil
}

// addConversation creates a conversation between a and b. requestedBy is nil for one that
// never was a request.
func addConversation(ctx context.Context, tx pgx.Tx, state string, requestedBy *string, a, b string) (string, error) {
	var id string
	if err := tx.QueryRow(ctx, insertConversation, state, requestedBy, a, b).Scan(&id); err != nil {
		return "", fmt.Errorf("add conversation: %w", err)
	}
	for _, member := range []string{a, b} {
		if _, err := tx.Exec(ctx, insertMember, id, member); err != nil {
			return "", fmt.Errorf("add member: %w", err)
		}
	}
	return id, nil
}

func addMessage(ctx context.Context, tx pgx.Tx, conversationID, senderID, body string, at time.Time) (string, error) {
	// A message's ID comes from the clock and must follow the script: two in the same
	// millisecond could sort either way.
	time.Sleep(2 * time.Millisecond)
	var id string
	if err := tx.QueryRow(ctx, insertMessage, conversationID, senderID, body, at).Scan(&id); err != nil {
		return "", fmt.Errorf("add message: %w", err)
	}
	if _, err := tx.Exec(ctx, touchConversation, conversationID, at); err != nil {
		return "", fmt.Errorf("touch conversation: %w", err)
	}
	return id, nil
}

// addLongConversation writes the script backwards from now, about five days of messages,
// sets each member's read marker, and has Beta report Alpha's last message, so the admin pages
// have an open report. Deleting the conversation on the next run takes the report with it.
func addLongConversation(ctx context.Context, tx pgx.Tx, alphaID, betaID string) error {
	id, err := addConversation(ctx, tx, "active", nil, alphaID, betaID)
	if err != nil {
		return err
	}
	start := time.Now().Add(-messageGap * time.Duration(len(script)-1)).Add(-20 * time.Minute)
	sent := make([]string, len(script))
	for i, line := range script {
		sender := betaID
		if speaker(i) {
			sender = alphaID
		}
		at := start.Add(messageGap * time.Duration(i))
		if sent[i], err = addMessage(ctx, tx, id, sender, line, at); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(ctx, moveMarker, id, alphaID, sent[len(sent)-1-unreadByAlpha]); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, moveMarker, id, betaID, sent[len(sent)-1-seenByBeta]); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, insertReport, id, betaID, alphaID, lastFromAlpha(sent)); err != nil {
		return fmt.Errorf("add report: %w", err)
	}
	return nil
}

// lastFromAlpha returns the ID of the last script line Alpha wrote.
func lastFromAlpha(sent []string) string {
	for i := len(sent) - 1; i >= 0; i-- {
		if speaker(i) {
			return sent[i]
		}
	}
	return ""
}

// addRequest writes one pending message request. Sending moves the sender's own marker.
func addRequest(ctx context.Context, tx pgx.Tx, from, to string) error {
	id, err := addConversation(ctx, tx, "request", &from, from, to)
	if err != nil {
		return err
	}
	message, err := addMessage(ctx, tx, id, from, "Hello, I read your paper. Could we talk about a joint dataset?", time.Now().Add(-90*time.Minute))
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, moveMarker, id, from, message)
	return err
}
