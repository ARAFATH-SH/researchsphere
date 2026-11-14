package main

import (
	"context"
	"io"
	"log/slog"
	"time"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/auth/adapter/smtp"
	messagingemail "github.com/ARAFATH-SH/researchsphere/backend/internal/messaging/adapter/email"
	messagingpg "github.com/ARAFATH-SH/researchsphere/backend/internal/messaging/adapter/postgres"
	messagingdomain "github.com/ARAFATH-SH/researchsphere/backend/internal/messaging/domain"
	messagingusecase "github.com/ARAFATH-SH/researchsphere/backend/internal/messaging/usecase"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/platform/config"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/platform/logging"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/platform/postgres"
)

// digestInterval is how often the unread message digest job runs (FR-16.11).
const digestInterval = time.Hour

// multipartMailer is the part of the SMTP adapter the digest uses.
type multipartMailer interface {
	SendMultipart(ctx context.Context, msg smtp.Message) error
}

// digestMailer renders messaging's digest and sends it through the SMTP adapter, with the
// one-click unsubscribe headers (RFC 8058).
type digestMailer struct {
	mailer     multipartMailer
	appBaseURL string
}

var _ messagingdomain.DigestMailer = digestMailer{}

func (m digestMailer) SendDigest(ctx context.Context, d messagingdomain.Digest) error {
	r, err := messagingemail.Render(d, m.appBaseURL)
	if err != nil {
		return err
	}
	return m.mailer.SendMultipart(ctx, smtp.Message{
		To: d.To, ToName: d.FullName, Subject: r.Subject, Text: r.Text, HTML: r.HTML,
		ListUnsubscribeURL: r.ListUnsubscribeURL,
	})
}

// newDigests is the settings and digest usecase of messaging (FR-16.11).
func newDigests(store *messagingpg.Store, tx messagingdomain.Transactor, mailer multipartMailer, cfg config.Config, logger *slog.Logger) *messagingusecase.Digests {
	return messagingusecase.NewDigests(store, tx, digestMailer{mailer: mailer, appBaseURL: cfg.AppBaseURL},
		cfg.UnsubscribeSecret, time.Now, logger)
}

// runDigestsOnce runs one pass of the unread message digest (FR-16.11) with the API's
// settings, logs what it did and returns. `make send-digests` runs it, so the job can be
// tried without waiting for the hourly loop.
func runDigestsOnce(ctx context.Context, getenv func(string) string, logOut io.Writer) error {
	cfg, err := config.Load(getenv)
	if err != nil {
		return err
	}
	logger := logging.New(logOut, cfg.LogLevel)
	pool, err := postgres.NewPool(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()
	digests := newDigests(messagingpg.NewStore(pool), postgres.NewTransactor(pool), newMailer(cfg), cfg, logger)
	run, err := digests.SendDue(ctx)
	if err != nil {
		return err
	}
	logger.Info("unread message digests finished", slog.Int("sent", run.Sent),
		slog.Int("skipped", run.Skipped), slog.Int("failed", run.Failed))
	return nil
}
