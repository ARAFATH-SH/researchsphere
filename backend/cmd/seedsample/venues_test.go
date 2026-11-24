package main

import (
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	eventdomain "github.com/ARAFATH-SH/researchsphere/backend/internal/event/domain"
	eventusecase "github.com/ARAFATH-SH/researchsphere/backend/internal/event/usecase"
)

// The research fields of venues are the sample clusters plus "other" (migration 000039), so
// every sample venue's field is one the database knows.
func TestResearchFieldsAreTheClusters(t *testing.T) {
	g := testGenerator(t, defaultSeed)
	sql, err := os.ReadFile("../../db/migrations/000039_venues.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range g.c.clusters {
		if !strings.Contains(string(sql), "('"+c.ID+"',") {
			t.Errorf("cluster %s is not a research field in 000039", c.ID)
		}
	}
}

func venueFields(g *generator) []eventdomain.Field {
	fields := make([]eventdomain.Field, 0, len(g.c.clusters)+1)
	for _, c := range g.c.clusters {
		fields = append(fields, eventdomain.Field{Key: c.ID, Label: c.Name})
	}
	return append(fields, eventdomain.Field{Key: "other", Label: "Other"})
}

// 200 fictional venues, 10 per cluster, of every type, each valid by the rules an
// administrator's venue follows, with deadlines from 90 days ago to 365 days ahead.
func TestSampleVenues(t *testing.T) {
	g := testGenerator(t, defaultSeed)
	today := time.Date(2026, 11, 24, 0, 0, 0, 0, time.UTC)
	fields := venueFields(g)
	n := g.sampleVenueCount()
	if n != 200 {
		t.Fatalf("%d sample venues, want 200", n)
	}
	perCluster := map[string]int{}
	types := map[eventdomain.VenueType]int{}
	names := map[string]bool{}
	var past, upcoming int
	for k := range n {
		sv := g.venue(k, today)
		v, err := eventusecase.Validate(sv.in, fields)
		if err != nil {
			t.Fatalf("venue %d (%+v): %v", k, sv.in, err)
		}
		if v.FieldKeys()[0] != sv.cluster && !strings.Contains(strings.Join(v.FieldKeys(), ","), sv.cluster) {
			t.Errorf("venue %d: fields %v miss its cluster %s", k, v.FieldKeys(), sv.cluster)
		}
		perCluster[sv.cluster]++
		types[v.Type]++
		if names[v.Name] {
			t.Errorf("venue %d: the name %q is used twice", k, v.Name)
		}
		names[v.Name] = true
		earliest, latest := today.AddDate(0, 0, -pastDays), today.AddDate(0, 0, futureDays+2)
		if v.SubmissionDeadline.Before(earliest) || v.SubmissionDeadline.After(latest) {
			t.Errorf("venue %d: deadline %v out of range", k, v.SubmissionDeadline)
		}
		if v.SubmissionDeadline.Before(today) {
			past++
		} else {
			upcoming++
		}
		if !strings.HasPrefix(v.OfficialURL, "https://") || !strings.HasSuffix(strings.SplitN(v.OfficialURL[8:], "/", 2)[0], ".example.org") {
			t.Errorf("venue %d: link %s", k, v.OfficialURL)
		}
		if !strings.Contains(v.Scope, "fictional sample") {
			t.Errorf("venue %d: the scope does not say it is fictional", k)
		}
	}
	for _, c := range g.c.clusters {
		if perCluster[c.ID] != venuesPerCluster {
			t.Errorf("cluster %s has %d venues", c.ID, perCluster[c.ID])
		}
	}
	for _, typ := range eventdomain.VenueTypes {
		if types[typ] == 0 {
			t.Errorf("no sample %s", typ)
		}
	}
	if past < 20 || upcoming < 100 {
		t.Errorf("%d past and %d upcoming deadlines; want both, mostly upcoming", past, upcoming)
	}
}

// The same seed and day make the same venue.
func TestSampleVenuesAreDeterministic(t *testing.T) {
	today := time.Date(2026, 11, 24, 0, 0, 0, 0, time.UTC)
	a, b := testGenerator(t, defaultSeed), testGenerator(t, defaultSeed)
	for _, k := range []int{0, 57, 199} {
		if va, vb := a.venue(k, today), b.venue(k, today); !reflect.DeepEqual(va, vb) {
			t.Errorf("venue %d differs: %+v and %+v", k, va, vb)
		}
	}
	if acronym("International Conference on Bangla NLP") != "ICBN" || acronym("Workshop") != "" {
		t.Errorf("acronym = %q", acronym("International Conference on Bangla NLP"))
	}
}
