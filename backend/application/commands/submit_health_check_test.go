package commands

import (
	"context"
	"errors"
	"testing"

	"github.com/agopalakrishnan/teams360/backend/domain/healthcheck"
)

// fakeHealthCheckRepository is an in-memory healthcheck.Repository used to unit test the
// submit command's duplicate-quarter pre-check without a live Postgres database. Save()
// mirrors the database's partial-unique-index behavior (see the "quarter duplicate
// prevention" migration): a second completed save for the same scope/quarter/year fails.
type fakeHealthCheckRepository struct {
	sessions []*healthcheck.HealthCheckSession
}

func (f *fakeHealthCheckRepository) Save(_ context.Context, session *healthcheck.HealthCheckSession) error {
	if session.Completed {
		if quarter, year, ok := healthcheck.PeriodQuarter(session.AssessmentPeriod); ok {
			for _, existing := range f.sessions {
				if !existing.Completed || existing.ID == session.ID {
					continue
				}
				existingQuarter, existingYear, existingOK := healthcheck.PeriodQuarter(existing.AssessmentPeriod)
				if !existingOK || existingQuarter != quarter || existingYear != year || existing.SurveyType != session.SurveyType {
					continue
				}
				sameScope := (session.SurveyType == healthcheck.SurveyTypePostWorkshop && existing.TeamID == session.TeamID) ||
					(session.SurveyType != healthcheck.SurveyTypePostWorkshop && existing.UserID == session.UserID)
				if sameScope {
					return healthcheck.NewDuplicateSubmissionError(session.SurveyType, session.AssessmentPeriod)
				}
			}
		}
	}
	f.sessions = append(f.sessions, session)
	return nil
}

func (f *fakeHealthCheckRepository) FindQuarterSubmission(_ context.Context, query healthcheck.QuarterSubmissionQuery) (*healthcheck.HealthCheckSession, error) {
	for _, existing := range f.sessions {
		if !existing.Completed || existing.SurveyType != query.SurveyType {
			continue
		}
		quarter, year, ok := healthcheck.PeriodQuarter(existing.AssessmentPeriod)
		if !ok || quarter != query.Quarter || year != query.Year {
			continue
		}
		if query.SurveyType == healthcheck.SurveyTypePostWorkshop {
			if existing.TeamID == query.TeamID {
				return existing, nil
			}
		} else if existing.UserID == query.UserID {
			return existing, nil
		}
	}
	return nil, nil
}

func (f *fakeHealthCheckRepository) FindLatestIndividualSubmission(_ context.Context, userID string) (*healthcheck.HealthCheckSession, error) {
	var latest *healthcheck.HealthCheckSession
	var latestQuarter, latestYear int
	for _, existing := range f.sessions {
		if !existing.Completed || existing.SurveyType != healthcheck.SurveyTypeIndividual || existing.UserID != userID {
			continue
		}
		quarter, year, ok := healthcheck.PeriodQuarter(existing.AssessmentPeriod)
		if !ok {
			continue
		}
		if latest == nil || year*4+quarter > latestYear*4+latestQuarter {
			latest = existing
			latestQuarter, latestYear = quarter, year
		}
	}
	return latest, nil
}

func (f *fakeHealthCheckRepository) FindByID(context.Context, string) (*healthcheck.HealthCheckSession, error) {
	return nil, errors.New("not implemented")
}
func (f *fakeHealthCheckRepository) FindByTeamID(context.Context, string) ([]*healthcheck.HealthCheckSession, error) {
	return nil, nil
}
func (f *fakeHealthCheckRepository) FindByUserID(context.Context, string) ([]*healthcheck.HealthCheckSession, error) {
	return nil, nil
}
func (f *fakeHealthCheckRepository) FindByAssessmentPeriod(context.Context, string) ([]*healthcheck.HealthCheckSession, error) {
	return nil, nil
}
func (f *fakeHealthCheckRepository) Delete(context.Context, string) error { return nil }
func (f *fakeHealthCheckRepository) FindTeamHealthByManager(context.Context, string, string) ([]healthcheck.TeamHealthSummary, error) {
	return nil, nil
}
func (f *fakeHealthCheckRepository) FindAggregatedDimensionsByManager(context.Context, string, string) ([]healthcheck.DimensionSummary, error) {
	return nil, nil
}
func (f *fakeHealthCheckRepository) GetTeamSubmissionStatus(context.Context, string, string) (*healthcheck.TeamSubmissionStatus, error) {
	return nil, nil
}
func (f *fakeHealthCheckRepository) FindDistinctAssessmentPeriods(context.Context) ([]string, error) {
	return nil, nil
}

