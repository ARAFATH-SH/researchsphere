package usecase

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/auth/domain"
)

func deleteAccount(e *env, userID, password string) error {
	return e.svc.DeleteAccount(context.Background(), DeleteAccountInput{UserID: userID, Password: password})
}

func TestDeleteAccount(t *testing.T) {
	e := newEnv()
	ada := verifiedUser(t, e, "ada@example.org")
	grace := verifiedUser(t, e, "grace@example.org")
	adaSession := loggedIn(t, e, "ada@example.org")
	graceSession := loggedIn(t, e, "grace@example.org")
	solo := e.store.addProject("Ada's notes", ada)
	gracesProject := e.store.addProject("Grace's compiler", grace, ada) // Ada is an editor
	e.store.pubs["solo-pub"] = []string{ada}
	e.store.pubs["shared-pub"] = []string{ada, grace}
	failLogins(t, e, "ada@example.org", MaxFailedLogins-1)

	if err := deleteAccount(e, ada, loginPassword); err != nil {
		t.Fatalf("DeleteAccount() error = %v, want nil", err)
	}

	if _, err := e.store.FindUserByID(context.Background(), ada); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("find the deleted user: error = %v, want ErrNotFound", err)
	}
	if _, ok := e.store.projects[solo]; ok {
		t.Error("the project where Ada was the only member was not deleted")
	}
	p, ok := e.store.projects[gracesProject]
	if !ok {
		t.Fatal("another user's project was deleted")
	}
	if _, ok := p.members[ada]; ok {
		t.Error("Ada is still a member of Grace's project")
	}
	if _, ok := e.store.pubs["solo-pub"]; ok {
		t.Error("a publication with no author left was not deleted")
	}
	if got := e.store.pubs["shared-pub"]; !slices.Equal(got, []string{grace}) {
		t.Errorf("shared publication authors = %v, want only Grace", got)
	}
	if _, err := e.svc.Refresh(context.Background(), adaSession); !errors.Is(err, domain.ErrSessionInvalid) {
		t.Errorf("refresh with the deleted user's session: error = %v, want ErrSessionInvalid", err)
	}
	if _, err := e.svc.Refresh(context.Background(), graceSession); err != nil {
		t.Errorf("another user's session stopped working: %v", err)
	}
	if _, ok := e.attempts.failures["ada@example.org"]; ok {
		t.Error("the failed logins were not cleared")
	}
	if err := login(e, "ada@example.org"); !errors.Is(err, domain.ErrInvalidCredentials) {
		t.Errorf("login after deletion: error = %v, want ErrInvalidCredentials", err)
	}
	if strings.Contains(e.logs.String(), loginPassword) {
		t.Error("the logs contain the password")
	}
}

func TestDeleteAccountRefused(t *testing.T) {
	long := strings.Repeat("x", MaxPasswordLength+1)
	for _, tt := range []struct {
		name, password string
	}{
		{"wrong password", "wrong-password"},
		{"empty password", ""},
		{"password longer than any stored one", long},
	} {
		t.Run(tt.name, func(t *testing.T) {
			e := newEnv()
			ada := verifiedUser(t, e, "ada@example.org")
			solo := e.store.addProject("Ada's notes", ada)
			txs := e.store.txCount

			if err := deleteAccount(e, ada, tt.password); !errors.Is(err, domain.ErrWrongPassword) {
				t.Fatalf("DeleteAccount() error = %v, want ErrWrongPassword", err)
			}
			if got := e.attempts.failures["ada@example.org"].count; got != 1 {
				t.Errorf("failed logins counted = %d, want 1", got)
			}
			if e.store.txCount != txs {
				t.Error("a transaction ran after a wrong password")
			}
			if _, ok := e.store.projects[solo]; !ok {
				t.Error("a refused deletion deleted a project")
			}
			if err := login(e, "ada@example.org"); err != nil {
				t.Errorf("login after a refused deletion: %v", err)
			}
		})
	}
}

