package usecase

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/auth/domain"
)

func TestRegisterSuccess(t *testing.T) {
	e := newEnv()
	res, err := e.svc.Register(context.Background(), RegisterInput{
		FullName: "  Ada Lovelace ", Email: " Ada@Example.org ", Password: "correct horse",
	})
	if err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	if res.Email != "Ada@Example.org" {
		t.Errorf("Email = %q, want the trimmed address", res.Email)
	}

	u, ok := e.store.users["ada@example.org"]
	if !ok {
		t.Fatal("user not stored")
	}
	if u.FullName != "Ada Lovelace" {
		t.Errorf("profile name = %q, want %q", u.FullName, "Ada Lovelace")
	}
	if u.passwordHash != "hashed:correct horse" {
		t.Errorf("password stored as %q, want the hasher's output", u.passwordHash)
	}
	if u.Verified() {
		t.Error("new user is already verified")
	}
	if e.store.txCount != 1 {
		t.Errorf("transactions = %d, want 1 for user, profile and token", e.store.txCount)
	}

	if len(e.mailer.sent) != 1 {
		t.Fatalf("emails sent = %d, want 1", len(e.mailer.sent))
	}
	msg := e.mailer.sent[0]
	if msg.To != "Ada@Example.org" || msg.FullName != "Ada Lovelace" {
		t.Errorf("email to %q for %q", msg.To, msg.FullName)
	}
	if !strings.HasPrefix(msg.Link, "https://rs.example/verify-email#token=") {
		t.Errorf("link = %q, want the token in the fragment of /verify-email", msg.Link)
	}
	token := tokenFromLink(msg.Link)
	if !wellFormedToken(token) {
		t.Fatalf("token %q is not 32 random bytes in base64url", token)
	}

	tokens := e.store.tokensFor(u.ID)
	if len(tokens) != 1 {
		t.Fatalf("tokens stored = %d, want 1", len(tokens))
	}
	tok := tokens[0]
	if tok.hash == token || tok.hash != hashToken(token) {
		t.Errorf("stored %q, want the SHA-256 of the token and never the token", tok.hash)
	}
	if tok.purpose != domain.PurposeEmailVerification {
		t.Errorf("purpose = %q", tok.purpose)
	}
	if want := start.Add(VerificationTTL); !tok.ExpiresAt.Equal(want) {
		t.Errorf("expires %v, want %v", tok.ExpiresAt, want)
	}
	if strings.Contains(e.logs.String(), token) {
		t.Error("logs contain the token")
	}
}

func TestRegisterRefused(t *testing.T) {
	const ok = "long enough"
	tests := []struct {
		name    string
		in      RegisterInput
		wantErr error
	}{
		{"empty name", RegisterInput{FullName: "   ", Email: "a@example.org", Password: ok}, domain.ErrInvalidFullName},
		{"name too long", RegisterInput{FullName: strings.Repeat("é", MaxFullNameLength+1), Email: "a@example.org", Password: ok}, domain.ErrInvalidFullName},
		{"name with a line break", RegisterInput{FullName: "Ada\r\nBcc: x@evil.test", Email: "a@example.org", Password: ok}, domain.ErrInvalidFullName},
		{"empty email", RegisterInput{FullName: "Ada", Email: "", Password: ok}, domain.ErrInvalidEmail},
		{"email without at sign", RegisterInput{FullName: "Ada", Email: "ada.example.org", Password: ok}, domain.ErrInvalidEmail},
		{"email with a display name", RegisterInput{FullName: "Ada", Email: "Ada <a@example.org>", Password: ok}, domain.ErrInvalidEmail},
		{"email too long", RegisterInput{FullName: "Ada", Email: strings.Repeat("a", 250) + "@x.org", Password: ok}, domain.ErrInvalidEmail},
		{"password of 7 characters", RegisterInput{FullName: "Ada", Email: "a@example.org", Password: "1234567"}, domain.ErrPasswordTooShort},
		{"password of 7 multibyte characters", RegisterInput{FullName: "Ada", Email: "a@example.org", Password: "ääääääá"}, domain.ErrPasswordTooShort}, //nolint:gosec // test input, not a credential
		{"empty password", RegisterInput{FullName: "Ada", Email: "a@example.org", Password: ""}, domain.ErrPasswordTooShort},
		{"password too long", RegisterInput{FullName: "Ada", Email: "a@example.org", Password: strings.Repeat("x", MaxPasswordLength+1)}, domain.ErrPasswordTooLong},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newEnv()
			_, err := e.svc.Register(context.Background(), tt.in)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("Register() error = %v, want %v", err, tt.wantErr)
			}
			if len(e.store.users) != 0 || len(e.mailer.sent) != 0 {
				t.Error("a refused registration stored a user or sent an email")
			}
		})
	}
}

