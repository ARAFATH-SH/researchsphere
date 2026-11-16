package main

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"fmt"
	mrand "math/rand/v2"
	"slices"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Sample projects (slice 19, chunk 19.1): with -projects, about one project per
// profilesPerProject sample profiles. Project k is owned by sample profile k*profilesPerProject,
// lies in that owner's main cluster and shares its split; its label row is in sample_projects
// for the later project matching evaluation (AI-3). docs/decisions.md, 2026-11-16.
const profilesPerProject = 10

// The status shares of sample projects, so search has closed and archived projects to leave out.
const (
	archivedProjectShare = 0.05
	closedProjectShare   = 0.10
)

// projectStreamBase puts project k's random stream far from every profile stream
// (seq*streamCount+stream), so adding projects changes no profile.
const projectStreamBase = 1 << 62

// sampleProject is one generated sample project.
type sampleProject struct {
	seq, ownerSeq                int
	title, description, abstract string // abstract "" is none
	status, cluster, split       string
	skills                       []string
}

// project makes sample project k from (seed, k) and its owner's label alone.
func (g *generator) project(k int) sampleProject {
	owner := k * profilesPerProject
	l := g.label(owner)
	c := &g.c.clusters[l.main]
	r := mrand.New(mrand.NewPCG(uint64(g.seed), projectStreamBase+uint64(k))) //nolint:gosec // seed and k are not negative
	f := newFiller(r, c, pick(r, g.c.common.Roles), pick(r, g.c.common.BnRoles))
	p := sampleProject{seq: k, ownerSeq: owner, cluster: c.ID, split: l.split, status: "open"}
	p.title = cut(upperFirst(f.fill(pick(r, c.Titles))), 150)
	p.description = f.fill(pick(r, c.Abstracts)) + " We are looking for collaborators who know " + f.slot("skill") + "."
	if r.Float64() < 0.5 {
		p.abstract = cut(newFiller(r, c, f.role, f.bnRole).fill(pick(r, c.Abstracts)), 3000)
	}
	p.skills = dedupe(pickN(r, c.Skills, 2+r.IntN(4)))
	switch x := r.Float64(); {
	case x < archivedProjectShare:
		p.status = "archived"
	case x < archivedProjectShare+closedProjectShare:
		p.status = "closed"
	}
	return p
}

// cut shortens s to at most n characters.
func cut(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n])
}

// existingSampleProjects returns the seq of every sample project, refusing another seed's.
func existingSampleProjects(ctx context.Context, pool *pgxpool.Pool, seed int64) (map[int]bool, error) {
	rows, err := pool.Query(ctx, `SELECT seq, seed FROM sample_projects`)
	if err != nil {
		return nil, err
	}
	have := map[int]bool{}
	for rows.Next() {
		var seq int
		var s int64
		if err := rows.Scan(&seq, &s); err != nil {
			return nil, err
		}
		if s != seed {
			rows.Close()
			return nil, fmt.Errorf("the sample projects were made with seed %d, not %d: run make sample-purge first", s, seed)
		}
		have[seq] = true
	}
	return have, rows.Err()
}

// projectPlan lists the sample projects to create: one for every sample profile whose seq is
// a multiple of profilesPerProject, unless it exists.
func projectPlan(profiles map[int]string, have map[int]bool) []int {
	var missing []int
	for seq := range profiles {
		if k := seq / profilesPerProject; seq%profilesPerProject == 0 && !have[k] {
			missing = append(missing, k)
		}
	}
	slices.Sort(missing)
	return missing
}