func baseCommand(overrides func(*SubmitHealthCheckCommand)) SubmitHealthCheckCommand {
	cmd := SubmitHealthCheckCommand{
		TeamID:           "team-1",
		UserID:           "user-1",
		Date:             "2026-01-15T10:00:00Z",
		AssessmentPeriod: "2026 Q1",
		SurveyType:       healthcheck.SurveyTypeIndividual,
		Completed:        true,
		Responses: []HealthCheckResponseCommand{
			{DimensionID: "mission", Score: 3, Trend: "stable"},
		},
	}
	if overrides != nil {
		overrides(&cmd)
	}
	return cmd
}

func TestSubmitHealthCheck_BlocksDuplicateIndividualSubmission(t *testing.T) {
	repo := &fakeHealthCheckRepository{}
	handler := NewSubmitHealthCheckHandler(repo)

	if _, err := handler.Handle(baseCommand(nil)); err != nil {
		t.Fatalf("first submission should succeed, got error: %v", err)
	}

	_, err := handler.Handle(baseCommand(func(c *SubmitHealthCheckCommand) { c.ID = "session-2" }))
	var dupErr *healthcheck.DuplicateSubmissionError
	if !errors.As(err, &dupErr) {
		t.Fatalf("expected DuplicateSubmissionError, got: %v", err)
	}
	if dupErr.SubmittedPeriod != "Q1 2026" || dupErr.NextEligiblePeriod != "Q3 2026" {
		t.Errorf("unexpected duplicate error periods: %+v", dupErr)
	}
}

func TestSubmitHealthCheck_BlocksDuplicatePostWorkshopSubmission(t *testing.T) {
	repo := &fakeHealthCheckRepository{}
	handler := NewSubmitHealthCheckHandler(repo)

	postWorkshop := func(c *SubmitHealthCheckCommand) { c.SurveyType = healthcheck.SurveyTypePostWorkshop }

	if _, err := handler.Handle(baseCommand(postWorkshop)); err != nil {
		t.Fatalf("first post-workshop submission should succeed, got error: %v", err)
	}

	_, err := handler.Handle(baseCommand(func(c *SubmitHealthCheckCommand) {
		postWorkshop(c)
		c.ID = "session-2"
		c.UserID = "different-lead" // a different Team Lead submitting for the same team
	}))
	var dupErr *healthcheck.DuplicateSubmissionError
	if !errors.As(err, &dupErr) {
		t.Fatalf("expected DuplicateSubmissionError for same team+quarter, got: %v", err)
	}
}

func TestSubmitHealthCheck_AllowsEligibleSubmission(t *testing.T) {
	repo := &fakeHealthCheckRepository{}
	handler := NewSubmitHealthCheckHandler(repo)

	if _, err := handler.Handle(baseCommand(nil)); err != nil {
		t.Fatalf("first submission should succeed, got error: %v", err)
	}

	// Q1 -> Q3 is eligible.
	_, err := handler.Handle(baseCommand(func(c *SubmitHealthCheckCommand) {
		c.ID = "session-2"
		c.AssessmentPeriod = "2026 Q3"
	}))
	if err != nil {
		t.Errorf("expected Q3 submission to be allowed after Q1, got error: %v", err)
	}
}

func TestSubmitHealthCheck_AllowsDifferentYear(t *testing.T) {
	repo := &fakeHealthCheckRepository{}
	handler := NewSubmitHealthCheckHandler(repo)

	if _, err := handler.Handle(baseCommand(nil)); err != nil {
		t.Fatalf("first submission should succeed, got error: %v", err)
	}

	_, err := handler.Handle(baseCommand(func(c *SubmitHealthCheckCommand) {
		c.ID = "session-2"
		c.AssessmentPeriod = "2027 Q1"
	}))
	if err != nil {
		t.Errorf("expected same quarter in a different year to be allowed, got error: %v", err)
	}
}

func TestSubmitHealthCheck_DoesNotBlockDifferentUserIndividualSubmission(t *testing.T) {
	repo := &fakeHealthCheckRepository{}
	handler := NewSubmitHealthCheckHandler(repo)

	if _, err := handler.Handle(baseCommand(nil)); err != nil {
		t.Fatalf("first submission should succeed, got error: %v", err)
	}

	_, err := handler.Handle(baseCommand(func(c *SubmitHealthCheckCommand) {
		c.ID = "session-2"
		c.UserID = "user-2"
	}))
	if err != nil {
		t.Errorf("a different user's individual submission must not be blocked, got error: %v", err)
	}
}

