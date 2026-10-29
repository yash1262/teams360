package email

import (
	"context"
	"fmt"
	"html"
	"net/mail"
	"strings"

	"github.com/agopalakrishnan/teams360/backend/pkg/logger"
)

// EnvRedirectTo lists the addresses every reminder email is delivered to
// INSTEAD of its real recipient, so a team can test real delivery without
// anyone outside the test receiving anything. Comma, semicolon or space
// separated.
const EnvRedirectTo = "EMAIL_REDIRECT_TO"

// RedirectTargets parses EMAIL_REDIRECT_TO. configured is false when the
// variable is empty or unset. An error means it is set but unusable (an entry
// that is not a plain email address, or nothing left after splitting); the
// caller must then send nothing rather than fall back to real recipients.
func RedirectTargets(raw string) (targets []string, configured bool, err error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, false, nil
	}

	seen := map[string]bool{}
	for _, token := range strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == ';' || r == ' ' || r == '\t' || r == '\n'
	}) {
		addr, perr := mail.ParseAddress(token)
		if perr != nil || addr.Address != token {
			return nil, true, fmt.Errorf("%s has an entry that is not a plain email address: %q", EnvRedirectTo, token)
		}
		key := strings.ToLower(token)
		if !seen[key] {
			seen[key] = true
			targets = append(targets, token)
		}
	}
	if len(targets) == 0 {
		return nil, true, fmt.Errorf("%s is set but contains no addresses", EnvRedirectTo)
	}
	return targets, true, nil
}

// RedirectSender wraps a real Sender and delivers every email to a fixed list
// of test addresses instead of the address it was meant for. The original
// recipient is named in the subject and in a banner at the top of the body,
// and is never passed to the wrapped sender.
type RedirectSender struct {
	inner Sender
	to    []string
}

// NewRedirectSender redirects everything sent through inner to the given
// addresses.
func NewRedirectSender(inner Sender, to []string) *RedirectSender {
	return &RedirectSender{inner: inner, to: append([]string(nil), to...)}
}

// RedirectTargets returns the addresses everything is delivered to.
func (r *RedirectSender) RedirectTargets() []string { return append([]string(nil), r.to...) }

// Delivers reports whether the wrapped sender delivers anything.
func (r *RedirectSender) Delivers() bool { return Delivers(r.inner) }

// SendHTML delivers one copy of the email to each test address.
func (r *RedirectSender) SendHTML(ctx context.Context, to, subject, htmlBody string) error {
	original := strings.NewReplacer("\r", "", "\n", "").Replace(to)
	testSubject := "[TEST - for " + original + "] " + subject
	testBody := withTestBanner(htmlBody, original)

	var firstErr error
	for _, target := range r.to {
		if err := r.inner.SendHTML(ctx, target, testSubject, testBody); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// SendPasswordResetEmail is never forwarded. A reset token is a credential
// for the real account, and sending it to a test address would hand that
// account to whoever reads the mailbox.
func (r *RedirectSender) SendPasswordResetEmail(_ context.Context, emailAddr, _ string) error {
	logger.Get().
		WithField("to", emailAddr).
		Warn("password reset email suppressed: EMAIL_REDIRECT_TO is set, and reset tokens are never redirected")
	return nil
}

// RedirectTargetsOf returns the test addresses a sender redirects to, or nil
// when it does not redirect.
func RedirectTargetsOf(s Sender) []string {
	if r, ok := s.(interface{ RedirectTargets() []string }); ok {
		return r.RedirectTargets()
	}
	return nil
}

// withTestBanner puts a notice naming the original recipient at the top of
// the email body.
func withTestBanner(body, original string) string {
	banner := `<div style="margin:0 0 16px;padding:12px 16px;background:#FEF3C7;border:1px solid #F59E0B;` +
		`color:#92400E;font:14px -apple-system,Segoe UI,Roboto,sans-serif;">TEST MODE: this email was originally ` +
		`addressed to <strong>` + html.EscapeString(original) + `</strong> and was redirected to you. ` +
		`The real recipient did not receive it.</div>`

	if i := strings.Index(strings.ToLower(body), "<body"); i >= 0 {
		if j := strings.Index(body[i:], ">"); j >= 0 {
			k := i + j + 1
			return body[:k] + banner + body[k:]
		}
	}
	return banner + body
}