func TestRegisterAcceptsBoundaryLengths(t *testing.T) {
	tests := []struct {
		name string
		in   RegisterInput
	}{
		{"password of exactly 8 characters", RegisterInput{FullName: "Ada", Email: "a@example.org", Password: "12345678"}},
		{"password of 8 multibyte characters", RegisterInput{FullName: "Ada", Email: "a@example.org", Password: "pässwörd"}}, //nolint:gosec // test input, not a credential
		{"password of the maximum length", RegisterInput{FullName: "Ada", Email: "a@example.org", Password: strings.Repeat("x", MaxPasswordLength)}},
		{"name of the maximum length", RegisterInput{FullName: strings.Repeat("é", MaxFullNameLength), Email: "a@example.org", Password: "12345678"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := newEnv().svc.Register(context.Background(), tt.in); err != nil {
				t.Fatalf("Register() error = %v, want nil", err)
			}
		})
	}
}

func TestRegisterDuplicateEmailInAnotherCase(t *testing.T) {
	e := newEnv()
	ctx := context.Background()
	if _, err := e.svc.Register(ctx, RegisterInput{FullName: "Ada", Email: "ada@example.org", Password: "password1"}); err != nil {
		t.Fatal(err)
	}
	_, err := e.svc.Register(ctx, RegisterInput{FullName: "Other", Email: "ADA@Example.ORG", Password: "password2"})
	if !errors.Is(err, domain.ErrEmailTaken) {
		t.Fatalf("Register() error = %v, want ErrEmailTaken", err)
	}
	if len(e.store.users) != 1 || len(e.store.tokens) != 1 {
		t.Errorf("users = %d, tokens = %d, want 1 and 1", len(e.store.users), len(e.store.tokens))
	}
	if len(e.mailer.sent) != 1 {
		t.Errorf("emails sent = %d, want only the first registration's", len(e.mailer.sent))
	}
}

func TestRegisterSucceedsWhenEmailFails(t *testing.T) {
	e := newEnv()
	e.mailer.err = errors.New("smtp: connection refused")
	if _, err := e.svc.Register(context.Background(), RegisterInput{FullName: "Ada", Email: "a@example.org", Password: "password1"}); err != nil {
		t.Fatalf("Register() error = %v, want nil: the user can ask for a new link", err)
	}
	if len(e.store.users) != 1 {
		t.Error("user not stored")
	}
	if !strings.Contains(e.logs.String(), "send verification email") {
		t.Errorf("failure not logged:\n%s", e.logs)
	}
}

func TestRegisterStoreAndHasherErrors(t *testing.T) {
	in := RegisterInput{FullName: "Ada", Email: "a@example.org", Password: "password1"}

	t.Run("store fails", func(t *testing.T) {
		e := newEnv()
		e.store.failTx = errors.New("database down")
		if _, err := e.svc.Register(context.Background(), in); err == nil {
			t.Fatal("Register() error = nil, want the store's error")
		}
		if len(e.mailer.sent) != 0 {
			t.Error("email sent although nothing was stored")
		}
	})

	t.Run("hasher fails", func(t *testing.T) {
		e := newEnv()
		e.svc.hasher = fakeHasher{err: errors.New("out of memory")}
		if _, err := e.svc.Register(context.Background(), in); err == nil {
			t.Fatal("Register() error = nil, want the hasher's error")
		}
		if len(e.store.users) != 0 {
			t.Error("user stored without a password hash")
		}
	})
}
