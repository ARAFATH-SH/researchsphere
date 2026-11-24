package main

import (
	"context"
	"fmt"
	"io"
	mrand "math/rand/v2"
	"strings"
	"time"
	"unicode"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	eventpg "github.com/ARAFATH-SH/researchsphere/backend/internal/event/adapter/postgres"
	eventdomain "github.com/ARAFATH-SH/researchsphere/backend/internal/event/domain"
	eventusecase "github.com/ARAFATH-SH/researchsphere/backend/internal/event/usecase"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/platform/postgres"
)

// Sample venues (slice 24, chunk 24.1): with -venues, sampleVenueCount fictional venues,
// venuesPerCluster in each research cluster. Venue k lies in cluster k mod 20 and is made from
// (seed, k) alone; only its dates move with the day the command runs, so the deadlines always
// spread from 90 days ago to 365 days ahead. Each has a sample_venues row (its cluster), the
// marker make sample-purge deletes by. docs/decisions.md, 2026-11-24.
const (
	venuesPerCluster = 10
	// venueStreamBase puts venue k's random stream away from every profile and project stream.
	venueStreamBase = 1<<62 + 1<<61
	pastDays        = 90
	futureDays      = 365
	// secondFieldShare of the sample venues also have a second cluster's field.
	secondFieldShare = 0.3
)

// venueZones are the time zone labels of sample deadlines, AoE most often, as in real calls.
var venueZones = []string{"AoE", "AoE", "AoE", "UTC", "Asia/Dhaka", "Europe/Berlin", "America/New_York", "Asia/Tokyo"}

// abroadCities are the places of sample events outside Bangladesh.
var abroadCities = []struct{ city, country string }{
	{"Singapore", "SG"}, {"Berlin", "DE"}, {"Tokyo", "JP"}, {"Toronto", "CA"}, {"Nairobi", "KE"}, {"Kathmandu", "NP"},
}

// venueKinds are the type and name of the venuesPerCluster venues of each cluster: 4
// conferences, 2 workshops, 2 journals and 2 special issues.
var venueKinds = [venuesPerCluster]struct{ typ, name string }{
	{"conference", "International Conference on %s"},
	{"conference", "Asian Symposium on %s"},
	{"conference", "%s Summit"},
	{"conference", "Bengal Conference on %s"},
	{"workshop", "Workshop on %s"},
	{"workshop", "Workshop on Advances in %s"},
	{"journal", "Journal of %s Research"},
	{"journal", "%s Letters"},
	{"special_issue", "Special Issue on %s"},
	{"special_issue", "Special Issue on Open Problems in %s"},
}

// sampleVenueCount is how many sample venues -venues makes.
func (g *generator) sampleVenueCount() int { return venuesPerCluster * len(g.c.clusters) }

// sampleVenue is one generated venue: what an administrator would enter, and its cluster.
type sampleVenue struct {
	seq     int
	cluster string
	in      eventusecase.Input
}

// venue makes sample venue k from (seed, k); today is the day its dates are relative to.
func (g *generator) venue(k int, today time.Time) sampleVenue {
	ci := k % len(g.c.clusters)
	i := k / len(g.c.clusters) // 0 to venuesPerCluster-1 within the cluster
	c := &g.c.clusters[ci]
	r := mrand.New(mrand.NewPCG(uint64(g.seed), venueStreamBase+uint64(k))) //nolint:gosec // seed and k are not negative
	topic := titleCase(c.Interests[i%len(c.Interests)])

	kind := venueKinds[i%len(venueKinds)]
	typ, name := kind.typ, fmt.Sprintf(kind.name, topic)

	in := eventusecase.Input{Name: name, ShortName: acronym(name), Type: typ, Fields: []string{c.ID}}
	if r.Float64() < secondFieldShare {
		if other := g.c.clusters[(ci+1+r.IntN(len(g.c.clusters)-1))%len(g.c.clusters)].ID; other != c.ID {
			in.Fields = append(in.Fields, other)
		}
	}

	in.TimeZone = pick(r, venueZones)
	deadline := today.AddDate(0, 0, -pastDays+r.IntN(pastDays+futureDays+1))
	in.SubmissionDeadline = deadline.Format("2006-01-02")
	if r.IntN(3) == 0 {
		in.SubmissionDeadline += "T17:00"
	}
	if typ == "conference" || typ == "workshop" {
		if r.IntN(5) < 2 {
			in.AbstractDeadline = deadline.AddDate(0, 0, -7).Format("2006-01-02")
		}
		in.NotificationAt = deadline.AddDate(0, 0, 30+r.IntN(30)).Format("2006-01-02")
		start := deadline.AddDate(0, 0, 90+r.IntN(60))
		in.StartsOn, in.EndsOn = start.Format("2006-01-02"), start.AddDate(0, 0, r.IntN(4)).Format("2006-01-02")
		switch x := r.Float64(); {
		case x < 0.2:
			in.Attendance = "online"
		case x < 0.5:
			in.Attendance = "hybrid"
		default:
			in.Attendance = "in_person"
		}
		if in.Attendance != "online" {
			if r.Float64() < 0.6 {
				in.City, in.Country = pick(r, g.c.common.BDCities), "BD"
			} else {
				p := abroadCities[r.IntN(len(abroadCities))]
				in.City, in.Country = p.city, p.country
			}
		}
	}
	in.OfficialURL = fmt.Sprintf("https://sample-venue-%03d.example.org/cfp", k)

	f := newFiller(r, c, "", "")
	topics := []string{
		f.fill("{method} for {object}"),
		f.fill("{interest} in {context}"),
		f.fill("datasets and benchmarks of {object}"),
	}
	in.Scope = fmt.Sprintf("This is a fictional sample call for papers, made for testing ResearchSphere.\n\n"+
		"%s invites original work on %s and %s.\n\n**Topics** include:\n- %s\n- %s\n- %s\n\n%s",
		name, strings.ToLower(topic), f.slot("interest2"), topics[0], topics[1], topics[2], f.fill(pick(r, c.Abstracts)))
	return sampleVenue{seq: k, cluster: c.ID, in: in}
}

