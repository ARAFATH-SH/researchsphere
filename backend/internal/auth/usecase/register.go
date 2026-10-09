package usecase

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/auth/domain"
)

// RegisterInput is what a visitor enters to register (FR-1.1).
type RegisterInput struct {
	FullName string
	Email    string
	Password string
}

// RegisterResult tells the client where the verification link went.
type RegisterResult struct {
	Email string
}

// Register creates the user, the profile and a verification token in one transaction,
// then emails the link (FR-1.1 to FR-1.3). It returns ErrInvalidFullName,
// ErrInvalidEmail, ErrPasswordTooShort, ErrPasswordTooLong or ErrEmailTaken when the
// registration is refused (FR-1.2). A failure to send the email is logged, not
// returned: the account exists and the user can ask for a new link (FR-1.5).
func (s *Service) Register(ctx context.Context, in RegisterInput) (RegisterResult, error) {
	fullName, err := normalizeFullName(in.FullName)
	if err != nil {
		return RegisterResult{}, err
	}
	email, err := normalizeEmail(in.Email)
	if err != nil {
		return RegisterResult{}, err
	}
	if err := checkPassword(in.Password); err != nil {
		return RegisterResult{}, err
	}

	passwordHash, err := s.hasher.Hash(in.Password)
	if err != nil {
		return RegisterResult{}, fmt.Errorf("hash password: %w", err)
	}
	token, tokenHash := newToken()

	var userID string
	err = s.store.RunInTx(ctx, func(tx domain.Tx) error {
		var err error
		userID, err = tx.CreateUser(ctx, domain.NewUser{Email: email, FullName: fullName, PasswordHash: passwordHash})
		if err != nil {
			return err
		}
		if err := tx.CreateProfile(ctx, userID, fullName); err != nil {
			return err
		}
		return tx.CreateToken(ctx, domain.NewToken{
			UserID:    userID,
			Purpose:   domain.PurposeEmailVerification,
			Hash:      tokenHash,
			ExpiresAt: s.now().Add(VerificationTTL),
		})
	})
	if err != nil {
		return RegisterResult{}, err
	}

	s.sendVerification(ctx, userID, email, fullName, token)
	return RegisterResult{Email: email}, nil
}

// sendVerification emails the link and logs a failure without the link or token.
func (s *Service) sendVerification(ctx context.Context, userID, email, fullName, token string) {
	err := s.mailer.SendVerification(ctx, domain.VerificationEmail{
		To:       email,
		FullName: fullName,
		Link:     s.verificationLink(token),
	})
	if err != nil {
		s.logger.ErrorContext(ctx, "send verification email", slog.String("user_id", userID), slog.Any("error", err))
	}
}