const (
	insertSampleProjects = `
INSERT INTO projects (id, title, description, abstract, status)
SELECT id, title, description, NULLIF(abstract, ''), status::project_status
FROM unnest($1::uuid[], $2::text[], $3::text[], $4::text[], $5::text[]) AS x(id, title, description, abstract, status)`

	insertSampleOwners = `
INSERT INTO project_members (project_id, user_id, role)
SELECT id, owner, 'owner' FROM unnest($1::uuid[], $2::uuid[]) AS x(id, owner)`

	linkProjectSkills = `
INSERT INTO project_skills (project_id, skill_id)
SELECT x.project_id, t.id FROM unnest($1::uuid[], $2::citext[]) AS x(project_id, name)
JOIN skills t ON t.name = x.name
ON CONFLICT DO NOTHING`

	insertProjectLabels = `
INSERT INTO sample_projects (project_id, seq, seed, owner_seq, cluster, split)
SELECT id, seq, $6, owner_seq, cluster, split
FROM unnest($1::uuid[], $2::int[], $3::int[], $4::text[], $5::text[]) AS x(id, seq, owner_seq, cluster, split)`

	// Sample projects wait behind real users' changes, like sample profiles.
	lowerProjectJobPriority = `
UPDATE embedding_jobs SET priority = 1
WHERE owner_type = 'project' AND owner_id = ANY($1::uuid[]) AND priority = 0`
)

// seedProjects creates the sample projects ks, batchSize per transaction. profiles maps a
// sample profile's seq to its user ID. progress is called after each batch.
func seedProjects(ctx context.Context, pool *pgxpool.Pool, g *generator, ks []int, profiles map[int]string, progress func(done int)) error {
	for start := 0; start < len(ks); start += batchSize {
		batch := ks[start:min(start+batchSize, len(ks))]
		var (
			ids, titles, descs, abstracts, statuses []string
			owners, clusters, splits                []string
			seqs, ownerSeqs                         []int32
			skillProjects, skillNames               []string
		)
		for _, k := range batch {
			p := g.project(k)
			owner, ok := profiles[p.ownerSeq]
			if !ok {
				return fmt.Errorf("sample project %d: owner profile %d does not exist", k, p.ownerSeq)
			}
			id := newUUIDv7()
			ids, titles, descs, abstracts, statuses = append(ids, id), append(titles, p.title), append(descs, p.description), append(abstracts, p.abstract), append(statuses, p.status)
			owners, clusters, splits = append(owners, owner), append(clusters, p.cluster), append(splits, p.split)
			seqs, ownerSeqs = append(seqs, int32(p.seq)), append(ownerSeqs, int32(p.ownerSeq)) //nolint:gosec // below maxProfiles
			for _, s := range p.skills {
				skillProjects, skillNames = append(skillProjects, id), append(skillNames, s)
			}
		}
		err := pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
			steps := []struct {
				what string
				sql  string
				args []any
			}{
				{"projects", insertSampleProjects, []any{ids, titles, descs, abstracts, statuses}},
				{"owners", insertSampleOwners, []any{ids, owners}},
				{"skill names", insertSkillNames, []any{skillNames}},
				{"project skills", linkProjectSkills, []any{skillProjects, skillNames}},
				{"labels", insertProjectLabels, []any{ids, seqs, ownerSeqs, clusters, splits, g.seed}},
				{"embedding priority", lowerProjectJobPriority, []any{ids}},
			}
			for _, s := range steps {
				if _, err := tx.Exec(ctx, s.sql, s.args...); err != nil {
					return fmt.Errorf("%s: %w", s.what, err)
				}
			}
			return nil
		})
		if err != nil {
			return fmt.Errorf("projects %d to %d: %w", batch[0], batch[len(batch)-1], err)
		}
		if progress != nil {
			progress(start + len(batch))
		}
	}
	return nil
}

// newUUIDv7 returns a time-ordered (version 7) UUID, like the database's uuid_v7(), so sample
// projects look like real ones.
func newUUIDv7() string {
	var b [16]byte
	_, _ = rand.Read(b[:]) // never fails (Go 1.24+)
	var ms [8]byte
	binary.BigEndian.PutUint64(ms[:], uint64(time.Now().UnixMilli())) //nolint:gosec // after 1970
	copy(b[0:6], ms[2:8])
	b[6] = b[6]&0x0f | 0x70
	b[8] = b[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}