// FR-2.9: the owner of a project that has other members must transfer or delete it first.
func TestDeleteAccountRefusedForOwnerOfSharedProject(t *testing.T) {
	e := newEnv()
	ada := verifiedUser(t, e, "ada@example.org")
	grace := verifiedUser(t, e, "grace@example.org")
	solo := e.store.addProject("Ada's notes", ada)
	beta := e.store.addProject("Beta survey", ada, grace)
	alpha := e.store.addProject("Alpha engine", ada, grace)
	e.store.pubs["solo-pub"] = []string{ada}

	err := deleteAccount(e, ada, loginPassword)
	var shared *domain.SharedProjectsError
	if !errors.As(err, &shared) || !errors.Is(err, domain.ErrOwnsSharedProject) {
		t.Fatalf("DeleteAccount() error = %v, want a SharedProjectsError", err)
	}
	want := []domain.ProjectRef{{ID: alpha, Title: "Alpha engine"}, {ID: beta, Title: "Beta survey"}}
	if !slices.Equal(shared.Projects, want) {
		t.Errorf("projects = %v, want %v", shared.Projects, want)
	}
	if _, err := e.store.FindUserByID(context.Background(), ada); err != nil {
		t.Errorf("the user was deleted: %v", err)
	}
	if _, ok := e.store.projects[solo]; !ok {
		t.Error("the solo project was deleted although the deletion was refused")
	}
	if _, ok := e.store.pubs["solo-pub"]; !ok {
		t.Error("a publication was deleted although the deletion was refused")
	}
	if got := e.attempts.failures["ada@example.org"].count; got != 0 {
		t.Errorf("failed logins counted = %d, want 0: the password was right", got)
	}
}

// A member who is not the owner does not block deletion; only the membership goes.
func TestDeleteAccountAsMemberOfSharedProject(t *testing.T) {
	e := newEnv()
	ada := verifiedUser(t, e, "ada@example.org")
	grace := verifiedUser(t, e, "grace@example.org")
	p := e.store.addProject("Grace's compiler", grace, ada)

	if err := deleteAccount(e, ada, loginPassword); err != nil {
		t.Fatalf("DeleteAccount() error = %v, want nil", err)
	}
	if got := e.store.projects[p].members; len(got) != 1 || got[grace] != "owner" {
		t.Errorf("members after deletion = %v, want only Grace as owner", got)
	}
}

func TestDeleteAccountWhileLocked(t *testing.T) {
	e := newEnv()
	ada := verifiedUser(t, e, "ada@example.org")
	failLogins(t, e, "ada@example.org", MaxFailedLogins)

	verified := e.hasher.verified
	var locked *domain.LockedError
	if err := deleteAccount(e, ada, loginPassword); !errors.As(err, &locked) {
		t.Fatalf("DeleteAccount() while locked: error = %v, want a LockedError", err)
	}
	if e.hasher.verified != verified {
		t.Error("the password was checked while the account was locked")
	}
	if e.attempts.failures["ada@example.org"].count != MaxFailedLogins {
		t.Error("an attempt while locked was counted")
	}
	if _, err := e.store.FindUserByID(context.Background(), ada); err != nil {
		t.Errorf("the user was deleted while locked: %v", err)
	}
}

// Wrong passwords here and at login share one counter (FR-2.7).
func TestDeleteAccountLocksOnFifthWrongPassword(t *testing.T) {
	e := newEnv()
	ada := verifiedUser(t, e, "ada@example.org")
	failLogins(t, e, "ada@example.org", MaxFailedLogins-1)

	if err := deleteAccount(e, ada, "wrong-password"); !errors.Is(err, domain.ErrWrongPassword) {
		t.Fatalf("DeleteAccount() error = %v, want ErrWrongPassword", err)
	}
	var locked *domain.LockedError
	if err := login(e, "ada@example.org"); !errors.As(err, &locked) {
		t.Errorf("login after the 5th failure: error = %v, want a LockedError", err)
	}
}

func TestDeleteAccountOfMissingUser(t *testing.T) {
	e := newEnv()
	if err := deleteAccount(e, "no-such-user", loginPassword); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("DeleteAccount() error = %v, want ErrNotFound", err)
	}
}

