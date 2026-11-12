# Email delivery (FR-17.1)

The API sends every email (verification links, password resets, moderation notices) through
one SMTP server. Which server is a matter of settings only: no code changes to move from
Mailpit to a provider, or from one provider to another.

## Settings

| Variable | Meaning |
| --- | --- |
| `SMTP_HOST`, `SMTP_PORT` | The server. Locally Mailpit: `localhost`, `1025`. |
| `SMTP_TLS` | `starttls` (connect in plain text, then require STARTTLS; usually port 587), `tls` (TLS from the first byte; usually port 465) or `none` (plain text; Mailpit only). With `starttls`, a server that does not offer STARTTLS is an error, never a silent fall back to plain text. |
| `SMTP_USERNAME`, `SMTP_PASSWORD` | The provider's SMTP login, sent with AUTH PLAIN over the encrypted connection. Set both or neither. The password is never logged. |
| `MAIL_FROM` | The sender, such as `ResearchSphere <no-reply@mail.example.org>`. Its domain must be one you set up at the provider (below). Each message gets a `Message-ID` at this domain. |
| `APP_ENV` | `production` turns on the checks below. |

With `APP_ENV=production` the API **refuses to start** when `SMTP_TLS` is `none`, `SMTP_HOST`
is this machine (`localhost`, `127.0.0.1`, `::1`), or `SMTP_USERNAME` or `SMTP_PASSWORD` is
missing. The error names the variable, never its value.

## Switching provider

1. Create an account at the provider and add your sending domain (use a subdomain such as
   `mail.example.org`, so its reputation is separate from the main domain).
2. Publish the DNS records the provider shows you (below) and wait until it marks the domain
   verified.
3. Create SMTP credentials at the provider.
4. Set the variables and restart the API. Send yourself a password reset to check.

Examples (check the provider's current documentation; hosts and ports change):

| Provider | `SMTP_HOST` | `SMTP_PORT` / `SMTP_TLS` | `SMTP_USERNAME` | `SMTP_PASSWORD` |
| --- | --- | --- | --- | --- |
| Brevo | `smtp-relay.brevo.com` | `587` / `starttls` | your Brevo SMTP login | the SMTP key |
| Resend | `smtp.resend.com` | `465` / `tls` (or `587` / `starttls`) | `resend` | an API key |
| Amazon SES | `email-smtp.<region>.amazonaws.com` | `587` / `starttls` | the SMTP user name (not the IAM access key) | the SMTP password |

Free tiers come first (FR-17.1); moving to a paid plan of the same provider changes nothing
here.

## DNS records on the sending domain

Set these on the domain of `MAIL_FROM` (the DNS work itself is done in slice 6, with the
deployment). Without them, providers and inboxes treat the mail as spam or refuse it.

- **SPF** (TXT on the sending domain): which servers may send for it. Use the include the
  provider gives, for example `v=spf1 include:spf.brevo.com ~all`. A domain has exactly one
  SPF record; when two providers send for it, put both includes in that one record.
- **DKIM** (TXT or CNAME, at a name the provider gives such as
  `<selector>._domainkey.mail.example.org`): the public key the provider signs each message
  with. Copy it exactly as shown.
- **DMARC** (TXT at `_dmarc.<domain>`): what receivers do when SPF and DKIM fail, and where
  they send reports. Start with monitoring, then tighten:
  `v=DMARC1; p=none; rua=mailto:dmarc@example.org` → `p=quarantine` → `p=reject` once the
  reports show only your provider sending.
- Many providers also ask for a **return-path / bounce** CNAME (custom MAIL FROM); add it if
  they do, so SPF aligns with the sender domain.

## Local development

Nothing to set: the defaults send to Mailpit (`make up`), which shows every message at
http://localhost:8025 and delivers nothing. Leave `SMTP_TLS=none` and the login empty.