func TestSubmitHealthCheck_SeparatesIndividualAndPostWorkshopRecords(t *testing.T) {
	repo := &fakeHealthCheckRepository{}
	handler := NewSubmitHealthCheckHandler(repo)

	if _, err := handler.Handle(baseCommand(nil)); err != nil {
		t.Fatalf("individual submission should succeed, got error: %v", err)
	}

	// Same team, same user, same quarter -- but a different survey type -- must be allowed.
	_, err := handler.Handle(baseCommand(func(c *SubmitHealthCheckCommand) {
		c.ID = "session-2"
		c.SurveyType = healthcheck.SurveyTypePostWorkshop
	}))
	if err != nil {
		t.Errorf("post-workshop submission must not be blocked by an individual submission, got error: %v", err)
	}
}

func TestSubmitHealthCheck_MatchesPostWorkshopByTeamNotUser(t *testing.T) {
	repo := &fakeHealthCheckRepository{}
	handler := NewSubmitHealthCheckHandler(repo)

	postWorkshop := func(c *SubmitHealthCheckCommand) { c.SurveyType = healthcheck.SurveyTypePostWorkshop }
	if _, err := handler.Handle(baseCommand(postWorkshop)); err != nil {
		t.Fatalf("first post-workshop submission should succeed, got error: %v", err)
	}

	// A different team in the same quarter must not be blocked.
	_, err := handler.Handle(baseCommand(func(c *SubmitHealthCheckCommand) {
		postWorkshop(c)
		c.ID = "session-2"
		c.TeamID = "team-2"
	}))
	if err != nil {
		t.Errorf("a different team's post-workshop submission must not be blocked, got error: %v", err)
	}
}

func TestSubmitHealthCheck_IgnoresIncompleteDraftsForDuplicateCheck(t *testing.T) {
	repo := &fakeHealthCheckRepository{}
	handler := NewSubmitHealthCheckHandler(repo)

	_, err := handler.Handle(baseCommand(func(c *SubmitHealthCheckCommand) { c.Completed = false }))
	if err != nil {
		t.Fatalf("incomplete draft save should succeed, got error: %v", err)
	}

	_, err = handler.Handle(baseCommand(func(c *SubmitHealthCheckCommand) { c.ID = "session-2" }))
	if err != nil {
		t.Errorf("a completed submission must not be blocked by an incomplete draft, got error: %v", err)
	}
}

func TestSubmitHealthCheck_BlocksConsecutiveQuarterSubmission(t *testing.T) {
	cases := []struct {
		name        string
		from        string
		to          string
		wantLast    string
		wantNextElg string
	}{
		{"Q1 to Q2 blocked", "2026 Q1", "2026 Q2", "Q1 2026", "Q3 2026"},
		{"Q2 to Q3 blocked", "2026 Q2", "2026 Q3", "Q2 2026", "Q4 2026"},
		{"Q3 to Q4 blocked", "2026 Q3", "2026 Q4", "Q3 2026", "Q1 2027"},
		{"Q4 to Q1 next year blocked", "2025 Q4", "2026 Q1", "Q4 2025", "Q2 2026"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := &fakeHealthCheckRepository{}
			handler := NewSubmitHealthCheckHandler(repo)

			if _, err := handler.Handle(baseCommand(func(c *SubmitHealthCheckCommand) { c.AssessmentPeriod = tc.from })); err != nil {
				t.Fatalf("first submission for %q should succeed, got error: %v", tc.from, err)
			}

			_, err := handler.Handle(baseCommand(func(c *SubmitHealthCheckCommand) {
				c.ID = "session-2"
				c.AssessmentPeriod = tc.to
			}))
			var consecErr *healthcheck.ConsecutiveQuarterError
			if !errors.As(err, &consecErr) {
				t.Fatalf("expected ConsecutiveQuarterError for %s -> %s, got: %v", tc.from, tc.to, err)
			}
			if consecErr.LastSubmittedPeriod != tc.wantLast || consecErr.NextEligiblePeriod != tc.wantNextElg {
				t.Errorf("unexpected consecutive-quarter error periods: %+v, want last=%s next=%s",
					consecErr, tc.wantLast, tc.wantNextElg)
			}
		})
	}
}

