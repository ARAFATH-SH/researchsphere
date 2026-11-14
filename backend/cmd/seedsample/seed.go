package main

import (
	"context"
	"crypto/rand"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// sampleDomain is the address domain of every sample account. It cannot receive mail.
const sampleDomain = "sample.researchsphere.local"

// maxProfiles is the most -n allows: the address has five digits.
const maxProfiles = 100000

// batchSize is how many profiles one transaction writes. A profile and its label row are
// always committed together, so an interrupted run leaves no half-made profile.
const batchSize = 500

// plan is what a run will do, printed before it writes.
type plan struct {
	existing int   // sample profiles already in the database
	missing  []int // seqs below n to create
}

// existingSamples returns the seq and user ID of every sample profile, and refuses sample data
// made with another seed: mixing seeds would break "the same command makes the same data".
func existingSamples(ctx context.Context, pool *pgxpool.Pool, seed int64) (map[int]string, error) {
	rows, err := pool.Query(ctx, `SELECT seq, user_id::text, seed FROM sample_profiles`)
	if err != nil {
		return nil, err
	}
	ids := map[int]string{}
	var other int64
	var mixed bool
	for rows.Next() {
		var seq int
		var id string
		var s int64
		if err := rows.Scan(&seq, &id, &s); err != nil {
			return nil, err
		}
		if s != seed {
			mixed, other = true, s
		}
		ids[seq] = id
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if mixed {
		return nil, fmt.Errorf("the sample data was made with seed %d, not %d: run make sample-purge first, or use -seed %d", other, seed, other)
	}
	return ids, nil
}

func makePlan(ids map[int]string, n int) plan {
	p := plan{existing: len(ids)}
	for seq := range n {
		if _, ok := ids[seq]; !ok {
			p.missing = append(p.missing, seq)
		}
	}
	return p
}

// seed creates the sample profiles in p.missing, batchSize per transaction, and adds their IDs
// to ids. hash is the argon2 hash of SAMPLE_PASSWORD, shared by every sample account. progress
// is called after each batch with how many it created so far.
func seed(ctx context.Context, pool *pgxpool.Pool, g *generator, p plan, ids map[int]string, hash string, progress func(done int)) error {
	if err := refuseTakenAddresses(ctx, pool, p.missing); err != nil {
		return err
	}
	for start := 0; start < len(p.missing); start += batchSize {
		seqs := p.missing[start:min(start+batchSize, len(p.missing))]
		batch := make([]profile, len(seqs))
		newIDs := map[int]string{}
		for i, seq := range seqs {
			batch[i] = g.profile(seq)
			newIDs[seq] = newUUID()
		}
		err := pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
			return insertBatch(ctx, tx, g, batch, ids, newIDs, hash)
		})
		if err != nil {
			return fmt.Errorf("profiles %d to %d: %w", seqs[0], seqs[len(seqs)-1], err)
		}
		for seq, id := range newIDs {
			ids[seq] = id
		}
		if progress != nil {
			progress(start + len(seqs))
		}
	}
	return nil
}

// refuseTakenAddresses stops before writing when an account that is not a sample profile
// already has an address seed would create.
func refuseTakenAddresses(ctx context.Context, pool *pgxpool.Pool, seqs []int) error {
	emails := make([]string, len(seqs))
	for i, seq := range seqs {
		emails[i] = email(seq)
	}
	var taken []string
	rows, err := pool.Query(ctx, `SELECT email::text FROM users WHERE email = ANY($1::citext[]) ORDER BY email LIMIT 5`, emails)
	if err != nil {
		return err
	}
	taken, err = pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return err
	}
	if len(taken) > 0 {
		return fmt.Errorf("accounts that are not sample profiles already use %s: delete them first", strings.Join(taken, ", "))
	}
	return nil
}

