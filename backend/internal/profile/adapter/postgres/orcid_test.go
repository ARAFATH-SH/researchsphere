//go:build integration

package postgres

import (
	"crypto/rand"
	"errors"
	"fmt"
	"math/big"
	"testing"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/profile/domain"
)

// testORCID returns a random ORCID iD with a valid check digit, so test runs do not
// collide on the unique constraint.
func testORCID(t *testing.T) string {
	t.Helper()
	n, err := rand.Int(rand.Reader, big.NewInt(1e15))
	if err != nil {
		t.Fatal(err)
	}
	digits := fmt.Sprintf("%015d", n)
	total := 0
	for _, r := range digits {
		total = (total + int(r-'0')) * 2
	}
	check := (12 - total%11) % 11
	last := fmt.Sprint(check)
	if check == 10 {
		last = "X"
	}
	s := digits + last
	return s[0:4] + "-" + s[4:8] + "-" + s[8:12] + "-" + s[12:16]
}

// The ORCID iD is saved, read back with the profile, refused when another profile has
// it, and cleared (FR-5.3).
func TestSetOrcidID(t *testing.T) {
	s, pool := newTestStore(t)
	ada, bob, nobody := newUser(t, pool), newUser(t, pool), newUser(t, pool)
	for _, id := range []string{ada, bob} {
		if _, err := s.UpsertProfile(t.Context(), domain.Profile{UserID: id, FullName: "Someone"}); err != nil {
			t.Fatal(err)
		}
	}
	orcid := testORCID(t)

	if err := s.SetOrcidID(t.Context(), ada, &orcid); err != nil {
		t.Fatal(err)
	}
	a, err := s.FindAccount(t.Context(), ada)
	if err != nil || a.Profile == nil || a.Profile.OrcidID == nil || *a.Profile.OrcidID != orcid {
		t.Fatalf("FindAccount() = %+v, %v; want the ORCID iD", a.Profile, err)
	}
	// Editing the profile keeps the iD.
	p, err := s.UpsertProfile(t.Context(), domain.Profile{UserID: ada, FullName: "Ada"})
	if err != nil || p.OrcidID == nil || *p.OrcidID != orcid {
		t.Errorf("UpsertProfile() = %+v, %v; want the iD kept", p, err)
	}

	if err := s.SetOrcidID(t.Context(), bob, &orcid); !errors.Is(err, domain.ErrORCIDTaken) {
		t.Errorf("Bob taking Ada's iD: err = %v, want ErrORCIDTaken", err)
	}
	if err := s.SetOrcidID(t.Context(), nobody, &orcid); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("user without a profile row: err = %v, want ErrNotFound", err)
	}
	if err := s.SetOrcidID(t.Context(), "not-a-uuid", nil); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("invalid user ID: err = %v, want ErrNotFound", err)
	}

	if err := s.SetOrcidID(t.Context(), ada, nil); err != nil {
		t.Fatal(err)
	}
	if a, _ := s.FindAccount(t.Context(), ada); a.Profile.OrcidID != nil {
		t.Errorf("after clearing, OrcidID = %q", *a.Profile.OrcidID)
	}
	// Once cleared, another profile may have it.
	if err := s.SetOrcidID(t.Context(), bob, &orcid); err != nil {
		t.Errorf("Bob after Ada cleared it: %v", err)
	}
}

// Stored DOIs are matched with how the user is linked to each; unknown DOIs are left out
// (FR-5.3, FR-5.4).
func TestMatchPublicationsByDOI(t *testing.T) {
	s, pool := newTestStore(t)
	ada, bob := newUser(t, pool), newUser(t, pool)
	confirmed, unconfirmed, others, unknown := testDOI(), testDOI(), testDOI(), testDOI()
	cleanupPublications(t, pool, []string{ada, bob}, confirmed, unconfirmed, others)

	ids := map[string]string{}
	for _, doi := range []string{confirmed, unconfirmed, others} {
		owner := ada
		if doi == others {
			owner = bob
		}
		p, _, err := addPublication(t.Context(), s, owner, domain.Publication{DOI: &doi, Title: "T", Source: domain.SourceDOI, CreatedBy: &owner}, nil)
		if err != nil {
			t.Fatal(err)
		}
		ids[doi] = p.ID
	}
	if _, err := pool.Exec(t.Context(), "UPDATE user_publications SET confirmed_at = NULL WHERE user_id = $1 AND publication_id = $2", ada, ids[unconfirmed]); err != nil {
		t.Fatal(err)
	}

	var got map[string]domain.DOIMatch
	err := s.RunInTx(t.Context(), func(tx domain.Tx) error {
		var err error
		got, err = tx.MatchPublicationsByDOI(t.Context(), ada, []string{confirmed, unconfirmed, others, unknown})
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]domain.DOIMatch{
		confirmed:   {PublicationID: ids[confirmed], State: domain.Confirmed},
		unconfirmed: {PublicationID: ids[unconfirmed], State: domain.Unconfirmed},
		others:      {PublicationID: ids[others], State: domain.NotLinked},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d matches %v, want %v", len(got), got, want)
	}
	for doi, m := range want {
		if got[doi] != m {
			t.Errorf("%s: got %+v, want %+v", doi, got[doi], m)
		}
	}

	// No DOIs, no query.
	err = s.RunInTx(t.Context(), func(tx domain.Tx) error {
		m, err := tx.MatchPublicationsByDOI(t.Context(), ada, nil)
		if err == nil && len(m) != 0 {
			t.Errorf("no DOIs: got %v", m)
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
}
