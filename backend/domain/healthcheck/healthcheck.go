package healthcheck

import (
	"context"
	"fmt"
)

// Survey type constants
const (
	SurveyTypeIndividual   = "individual"
	SurveyTypePostWorkshop = "post_workshop"
)

// HealthCheckResponse represents a single dimension response
type HealthCheckResponse struct {
	DimensionID string `json:"dimensionId"`
	Score       int    `json:"score"` // 1 = red, 2 = yellow, 3 = green
	Trend       string `json:"trend"` // improving, stable, declining
	Comment     string `json:"comment,omitempty"`
}

// HealthCheckSession represents a completed health check
// This is an aggregate root in DDD terms
type HealthCheckSession struct {
	ID               string                `json:"id"`
	TeamID           string                `json:"teamId"`
	UserID           string                `json:"userId"`
	Date             string                `json:"date"`
	AssessmentPeriod string                `json:"assessmentPeriod,omitempty"`
	SurveyType       string                `json:"surveyType,omitempty"`
	Responses        []HealthCheckResponse `json:"responses"`
	Completed        bool                  `json:"completed"`
}

// TeamHealthSummary represents aggregated health data for a team
type TeamHealthSummary struct {
	TeamID             string             `json:"teamId"`
	TeamName           string             `json:"teamName"`
	SubmissionCount    int                `json:"submissionCount"`
	OverallHealth      float64            `json:"overallHealth"`
	Dimensions         []DimensionSummary `json:"dimensions"`
	PostWorkshopStatus string             `json:"postWorkshopStatus,omitempty"`
}

// TeamSubmissionStatus represents the submission status of a team for an assessment period
type TeamSubmissionStatus struct {
	TeamID             string `json:"teamId"`
	AssessmentPeriod   string `json:"assessmentPeriod"`
	TotalMembers       int    `json:"totalMembers"`
	SubmittedMembers   int    `json:"submittedMembers"`
	AllSubmitted       bool   `json:"allSubmitted"`
	PostWorkshopExists bool   `json:"postWorkshopExists"`
}

// QuarterSubmissionQuery looks up an existing completed submission for the same
// calendar quarter/year as a target assessment period, scoped by survey type:
//   - individual:   scoped to UserID only (a different user's submission never blocks).
//   - post_workshop: scoped to TeamID only (one workshop consensus per team per quarter).
type QuarterSubmissionQuery struct {
	SurveyType string
	TeamID     string // required when SurveyType == SurveyTypePostWorkshop
	UserID     string // required when SurveyType == SurveyTypeIndividual
	Quarter    int
	Year       int
}

// DuplicateSubmissionError indicates a survey was already submitted for the same
// calendar quarter/year as the requested assessment period.
type DuplicateSubmissionError struct {
	SurveyType         string
	SubmittedPeriod    string
	NextEligiblePeriod string
}

func (e *DuplicateSubmissionError) Error() string {
	return fmt.Sprintf(
		"You have already submitted the %s for %s. Your next submission will be available in %s.",
		SurveyTypeLabel(e.SurveyType), e.SubmittedPeriod, e.NextEligiblePeriod,
	)
}

// ConsecutiveQuarterError indicates an Individual Survey submission was attempted for the
// calendar quarter immediately adjacent to the user's last completed Individual Survey
// submission. The rule requires at least one full quarter to be skipped between submissions.
type ConsecutiveQuarterError struct {
	LastSubmittedPeriod string
	NextEligiblePeriod  string
}

func (e *ConsecutiveQuarterError) Error() string {
	return fmt.Sprintf(
		"You cannot submit the survey in consecutive quarters. Your next eligible submission will be available in %s.",
		e.NextEligiblePeriod,
	)
}

// NewConsecutiveQuarterError builds a ConsecutiveQuarterError from the quarter/year of the
// user's last Individual Survey submission, computing the next eligible quarter from it.
func NewConsecutiveQuarterError(lastQuarter, lastYear int) *ConsecutiveQuarterError {
	nextQuarter, nextYear := NextEligibleQuarter(lastQuarter, lastYear)
	return &ConsecutiveQuarterError{
		LastSubmittedPeriod: FormatQuarterPeriod(lastQuarter, lastYear),
		NextEligiblePeriod:  FormatQuarterPeriod(nextQuarter, nextYear),
	}
}

// SurveyTypeLabel renders a survey type constant as a user-facing label.
func SurveyTypeLabel(surveyType string) string {
	if surveyType == SurveyTypePostWorkshop {
		return "Post-Workshop Survey"
	}
	return "Individual Survey"
}

// NewDuplicateSubmissionError builds a DuplicateSubmissionError for the given survey type
// and the assessment period the caller just tried (and failed) to submit for. Both the
// submitted and next-eligible periods are rendered in "Qn YYYY" form (regardless of the
// team's actual cadence label) since the duplicate-submission rule is quarter-based.
func NewDuplicateSubmissionError(surveyType string, assessmentPeriod string) *DuplicateSubmissionError {
	submitted := assessmentPeriod
	next := assessmentPeriod
	if quarter, year, ok := PeriodQuarter(assessmentPeriod); ok {
		submitted = FormatQuarterPeriod(quarter, year)
		nextQuarter, nextYear := NextEligibleQuarter(quarter, year)
		next = FormatQuarterPeriod(nextQuarter, nextYear)
	}
	return &DuplicateSubmissionError{
		SurveyType:         surveyType,
		SubmittedPeriod:    submitted,
		NextEligiblePeriod: next,
	}
}

// DimensionSummary represents aggregated dimension health
type DimensionSummary struct {
	DimensionID   string  `json:"dimensionId"`
	AvgScore      float64 `json:"avgScore"`
	ResponseCount int     `json:"responseCount"`
}

// Repository defines the interface for health check data access
type Repository interface {
	FindByID(ctx context.Context, id string) (*HealthCheckSession, error)
	FindByTeamID(ctx context.Context, teamID string) ([]*HealthCheckSession, error)
	FindByUserID(ctx context.Context, userID string) ([]*HealthCheckSession, error)
	FindByAssessmentPeriod(ctx context.Context, period string) ([]*HealthCheckSession, error)
	Save(ctx context.Context, session *HealthCheckSession) error
	Delete(ctx context.Context, id string) error

	// Advanced queries for manager dashboard
	FindTeamHealthByManager(ctx context.Context, managerID string, assessmentPeriod string) ([]TeamHealthSummary, error)
	FindAggregatedDimensionsByManager(ctx context.Context, managerID string, assessmentPeriod string) ([]DimensionSummary, error)

	// Team submission status for post-workshop survey
	GetTeamSubmissionStatus(ctx context.Context, teamID string, assessmentPeriod string) (*TeamSubmissionStatus, error)

	// FindDistinctAssessmentPeriods returns all unique assessment periods from submitted sessions
	FindDistinctAssessmentPeriods(ctx context.Context) ([]string, error)

	// FindQuarterSubmission returns the existing completed submission (if any) matching the
	// given scope for the same calendar quarter/year, or nil if none exists.
	FindQuarterSubmission(ctx context.Context, query QuarterSubmissionQuery) (*HealthCheckSession, error)

	// FindLatestIndividualSubmission returns the user's most recent completed Individual
	// Survey submission (by calendar quarter/year), or nil if the user has never submitted
	// one. Used to enforce the consecutive-quarter submission restriction, which depends on
	// the user's most recent submission regardless of which quarter it targeted.
	FindLatestIndividualSubmission(ctx context.Context, userID string) (*HealthCheckSession, error)
}
