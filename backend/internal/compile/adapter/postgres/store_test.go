//go:build integration

package postgres

import (
	"testing"
	"time"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/platform/testenv"
)

// cmd/purgecompileoutput: clearing the output of compiles that started before a cutoff drops
// their PDFs' stored_files rows and queues the objects for deletion (triggers of 000038 and
// 000007), clears their logs, and leaves later compiles and every record's status alone.
func TestClearOutputBefore(t *testing.T) {
	pool := testenv.DB(t)
	ctx := t.Context()
	s := NewStore(pool)
	var owner, projectID string
	if err := pool.QueryRow(ctx, `INSERT INTO users (email, password_hash) VALUES ($1, 'hash') RETURNING id::text`, testenv.Email(t)).Scan(&owner); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `WITH p AS (INSERT INTO projects (title, description) VALUES ('Paper', 'About') RETURNING id),
		m AS (INSERT INTO project_members (project_id, user_id, role) SELECT id, $1, 'owner' FROM p)
		SELECT id::text FROM p`, owner).Scan(&projectID); err != nil {
		t.Fatal(err)
	}
	cutoff := time.Date(2026, 11, 23, 15, 10, 27, 0, time.UTC)
	// job inserts a finished compile that started at started, with a PDF and a log if asked.
	job := func(started time.Time, status string, pdf bool, log *string) string {
		t.Helper()
		var fileID *string
		if pdf {
			var id string
			if err := pool.QueryRow(ctx, `INSERT INTO stored_files (purpose, project_id, object_key, content_type, size_bytes, scan_status)
				VALUES ('pdf', $1::uuid, 'projects/' || $1::text || '/compiles/' || gen_random_uuid()::text || '.pdf', 'application/pdf', 10, 'clean')
				RETURNING id::text`, projectID).Scan(&id); err != nil {
				t.Fatal(err)
			}
			fileID = &id
		}
		var id string
		if err := pool.QueryRow(ctx, `INSERT INTO compile_jobs (project_id, status, engine, main_file_path, queued_at, started_at, finished_at, pdf_file_id, log)
			VALUES ($1, $2, 'pdflatex', 'main.tex', $3, $3, $3::timestamptz + interval '5 seconds', $4, $5) RETURNING id::text`,
			projectID, status, started, fileID, log).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id
	}
	text := "This is pdfTeX"
	leaked := job(cutoff.Add(-80*time.Minute), "succeeded", true, nil)
	oldLog := job(cutoff.Add(-10*time.Minute), "failed", false, &text)
	job(cutoff.Add(-90*time.Minute), "timed_out", false, nil) // nothing to drop
	after := job(cutoff.Add(17*time.Minute), "succeeded", true, &text)

	listed, err := s.OutputBefore(ctx, cutoff)
	if err != nil || len(listed) != 2 || listed[0].JobID != leaked || !listed[0].HasPDF || listed[0].HasLog ||
		listed[1].JobID != oldLog || listed[1].HasPDF || !listed[1].HasLog {
		t.Fatalf("OutputBefore = %+v, %v", listed, err)
	}
	var files int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM stored_files WHERE purpose = 'pdf'`).Scan(&files); err != nil || files != 2 {
		t.Fatalf("listing changed the files: %d %v", files, err)
	}

	cleared, err := s.ClearOutputBefore(ctx, cutoff)
	if err != nil || len(cleared) != 2 {
		t.Fatalf("ClearOutputBefore = %+v, %v", cleared, err)
	}
	var pdfLeft, logLeft bool
	var status string
	if err := pool.QueryRow(ctx, `SELECT pdf_file_id IS NOT NULL, log IS NOT NULL, status FROM compile_jobs WHERE id = $1`, leaked).Scan(&pdfLeft, &logLeft, &status); err != nil {
		t.Fatal(err)
	}
	if pdfLeft || logLeft || status != "succeeded" {
		t.Errorf("leaked job: pdf %v log %v status %s", pdfLeft, logLeft, status)
	}
	if err := pool.QueryRow(ctx, `SELECT log IS NOT NULL FROM compile_jobs WHERE id = $1`, oldLog).Scan(&logLeft); err != nil || logLeft {
		t.Errorf("old log kept: %v %v", logLeft, err)
	}
	// One PDF row left (the later compile's), and the leaked object queued for deletion.
	var keys []string
	rows, err := pool.Query(ctx, `SELECT object_key FROM storage_deletions`)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var k string
		if err := rows.Scan(&k); err != nil {
			t.Fatal(err)
		}
		keys = append(keys, k)
	}
	rows.Close()
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM stored_files WHERE purpose = 'pdf'`).Scan(&files); err != nil || files != 1 || len(keys) != 1 {
		t.Errorf("files %d, queued %v, err %v", files, keys, err)
	}
	pdf, err := s.LatestPDF(ctx, projectID)
	if err != nil || pdf.CompileID != after {
		t.Errorf("LatestPDF = %+v, %v", pdf, err)
	}
	if again, err := s.ClearOutputBefore(ctx, cutoff); err != nil || len(again) != 0 {
		t.Errorf("second run = %+v, %v", again, err)
	}
}
