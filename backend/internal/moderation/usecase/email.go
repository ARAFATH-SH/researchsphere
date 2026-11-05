package usecase

import (
	"fmt"
	"time"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/moderation/domain"
)

// The emails of FR-15.8. Each is made from the account and the reason only: the internal note
// is never passed to these functions, so it cannot reach a user.

const timeLayout = "2 January 2006 at 15:04 UTC"

func email(to domain.Account, subject, body string) domain.Email {
	return domain.Email{To: to.Email, FullName: to.FullName, Subject: subject, Body: fmt.Sprintf("Hello %s,\n\n%s\n", to.FullName, body)}
}

func warningEmail(to domain.Account, reason string) domain.Email {
	return email(to, "A warning about your ResearchSphere account", fmt.Sprintf(
		"An administrator has warned you about your use of ResearchSphere.\n\nReason:\n%s\n\nPlease log in to acknowledge this warning.", reason))
}

func restrictionEmail(to domain.Account, reason string, ends time.Time) domain.Email {
	return email(to, "You cannot send messages on ResearchSphere for now", fmt.Sprintf(
		"An administrator has stopped you sending messages and message requests until %s. Everything else keeps working.\n\nReason:\n%s",
		ends.UTC().Format(timeLayout), reason))
}

func liftEmail(to domain.Account, reason string) domain.Email {
	return email(to, "You can send messages on ResearchSphere again", fmt.Sprintf(
		"An administrator has ended the restriction on your account, so you can send messages again.\n\nReason:\n%s", reason))
}

func suspensionEmail(to domain.Account, reason string, until *time.Time) domain.Email {
	how := "permanently"
	if until != nil {
		how = "until " + until.UTC().Format(timeLayout)
	}
	return email(to, "Your ResearchSphere account is suspended", fmt.Sprintf(
		"An administrator has suspended your account %s. You cannot log in during this time.\n\nReason:\n%s", how, reason))
}

func restoreEmail(to domain.Account, reason string) domain.Email {
	return email(to, "Your ResearchSphere account is restored", fmt.Sprintf(
		"An administrator has ended the suspension of your account, so you can log in again.\n\nReason:\n%s", reason))
}