func TestSubmitHealthCheck_AllowsSkippingAFullQuarter(t *testing.T) {
	cases := []struct {
		name string
		from string
		to   string
	}{
		{"Q1 to Q3 allowed", "2026 Q1", "2026 Q3"},
		{"Q2 to Q4 allowed", "2026 Q2", "2026 Q4"},
		{"Q1 to Q4 allowed", "2026 Q1", "2026 Q4"},
		{"Q3 to Q1 next year allowed", "2025 Q3", "2026 Q1"},
		{"Q4 to Q2 next year allowed", "2025 Q4", "2026 Q2"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := &fakeHealthCheckRepository{}
			handler := NewSubmitHealthCheckHandler(repo)

			if _, err := handler.Handle(baseCommand(func(c *SubmitHealthCheckCommand) { c.AssessmentPeriod = tc.from })); err != nil {
				t.Fatalf("first submission for %q should succeed, got error: %v", tc.from, err)
			}

			_, err := handler.Handle(baseCommand(func(c *SubmitHealthCheckCommand) {
				c.ID = "session-2"
				c.AssessmentPeriod = tc.to
			}))
			if err != nil {
				t.Errorf("expected %s submission to be allowed after %s, got error: %v", tc.to, tc.from, err)
			}
		})
	}
}

func TestSubmitHealthCheck_ConsecutiveQuarterCheckUsesMostRecentSubmission(t *testing.T) {
	repo := &fakeHealthCheckRepository{}
	handler := NewSubmitHealthCheckHandler(repo)

	// User submits Q1, then eligible Q3.
	if _, err := handler.Handle(baseCommand(func(c *SubmitHealthCheckCommand) { c.AssessmentPeriod = "2026 Q1" })); err != nil {
		t.Fatalf("Q1 submission should succeed, got error: %v", err)
	}
	if _, err := handler.Handle(baseCommand(func(c *SubmitHealthCheckCommand) {
		c.ID = "session-2"
		c.AssessmentPeriod = "2026 Q3"
	})); err != nil {
		t.Fatalf("Q3 submission should succeed, got error: %v", err)
	}

	// Now the most recent submission is Q3, so Q4 (consecutive to Q3) must be blocked even
	// though it is not consecutive to the older Q1 submission.
	_, err := handler.Handle(baseCommand(func(c *SubmitHealthCheckCommand) {
		c.ID = "session-3"
		c.AssessmentPeriod = "2026 Q4"
	}))
	var consecErr *healthcheck.ConsecutiveQuarterError
	if !errors.As(err, &consecErr) {
		t.Fatalf("expected Q4 to be blocked as consecutive to the most recent Q3 submission, got: %v", err)
	}
}

func TestSubmitHealthCheck_ConsecutiveQuarterCheckOnlyAppliesToIndividualSurveys(t *testing.T) {
	repo := &fakeHealthCheckRepository{}
	handler := NewSubmitHealthCheckHandler(repo)

	postWorkshop := func(c *SubmitHealthCheckCommand) { c.SurveyType = healthcheck.SurveyTypePostWorkshop }

	if _, err := handler.Handle(baseCommand(func(c *SubmitHealthCheckCommand) {
		postWorkshop(c)
		c.AssessmentPeriod = "2026 Q1"
	})); err != nil {
		t.Fatalf("first post-workshop submission should succeed, got error: %v", err)
	}

	// Consecutive quarter (Q2) for post-workshop must not be blocked -- the restriction is
	// Individual-Survey-only.
	_, err := handler.Handle(baseCommand(func(c *SubmitHealthCheckCommand) {
		postWorkshop(c)
		c.ID = "session-2"
		c.AssessmentPeriod = "2026 Q2"
	}))
	if err != nil {
		t.Errorf("post-workshop submissions must not be subject to the consecutive-quarter rule, got error: %v", err)
	}
}

func TestSubmitHealthCheck_MissingOrInvalidAssessmentPeriodSkipsQuarterCheck(t *testing.T) {
	// An unparseable (or empty) assessment period can't be resolved to a quarter/year, so
	// the duplicate-quarter rule must not apply -- even for what would otherwise look like
	// an exact repeat of the same user/team/survey-type submission.
	for _, period := range []string{"", "not-a-quarter"} {
		t.Run("period="+period, func(t *testing.T) {
			repo := &fakeHealthCheckRepository{}
			handler := NewSubmitHealthCheckHandler(repo)

			if _, err := handler.Handle(baseCommand(func(c *SubmitHealthCheckCommand) {
				c.AssessmentPeriod = period
			})); err != nil {
				t.Fatalf("first submission with period %q should succeed, got: %v", period, err)
			}

			_, err := handler.Handle(baseCommand(func(c *SubmitHealthCheckCommand) {
				c.ID = "session-2"
				c.AssessmentPeriod = period
			}))
			if err != nil {
				t.Errorf("submission with unparseable period %q should not fail duplicate check, got: %v", period, err)
			}
		})
	}
}
