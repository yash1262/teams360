package email

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
)

type recordingSender struct {
	calls    []sentMail
	failFor  map[string]bool
	resetCnt int
}

type sentMail struct{ to, subject, body string }

func (r *recordingSender) SendHTML(_ context.Context, to, subject, body string) error {
	r.calls = append(r.calls, sentMail{to, subject, body})
	if r.failFor[to] {
		return errors.New("rejected")
	}
	return nil
}

func (r *recordingSender) SendPasswordResetEmail(context.Context, string, string) error {
	r.resetCnt++
	return nil
}

func TestRedirectTargetsParsing(t *testing.T) {
	cases := []struct {
		name       string
		raw        string
		want       []string
		configured bool
		wantErr    bool
	}{
		{"unset is not configured", "", nil, false, false},
		{"whitespace only is not configured", "   ", nil, false, false},
		{"one address", "a@x.com", []string{"a@x.com"}, true, false},
		{"comma, semicolon and space separators", "a@x.com, b@x.com;c@x.com d@x.com", []string{"a@x.com", "b@x.com", "c@x.com", "d@x.com"}, true, false},
		{"duplicates collapse, case-insensitively", "a@x.com,A@X.com", []string{"a@x.com"}, true, false},
		{"a typo is rejected", "a@x.com,not-an-email", nil, true, true},
		{"a display-name form is rejected", "Jane <jane@x.com>", nil, true, true},
		{"only separators is rejected", ",;,", nil, true, true},
	}
	for _, c := range cases {
		got, configured, err := RedirectTargets(c.raw)
		if configured != c.configured {
			t.Errorf("%s: configured = %v, want %v", c.name, configured, c.configured)
		}
		if (err != nil) != c.wantErr {
			t.Errorf("%s: err = %v, wantErr %v", c.name, err, c.wantErr)
		}
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: targets = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestRedirectSenderNeverUsesTheRealRecipient(t *testing.T) {
	inner := &recordingSender{}
	s := NewRedirectSender(inner, []string{"cw1@x.com", "cw2@x.com"})

	if err := s.SendHTML(context.Background(), "realteamlead@x.com", "Survey reminder", "<html><body><p>hi</p></body></html>"); err != nil {
		t.Fatal(err)
	}

	if len(inner.calls) != 2 {
		t.Fatalf("expected one copy per test address, got %d", len(inner.calls))
	}
	for _, c := range inner.calls {
		if c.to == "realteamlead@x.com" {
			t.Fatalf("the real recipient was passed to the real sender: %+v", c)
		}
	}
	if inner.calls[0].to != "cw1@x.com" || inner.calls[1].to != "cw2@x.com" {
		t.Errorf("unexpected recipients: %s, %s", inner.calls[0].to, inner.calls[1].to)
	}
}

func TestRedirectSenderNamesTheOriginalRecipient(t *testing.T) {
	inner := &recordingSender{}
	s := NewRedirectSender(inner, []string{"cw1@x.com"})

	_ = s.SendHTML(context.Background(), "real@x.com", "Survey reminder", "<html><body style=\"x\"><p>hi</p></body></html>")

	c := inner.calls[0]
	if !strings.HasPrefix(c.subject, "[TEST - for real@x.com] ") || !strings.HasSuffix(c.subject, "Survey reminder") {
		t.Errorf("subject = %q", c.subject)
	}
	if !strings.Contains(c.body, "originally addressed to <strong>real@x.com</strong>") {
		t.Errorf("body is missing the banner: %s", c.body)
	}
	if strings.Index(c.body, "TEST MODE") > strings.Index(c.body, "<p>hi</p>") {
		t.Error("the banner must come before the original content")
	}
	if !strings.Contains(c.body, `<body style="x">`) {
		t.Error("the original body tag must be kept intact")
	}
}

func TestRedirectBannerEscapesTheOriginalAddress(t *testing.T) {
	inner := &recordingSender{}
	s := NewRedirectSender(inner, []string{"cw1@x.com"})

	_ = s.SendHTML(context.Background(), `"><script>alert(1)</script>@x.com`, "s", "<body></body>")

	if strings.Contains(inner.calls[0].body, "<script>") {
		t.Errorf("the original address must be HTML-escaped: %s", inner.calls[0].body)
	}
}

func TestRedirectSenderStripsLineBreaksFromTheSubject(t *testing.T) {
	inner := &recordingSender{}
	s := NewRedirectSender(inner, []string{"cw1@x.com"})

	_ = s.SendHTML(context.Background(), "real@x.com\r\nBcc: evil@x.com", "s", "<body></body>")

	if strings.ContainsAny(inner.calls[0].subject, "\r\n") {
		t.Errorf("subject must not contain line breaks: %q", inner.calls[0].subject)
	}
}

func TestRedirectSenderKeepsGoingAfterOneTargetFails(t *testing.T) {
	inner := &recordingSender{failFor: map[string]bool{"cw1@x.com": true}}
	s := NewRedirectSender(inner, []string{"cw1@x.com", "cw2@x.com"})

	err := s.SendHTML(context.Background(), "real@x.com", "s", "<body></body>")

	if err == nil {
		t.Error("a failed delivery must be reported")
	}
	if len(inner.calls) != 2 {
		t.Errorf("the second address must still be tried, got %d calls", len(inner.calls))
	}
}

func TestRedirectSenderNeverForwardsPasswordResets(t *testing.T) {
	inner := &recordingSender{}
	s := NewRedirectSender(inner, []string{"cw1@x.com"})

	if err := s.SendPasswordResetEmail(context.Background(), "real@x.com", "secret-token"); err != nil {
		t.Fatal(err)
	}
	if inner.resetCnt != 0 || len(inner.calls) != 0 {
		t.Fatal("a password reset token must never reach a test address")
	}
}

func TestRedirectSenderReportsItsTargetsAndDelivery(t *testing.T) {
	s := NewRedirectSender(&recordingSender{}, []string{"cw1@x.com"})

	if got := RedirectTargetsOf(s); !reflect.DeepEqual(got, []string{"cw1@x.com"}) {
		t.Errorf("RedirectTargetsOf = %v", got)
	}
	if RedirectTargetsOf(&recordingSender{}) != nil {
		t.Error("an ordinary sender has no redirect targets")
	}
	if !Delivers(s) {
		t.Error("wrapping a delivering sender must still report delivery")
	}
	if Delivers(NewRedirectSender(LogSender{}, []string{"cw1@x.com"})) {
		t.Error("wrapping the log sender must not claim delivery")
	}
}