// The batch inserts. Each takes arrays, one element per row, through unnest; empty text
// becomes NULL, as the profile checks want.
const (
	insertUsers = `
INSERT INTO users (id, email, password_hash, email_verified_at)
SELECT id, email, $3, now() FROM unnest($1::uuid[], $2::citext[]) AS x(id, email)`

	insertProfiles = `
INSERT INTO profiles (user_id, full_name, headline, bio, institution, department, job_title, country, city)
SELECT id, name, NULLIF(headline, ''), NULLIF(bio, ''), NULLIF(institution, ''), NULLIF(department, ''),
       NULLIF(job_title, ''), NULLIF(country, ''), NULLIF(city, '')
FROM unnest($1::uuid[], $2::text[], $3::text[], $4::text[], $5::text[], $6::text[], $7::text[], $8::text[], $9::text[])
    AS x(id, name, headline, bio, institution, department, job_title, country, city)`

	insertInterestNames = `
INSERT INTO research_interests (name)
SELECT DISTINCT ON (lower(name)) name FROM unnest($1::text[]) AS x(name)
ON CONFLICT (name) DO NOTHING`

	linkInterests = `
INSERT INTO user_interests (user_id, interest_id)
SELECT x.user_id, t.id FROM unnest($1::uuid[], $2::citext[]) AS x(user_id, name)
JOIN research_interests t ON t.name = x.name
ON CONFLICT DO NOTHING`

	insertSkillNames = `
INSERT INTO skills (name)
SELECT DISTINCT ON (lower(name)) name FROM unnest($1::text[]) AS x(name)
ON CONFLICT (name) DO NOTHING`

	linkSkills = `
INSERT INTO user_skills (user_id, skill_id)
SELECT x.user_id, t.id FROM unnest($1::uuid[], $2::citext[]) AS x(user_id, name)
JOIN skills t ON t.name = x.name
ON CONFLICT DO NOTHING`

	// No DOI and no URL: the records are fictional (source manual).
	insertPublications = `
INSERT INTO publications (id, title, abstract, venue_name, publication_year, source, created_by)
SELECT id, title, abstract, venue, year, 'manual', created_by
FROM unnest($1::uuid[], $2::text[], $3::text[], $4::text[], $5::int[], $6::uuid[])
    AS x(id, title, abstract, venue, year, created_by)`

	linkPublications = `
INSERT INTO user_publications (user_id, publication_id, confirmed_at)
SELECT user_id, publication_id, now() FROM unnest($1::uuid[], $2::uuid[]) AS x(user_id, publication_id)
ON CONFLICT DO NOTHING`

	insertConnections = `
INSERT INTO connections (requester_id, receiver_id, status, responded_at)
SELECT requester, receiver, 'accepted', now() FROM unnest($1::uuid[], $2::uuid[]) AS x(requester, receiver)
ON CONFLICT DO NOTHING`

	insertLabels = `
INSERT INTO sample_profiles (user_id, seq, seed, main_cluster, second_cluster, split, sparse, bangla)
SELECT id, seq, $8, main, NULLIF(second, ''), split, sparse, bangla
FROM unnest($1::uuid[], $2::int[], $3::text[], $4::text[], $5::text[], $6::bool[], $7::text[])
    AS x(id, seq, main, second, split, sparse, bangla)`

	// The triggers of migration 000028 queued every changed profile at priority 0, the
	// priority of a user's own edit. Sample profiles wait behind real users' changes, as a
	// backfill does (AI-1.5). Only the given sample IDs are touched.
	lowerJobPriority = `
UPDATE embedding_jobs SET priority = 1
WHERE owner_type = 'profile' AND owner_id = ANY($1::uuid[]) AND priority = 0`
)