// titleCase capitalises the first letter of each word, keeping the rest (so "NLP" stays).
func titleCase(s string) string {
	words := strings.Fields(s)
	for i, w := range words {
		words[i] = upperFirst(w)
	}
	return strings.Join(words, " ")
}

// acronym is the capital letters of the name's words, such as ICOBN for "International
// Conference on Bangla NLP"; "" when it would be shorter than 2 or longer than 10 letters.
func acronym(name string) string {
	var b strings.Builder
	for _, w := range strings.Fields(name) {
		if r := []rune(w)[0]; unicode.IsUpper(r) {
			b.WriteRune(r)
		}
	}
	if n := b.Len(); n < 2 || n > 10 {
		return ""
	}
	return b.String()
}

// existingSampleVenues returns the seq of every sample venue, refusing another seed's.
func existingSampleVenues(ctx context.Context, pool *pgxpool.Pool, seed int64) (map[int]bool, error) {
	rows, err := pool.Query(ctx, `SELECT seq, seed FROM sample_venues`)
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
			return nil, fmt.Errorf("the sample venues were made with seed %d, not %d: run make sample-purge first", s, seed)
		}
		have[seq] = true
	}
	return have, rows.Err()
}

const (
	insertVenueLabel = `INSERT INTO sample_venues (venue_id, seq, seed, cluster) VALUES ($1, $2, $3, $4)`
	// Sample venues wait behind real changes, like sample profiles.
	lowerVenueJobPriority = `UPDATE embedding_jobs SET priority = 1 WHERE owner_type = 'venue' AND owner_id = $1 AND priority = 0`
)

// runVenues tops up the sample venues, all in one transaction, through the event module's
// store and its rules (eventusecase.Validate), so a sample venue is one an administrator could
// have entered. They have no audit rows: no administrator made them.
func runVenues(ctx context.Context, pool *pgxpool.Pool, g *generator, today time.Time, out io.Writer) error {
	have, err := existingSampleVenues(ctx, pool, g.seed)
	if err != nil {
		return err
	}
	var missing []int
	for k := range g.sampleVenueCount() {
		if !have[k] {
			missing = append(missing, k)
		}
	}
	fmt.Fprintf(out, "Sample venues: %d exist; adding %d (%d per cluster, deadlines from %d days ago to %d days ahead).\n",
		len(have), len(missing), venuesPerCluster, pastDays, futureDays)
	if len(missing) == 0 {
		return nil
	}
	store := eventpg.NewStore(pool)
	fields, err := store.Fields(ctx)
	if err != nil {
		return err
	}
	err = postgres.NewTransactor(pool).InTx(ctx, func(ctx context.Context) error {
		conn := postgres.Conn(ctx, pool)
		for _, k := range missing {
			sv := g.venue(k, today)
			v, err := eventusecase.Validate(sv.in, fields)
			if err != nil {
				return fmt.Errorf("sample venue %d: %w", k, err)
			}
			v.Source = eventdomain.SourceManual
			id, err := store.Create(ctx, v, "")
			if err != nil {
				return fmt.Errorf("sample venue %d: %w", k, err)
			}
			for _, step := range []struct {
				sql  string
				args []any
			}{
				{insertVenueLabel, []any{id, k, g.seed, sv.cluster}},
				{lowerVenueJobPriority, []any{id}},
			} {
				if _, err := conn.Exec(ctx, step.sql, step.args...); err != nil {
					return fmt.Errorf("sample venue %d: %w", k, err)
				}
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "Created %d sample venues.\n", len(missing))
	return nil
}

// purgeVenues deletes every venue in sample_venues: its fields, embedding and marker go by
// cascade, and the delete trigger drops its embedding job. Removal (a soft delete) is for
// real venues; sample ones go for good.
func purgeVenues(ctx context.Context, pool *pgxpool.Pool) (int, error) {
	var n int64
	err := pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `DELETE FROM venues WHERE id IN (SELECT venue_id FROM sample_venues)`)
		n = tag.RowsAffected()
		return err
	})
	return int(n), err
}
