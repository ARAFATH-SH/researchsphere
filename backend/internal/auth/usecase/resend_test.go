package usecase

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/auth/domain"
)

func TestResendVerificationUnknownEmail(t *testing.T) {
	e := newEnv()
	if err := e.svc.ResendVerification(context.Background(), "nobody@example.org"); err != nil {
		t.Fatalf("ResendVerification() error = %v, want nil, as for a registered address", err)
	}
	if len(e.mailer.sent) != 0 || len(e.store.tokens) != 0 {
		t.Error("sent an email or stored a token for an unknown address")
	}
}

func TestResendVerificationVerifiedEmail(t *testing.T) {
	e := newEnv()
	token := registered(t, e, "ada@example.org")
	if err := e.svc.VerifyEmail(context.Background(), token); err != nil {
		t.Fatal(err)
	}
	if err := e.svc.ResendVerification(context.Background(), "ada@example.org"); err != nil {
		t.Fatalf("ResendVerification() error = %v, want nil", err)
	}
	if len(e.mailer.sent) != 1 {
		t.Errorf("emails sent = %d, want only the registration email", len(e.mailer.sent))
	}
}

func TestResendVerificationUnverifiedEmail(t *testing.T) {
	e := newEnv()
	ctx := context.Background()
	oldToken := registered(t, e, "ada@example.org")

	e.clock.t = start.Add(VerificationTTL + 1) // the first link has expired
	if err := e.svc.ResendVerification(ctx, "ADA@example.org"); err != nil {
		t.Fatalf("ResendVerification() error = %v", err)
	}
	if len(e.mailer.sent) != 2 {
		t.Fatalf("emails sent = %d, want 2", len(e.mailer.sent))
	}
	msg := e.mailer.sent[1]
	if msg.To != "ada@example.org" {
		t.Errorf("sent to %q, want the registered address", msg.To)
	}
	newTok := tokenFromLink(msg.Link)
	if newTok == oldToken {
		t.Fatal("the new link has the old token")
	}

	if err := e.svc.VerifyEmail(ctx, oldToken); !errors.Is(err, domain.ErrTokenInvalid) {
		t.Errorf("old token: error = %v, want ErrTokenInvalid", err)
	}
	if err := e.svc.VerifyEmail(ctx, newTok); err != nil {
		t.Errorf("new token: error = %v, want nil", err)
	}
}

func TestResendVerificationFailuresLookTheSame(t *testing.T) {
	t.Run("store lookup fails", func(t *testing.T) {
		e := newEnv()
		e.store.failFind = errors.New("database down")
		if err := e.svc.ResendVerification(context.Background(), "ada@example.org"); err != nil {
			t.Fatalf("error = %v, want nil", err)
		}
		if !strings.Contains(e.logs.String(), "database down") {
			t.Errorf("failure not logged:\n%s", e.logs)
		}
	})

	t.Run("token write fails", func(t *testing.T) {
		e := newEnv()
		registered(t, e, "ada@example.org")
		e.store.failTx = errors.New("disk full")
		if err := e.svc.ResendVerification(context.Background(), "ada@example.org"); err != nil {
			t.Fatalf("error = %v, want nil", err)
		}
		if len(e.mailer.sent) != 1 {
			t.Error("sent a link whose token was not stored")
		}
	})

	t.Run("email fails", func(t *testing.T) {
		e := newEnv()
		registered(t, e, "ada@example.org")
		e.mailer.err = errors.New("smtp down")
		if err := e.svc.ResendVerification(context.Background(), "ada@example.org"); err != nil {
			t.Fatalf("error = %v, want nil", err)
		}
	})
}

func TestResendVerificationMalformedEmail(t *testing.T) {
	err := newEnv().svc.ResendVerification(context.Background(), "not an address")
	if !errors.Is(err, domain.ErrInvalidEmail) {
		t.Fatalf("error = %v, want ErrInvalidEmail", err)
	}
}