func TestDeleteAccountRollsBackOnFailure(t *testing.T) {
	e := newEnv()
	ada := verifiedUser(t, e, "ada@example.org")
	solo := e.store.addProject("Ada's notes", ada)
	failLogins(t, e, "ada@example.org", 2)

	e.store.failTx = errors.New("database down")
	if err := deleteAccount(e, ada, loginPassword); err == nil {
		t.Fatal("DeleteAccount() error = nil, want the store's error")
	}
	e.store.failTx = nil

	if _, err := e.store.FindUserByID(context.Background(), ada); err != nil {
		t.Errorf("the user was deleted by a failed deletion: %v", err)
	}
	if _, ok := e.store.projects[solo]; !ok {
		t.Error("a project was deleted by a failed deletion")
	}
	if got := e.attempts.failures["ada@example.org"].count; got != 2 {
		t.Errorf("failed logins = %d, want 2: a failed deletion must not clear them", got)
	}
}

func TestDeleteAccountWhenCounterFails(t *testing.T) {
	e := newEnv()
	ada := verifiedUser(t, e, "ada@example.org")
	e.attempts.err = errors.New("redis down")

	if err := deleteAccount(e, ada, "wrong-password"); !errors.Is(err, domain.ErrWrongPassword) {
		t.Errorf("wrong password: error = %v, want ErrWrongPassword", err)
	}
	if err := deleteAccount(e, ada, loginPassword); err != nil {
		t.Errorf("right password: error = %v, want nil: the counter must not fail a deletion", err)
	}
}

// DeleteAccountByID runs the same deletion as DeleteAccount, for maintenance commands.
func TestDeleteAccountByID(t *testing.T) {
	e := newEnv()
	ada := verifiedUser(t, e, "ada@example.org")
	grace := verifiedUser(t, e, "grace@example.org")
	adaSession := loggedIn(t, e, "ada@example.org")
	solo := e.store.addProject("Ada's notes", ada)
	e.store.pubs["solo-pub"] = []string{ada}
	e.store.pubs["shared-pub"] = []string{ada, grace}

	if err := e.svc.DeleteAccountByID(context.Background(), ada, "sample_purge"); err != nil {
		t.Fatalf("DeleteAccountByID() error = %v, want nil", err)
	}
	if _, err := e.store.FindUserByID(context.Background(), ada); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("find the deleted user: error = %v, want ErrNotFound", err)
	}
	if _, ok := e.store.projects[solo]; ok {
		t.Error("the project where Ada was the only member was not deleted")
	}
	if _, ok := e.store.pubs["solo-pub"]; ok {
		t.Error("a publication with no author left was not deleted")
	}
	if got := e.store.pubs["shared-pub"]; !slices.Equal(got, []string{grace}) {
		t.Errorf("shared publication authors = %v, want only Grace", got)
	}
	if _, err := e.svc.Refresh(context.Background(), adaSession); !errors.Is(err, domain.ErrSessionInvalid) {
		t.Errorf("refresh with the deleted user's session: error = %v, want ErrSessionInvalid", err)
	}
	if !strings.Contains(e.logs.String(), "sample_purge") {
		t.Error("the log does not say who deleted the account")
	}
	if _, err := e.store.FindUserByID(context.Background(), grace); err != nil {
		t.Errorf("another user was deleted: %v", err)
	}
}

func TestDeleteAccountByIDRefusedForOwnerOfSharedProject(t *testing.T) {
	e := newEnv()
	ada := verifiedUser(t, e, "ada@example.org")
	grace := verifiedUser(t, e, "grace@example.org")
	e.store.addProject("Beta survey", ada, grace)

	err := e.svc.DeleteAccountByID(context.Background(), ada, "sample_purge")
	var shared *domain.SharedProjectsError
	if !errors.As(err, &shared) {
		t.Fatalf("DeleteAccountByID() error = %v, want a SharedProjectsError", err)
	}
	if _, err := e.store.FindUserByID(context.Background(), ada); err != nil {
		t.Errorf("the user was deleted: %v", err)
	}
}

func TestDeleteAccountByIDOfMissingUser(t *testing.T) {
	e := newEnv()
	if err := e.svc.DeleteAccountByID(context.Background(), "no-such-user", "sample_purge"); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("DeleteAccountByID() error = %v, want ErrNotFound", err)
	}
}
