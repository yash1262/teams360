package commands

import (
	"context"
	"fmt"
	"time"

	"github.com/agopalakrishnan/teams360/backend/domain/healthcheck"
)

// SubmitHealthCheckCommand represents the command to submit a health check
type SubmitHealthCheckCommand struct {
	ID               string
	TeamID           string
	UserID           string
	Date             string
	AssessmentPeriod string
	SurveyType       string
	Responses        []HealthCheckResponseCommand
	Completed        bool
}

// HealthCheckResponseCommand represents a response in the command
type HealthCheckResponseCommand struct {
	DimensionID string
	Score       int
	Trend       string
	Comment     string
}

// SubmitHealthCheckHandler handles the submit health check command
type SubmitHealthCheckHandler struct {
	repository healthcheck.Repository
}

// NewSubmitHealthCheckHandler creates a new command handler
func NewSubmitHealthCheckHandler(repository healthcheck.Repository) *SubmitHealthCheckHandler {
	return &SubmitHealthCheckHandler{
		repository: repository,
	}
}

// Handle executes the command
func (h *SubmitHealthCheckHandler) Handle(cmd SubmitHealthCheckCommand) (*healthcheck.HealthCheckSession, error) {
	// Generate ID if not provided
	if cmd.ID == "" {
		cmd.ID = fmt.Sprintf("session-%d", time.Now().UnixNano())
	}

	// Validate command
	if err := h.validate(cmd); err != nil {
		return nil, fmt.Errorf("validation failed: %w", err)
	}

	// Default survey type
	surveyType := cmd.SurveyType
	if surveyType == "" {
		surveyType = healthcheck.SurveyTypeIndividual
	}

	// Reject a duplicate submission for the same calendar quarter before writing anything.
	// This is a friendly pre-check; the database's partial unique indexes (see the
	// "quarter duplicate prevention" migration) are the authoritative, race-safe guard
	// against two concurrent requests both passing this check.
	if quarter, year, ok := healthcheck.PeriodQuarter(cmd.AssessmentPeriod); ok && cmd.Completed {
		existing, err := h.repository.FindQuarterSubmission(context.Background(), healthcheck.QuarterSubmissionQuery{
			SurveyType: surveyType,
			TeamID:     cmd.TeamID,
			UserID:     cmd.UserID,
			Quarter:    quarter,
			Year:       year,
		})
		if err != nil {
			return nil, fmt.Errorf("failed to check for duplicate submission: %w", err)
		}
		if existing != nil {
			return nil, healthcheck.NewDuplicateSubmissionError(surveyType, cmd.AssessmentPeriod)
		}
	}

	// Reject an Individual Survey submission for the calendar quarter immediately adjacent to
	// the user's last completed Individual Survey submission (the "no consecutive quarters"
	// rule) -- at least one full quarter must be skipped between submissions. Post-Workshop
	// submissions are scoped per team and are not subject to this rule.
	if surveyType == healthcheck.SurveyTypeIndividual && cmd.Completed {
		if quarter, year, ok := healthcheck.PeriodQuarter(cmd.AssessmentPeriod); ok {
			latest, err := h.repository.FindLatestIndividualSubmission(context.Background(), cmd.UserID)
			if err != nil {
				return nil, fmt.Errorf("failed to check for consecutive-quarter submission: %w", err)
			}
			if latest != nil {
				if lastQuarter, lastYear, lastOK := healthcheck.PeriodQuarter(latest.AssessmentPeriod); lastOK &&
					healthcheck.IsConsecutiveQuarter(lastQuarter, lastYear, quarter, year) {
					return nil, healthcheck.NewConsecutiveQuarterError(lastQuarter, lastYear)
				}
			}
		}
	}

	// Convert command to domain model
	session := &healthcheck.HealthCheckSession{
		ID:               cmd.ID,
		TeamID:           cmd.TeamID,
		UserID:           cmd.UserID,
		Date:             cmd.Date,
		AssessmentPeriod: cmd.AssessmentPeriod,
		SurveyType:       surveyType,
		Responses:        make([]healthcheck.HealthCheckResponse, len(cmd.Responses)),
		Completed:        cmd.Completed,
	}

	for i, resp := range cmd.Responses {
		session.Responses[i] = healthcheck.HealthCheckResponse{
			DimensionID: resp.DimensionID,
			Score:       resp.Score,
			Trend:       resp.Trend,
			Comment:     resp.Comment,
		}
	}

	// Save to repository
	if err := h.repository.Save(context.Background(), session); err != nil {
		return nil, fmt.Errorf("failed to save session: %w", err)
	}

	return session, nil
}

// validate ensures the command is valid
func (h *SubmitHealthCheckHandler) validate(cmd SubmitHealthCheckCommand) error {
	if cmd.TeamID == "" {
		return fmt.Errorf("teamId is required")
	}

	if cmd.UserID == "" {
		return fmt.Errorf("userId is required")
	}

	if cmd.Date == "" {
		return fmt.Errorf("date is required")
	}

	if cmd.SurveyType != "" && cmd.SurveyType != healthcheck.SurveyTypeIndividual && cmd.SurveyType != healthcheck.SurveyTypePostWorkshop {
		return fmt.Errorf("surveyType must be 'individual' or 'post_workshop'")
	}

	if len(cmd.Responses) == 0 {
		return fmt.Errorf("responses cannot be empty")
	}

	// Validate each response
	for i, resp := range cmd.Responses {
		if resp.DimensionID == "" {
			return fmt.Errorf("response %d: dimensionId is required", i)
		}

		if resp.Score < 1 || resp.Score > 3 {
			return fmt.Errorf("response %d: score must be between 1 and 3", i)
		}

		if resp.Trend != "improving" && resp.Trend != "stable" && resp.Trend != "declining" {
			return fmt.Errorf("response %d: trend must be 'improving', 'stable', or 'declining'", i)
		}
	}

	return nil
}