// insertBatch writes one batch in tx. ids holds the profiles already stored, newIDs those of
// this batch; links to an earlier profile that no longer exists are skipped.
func insertBatch(ctx context.Context, tx pgx.Tx, g *generator, batch []profile, ids, newIDs map[int]string, hash string) error {
	idOf := func(seq int) (string, bool) {
		if id, ok := newIDs[seq]; ok {
			return id, true
		}
		id, ok := ids[seq]
		return id, ok
	}
	var (
		userIDs, emails                                   []string
		names, headlines, bios, insts, depts, jobs, ctrys []string
		cities                                            []string
		intUsers, intNames, skillUsers, skillNames        []string
		pubIDs, pubTitles, pubAbstracts, pubVenues        []string
		pubYears                                          []int32
		pubCreators, linkUsers, linkPubs                  []string
		connFrom, connTo                                  []string
		seqs                                              []int32
		mains, seconds, splits, langs                     []string
		sparse                                            []bool
		touched                                           []string
	)
	clusters := func(i int) string {
		if i < 0 {
			return ""
		}
		return g.c.clusters[i].ID
	}
	for _, p := range batch {
		id := newIDs[p.seq]
		userIDs = append(userIDs, id)
		emails = append(emails, p.email)
		names = append(names, p.fullName)
		headlines = append(headlines, p.headline)
		bios = append(bios, p.bio)
		insts = append(insts, p.institution)
		depts = append(depts, p.department)
		jobs = append(jobs, p.jobTitle)
		ctrys = append(ctrys, p.country)
		cities = append(cities, p.city)
		for _, s := range p.interests {
			intUsers, intNames = append(intUsers, id), append(intNames, s)
		}
		for _, s := range p.skills {
			skillUsers, skillNames = append(skillUsers, id), append(skillNames, s)
		}
		touched = append(touched, id)
		for _, pub := range p.publications {
			pid := newUUID()
			pubIDs = append(pubIDs, pid)
			pubTitles = append(pubTitles, pub.title)
			pubAbstracts = append(pubAbstracts, pub.abstract)
			pubVenues = append(pubVenues, pub.venue)
			pubYears = append(pubYears, int32(pub.year)) //nolint:gosec // 2015 to 2026
			pubCreators = append(pubCreators, id)
			linkUsers, linkPubs = append(linkUsers, id), append(linkPubs, pid)
			if pub.coAuthor >= 0 {
				if co, ok := idOf(pub.coAuthor); ok {
					linkUsers, linkPubs = append(linkUsers, co), append(linkPubs, pid)
					touched = append(touched, co)
				}
			}
		}
		for _, j := range p.connections {
			if other, ok := idOf(j); ok {
				connFrom, connTo = append(connFrom, id), append(connTo, other)
			}
		}
		seqs = append(seqs, int32(p.seq)) //nolint:gosec // below maxProfiles
		mains = append(mains, clusters(p.label.main))
		seconds = append(seconds, clusters(p.label.second))
		splits = append(splits, p.label.split)
		sparse = append(sparse, p.label.sparse)
		langs = append(langs, p.label.bangla)
	}

	steps := []struct {
		what string
		sql  string
		args []any
	}{
		{"users", insertUsers, []any{userIDs, emails, hash}},
		{"profiles", insertProfiles, []any{userIDs, names, headlines, bios, insts, depts, jobs, ctrys, cities}},
		{"interest names", insertInterestNames, []any{intNames}},
		{"interests", linkInterests, []any{intUsers, intNames}},
		{"skill names", insertSkillNames, []any{skillNames}},
		{"skills", linkSkills, []any{skillUsers, skillNames}},
		{"publications", insertPublications, []any{pubIDs, pubTitles, pubAbstracts, pubVenues, pubYears, pubCreators}},
		{"publication authors", linkPublications, []any{linkUsers, linkPubs}},
		{"connections", insertConnections, []any{connFrom, connTo}},
		{"labels", insertLabels, []any{userIDs, seqs, mains, seconds, splits, sparse, langs, g.seed}},
		{"embedding priority", lowerJobPriority, []any{touched}},
	}
	for _, s := range steps {
		if _, err := tx.Exec(ctx, s.sql, s.args...); err != nil {
			return fmt.Errorf("%s: %w", s.what, err)
		}
	}
	return nil
}

// newUUID returns a random (version 4) UUID. IDs are made here, not by the database, so a
// batch can link rows without reading them back.
func newUUID() string {
	var b [16]byte
	_, _ = rand.Read(b[:]) // never fails (Go 1.24+)
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}
