package v1_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/gin-gonic/gin"
	"github.com/golang-migrate/migrate/v4"
	migratePostgres "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	_ "github.com/lib/pq"

	"github.com/agopalakrishnan/teams360/backend/application/services"
	"github.com/agopalakrishnan/teams360/backend/infrastructure/persistence/postgres"
	"github.com/agopalakrishnan/teams360/backend/interfaces/api/v1"
)

var _ = Describe("Health Check API", func() {
	var (
		db         *sql.DB
		router     *gin.Engine
		err        error
		jwtService *services.JWTService
		userToken  string
	)

	BeforeEach(func() {
		// Set Gin to test mode
		gin.SetMode(gin.TestMode)

		// Connect to test database
		databaseURL := os.Getenv("TEST_DATABASE_URL")
		if databaseURL == "" {
			databaseURL = "postgres://postgres:postgres@localhost:5432/teams360_test?sslmode=disable"
		}

		db, err = sql.Open("postgres", databaseURL)
		Expect(err).NotTo(HaveOccurred())
		Expect(db.Ping()).To(Succeed())

		// Clean and run migrations
		_, err = db.Exec("DROP SCHEMA public CASCADE; CREATE SCHEMA public;")
		Expect(err).NotTo(HaveOccurred())

		driver, err := migratePostgres.WithInstance(db, &migratePostgres.Config{})
		Expect(err).NotTo(HaveOccurred())

		migrationEngine, err := migrate.NewWithDatabaseInstance(
			"file://../../../infrastructure/persistence/postgres/migrations",
			"postgres",
			driver,
		)
		Expect(err).NotTo(HaveOccurred())

		err = migrationEngine.Up()
		Expect(err).NotTo(HaveOccurred())

		// Initialize JWT service and generate test token
		jwtService = services.NewJWTService()
		tokenPair, tokenErr := jwtService.GenerateTokenPair(context.Background(), "user123", "user123", "user123@test.com", "level-5", []string{"team1"})
		Expect(tokenErr).NotTo(HaveOccurred())
		userToken = tokenPair.AccessToken

		// Initialize router with API routes
		router = gin.New()
		healthCheckRepo := postgres.NewHealthCheckRepository(db)
		orgRepo := postgres.NewOrganizationRepository(db)
		v1.SetupHealthCheckRoutes(router, healthCheckRepo, orgRepo, jwtService, nil)
	})

	AfterEach(func() {
		if db != nil {
			db.Close()
		}
	})

	Describe("POST /api/v1/health-checks", func() {
		Context("when submitting a valid health check", func() {
			It("should create a new session and return 201 Created", func() {
				// Given: A valid health check submission
				submission := map[string]interface{}{
					"teamId":           "team1",
					"userId":           "user123",
					"date":             time.Now().Format(time.RFC3339),
					"assessmentPeriod": "2024 - 2nd Half",
					"responses": []map[string]interface{}{
						{
							"dimensionId": "mission",
							"score":       3,
							"trend":       "improving",
							"comment":     "Clear mission and goals",
						},
						{
							"dimensionId": "value",
							"score":       2,
							"trend":       "stable",
							"comment":     "Delivering value consistently",
						},
						{
							"dimensionId": "speed",
							"score":       1,
							"trend":       "declining",
							"comment":     "Too many blockers",
						},
					},
					"completed": true,
				}

				body, err := json.Marshal(submission)
				Expect(err).NotTo(HaveOccurred())

				// When: Submitting via POST
				req := httptest.NewRequest(http.MethodPost, "/api/v1/health-checks", bytes.NewBuffer(body))
				req.Header.Set("Content-Type", "application/json")
				req.Header.Set("Authorization", "Bearer "+userToken)
				w := httptest.NewRecorder()
				router.ServeHTTP(w, req)

				// Then: Should return 201 Created
				Expect(w.Code).To(Equal(http.StatusCreated))

				// And: Response should contain session ID
				var response map[string]interface{}
				err = json.Unmarshal(w.Body.Bytes(), &response)
				Expect(err).NotTo(HaveOccurred())
				Expect(response).To(HaveKey("id"))
				Expect(response["teamId"]).To(Equal("team1"))
				Expect(response["userId"]).To(Equal("user123"))
				Expect(response["completed"]).To(BeTrue())

				// And: Session should be persisted in database
				sessionId := response["id"].(string)
				var count int
				err = db.QueryRow("SELECT COUNT(*) FROM health_check_sessions WHERE id = $1", sessionId).Scan(&count)
				Expect(err).NotTo(HaveOccurred())
				Expect(count).To(Equal(1))

				// And: Responses should be persisted
				var responseCount int
				err = db.QueryRow("SELECT COUNT(*) FROM health_check_responses WHERE session_id = $1", sessionId).Scan(&responseCount)
				Expect(err).NotTo(HaveOccurred())
				Expect(responseCount).To(Equal(3))
			})

			It("should auto-generate session ID if not provided", func() {
				// Given: Submission without ID
				submission := map[string]interface{}{
					"teamId": "team1",
					"userId": "user123",
					"date":   time.Now().Format(time.RFC3339),
					"responses": []map[string]interface{}{
						{
							"dimensionId": "mission",
							"score":       3,
							"trend":       "improving",
						},
					},
					"completed": true,
				}

				body, _ := json.Marshal(submission)
				req := httptest.NewRequest(http.MethodPost, "/api/v1/health-checks", bytes.NewBuffer(body))
				req.Header.Set("Content-Type", "application/json")
				req.Header.Set("Authorization", "Bearer "+userToken)
				w := httptest.NewRecorder()

				// When: Posting
				router.ServeHTTP(w, req)

				// Then: Should generate ID
				Expect(w.Code).To(Equal(http.StatusCreated))
				var response map[string]interface{}
				json.Unmarshal(w.Body.Bytes(), &response)
				Expect(response["id"]).NotTo(BeEmpty())
			})
		})

		Context("when submitting invalid data", func() {
			It("should return 400 Bad Request for missing teamId", func() {
				// Given: Submission without teamId
				submission := map[string]interface{}{
					"userId": "user123",
					"date":   time.Now().Format(time.RFC3339),
					"responses": []map[string]interface{}{
						{"dimensionId": "mission", "score": 3, "trend": "improving"},
					},
				}

				body, _ := json.Marshal(submission)
				req := httptest.NewRequest(http.MethodPost, "/api/v1/health-checks", bytes.NewBuffer(body))
				req.Header.Set("Content-Type", "application/json")
				req.Header.Set("Authorization", "Bearer "+userToken)
				w := httptest.NewRecorder()

				// When: Posting
				router.ServeHTTP(w, req)

				// Then: Should return 400
				Expect(w.Code).To(Equal(http.StatusBadRequest))
				var response map[string]interface{}
				json.Unmarshal(w.Body.Bytes(), &response)
				Expect(response).To(HaveKey("error"))
			})

			It("should return 400 Bad Request for invalid score", func() {
				// Given: Submission with score out of range
				submission := map[string]interface{}{
					"teamId": "team1",
					"userId": "user123",
					"date":   time.Now().Format(time.RFC3339),
					"responses": []map[string]interface{}{
						{"dimensionId": "mission", "score": 5, "trend": "improving"}, // Invalid score
					},
				}

				body, _ := json.Marshal(submission)
				req := httptest.NewRequest(http.MethodPost, "/api/v1/health-checks", bytes.NewBuffer(body))
				req.Header.Set("Content-Type", "application/json")
				req.Header.Set("Authorization", "Bearer "+userToken)
				w := httptest.NewRecorder()

				// When: Posting
				router.ServeHTTP(w, req)

				// Then: Should return 400
				Expect(w.Code).To(Equal(http.StatusBadRequest))
			})

			It("should return 400 Bad Request for invalid trend", func() {
				// Given: Submission with invalid trend
				submission := map[string]interface{}{
					"teamId": "team1",
					"userId": "user123",
					"date":   time.Now().Format(time.RFC3339),
					"responses": []map[string]interface{}{
						{"dimensionId": "mission", "score": 3, "trend": "invalid-trend"},
					},
				}

				body, _ := json.Marshal(submission)
				req := httptest.NewRequest(http.MethodPost, "/api/v1/health-checks", bytes.NewBuffer(body))
				req.Header.Set("Content-Type", "application/json")
				req.Header.Set("Authorization", "Bearer "+userToken)
				w := httptest.NewRecorder()

				// When: Posting
				router.ServeHTTP(w, req)

				// Then: Should return 400
				Expect(w.Code).To(Equal(http.StatusBadRequest))
			})

			It("should return 400 Bad Request for empty responses", func() {
				// Given: Submission with no responses
				submission := map[string]interface{}{
					"teamId":    "team1",
					"userId":    "user123",
					"date":      time.Now().Format(time.RFC3339),
					"responses": []map[string]interface{}{},
				}

				body, _ := json.Marshal(submission)
				req := httptest.NewRequest(http.MethodPost, "/api/v1/health-checks", bytes.NewBuffer(body))
				req.Header.Set("Content-Type", "application/json")
				req.Header.Set("Authorization", "Bearer "+userToken)
				w := httptest.NewRecorder()

				// When: Posting
				router.ServeHTTP(w, req)

				// Then: Should return 400
				Expect(w.Code).To(Equal(http.StatusBadRequest))
			})
		})
	})

	Describe("GET /api/v1/health-dimensions", func() {
		Context("when fetching health dimensions", func() {
			It("should return all 11 active dimensions", func() {
				// When: Getting dimensions
				req := httptest.NewRequest(http.MethodGet, "/api/v1/health-dimensions", nil)
				req.Header.Set("Authorization", "Bearer "+userToken)
				w := httptest.NewRecorder()
				router.ServeHTTP(w, req)

				// Then: Should return 200 OK
				Expect(w.Code).To(Equal(http.StatusOK))

				// And: Should have 11 dimensions
				var response map[string]interface{}
				err := json.Unmarshal(w.Body.Bytes(), &response)
				Expect(err).NotTo(HaveOccurred())
				Expect(response).To(HaveKey("dimensions"))

				dimensions := response["dimensions"].([]interface{})
				Expect(dimensions).To(HaveLen(11))

				// And: Each dimension should have required fields
				firstDimension := dimensions[0].(map[string]interface{})
				Expect(firstDimension).To(HaveKey("id"))
				Expect(firstDimension).To(HaveKey("name"))
				Expect(firstDimension).To(HaveKey("description"))
				Expect(firstDimension).To(HaveKey("goodDescription"))
				Expect(firstDimension).To(HaveKey("badDescription"))
			})

			It("should include mission dimension", func() {
				// When: Getting dimensions
				req := httptest.NewRequest(http.MethodGet, "/api/v1/health-dimensions", nil)
				req.Header.Set("Authorization", "Bearer "+userToken)
				w := httptest.NewRecorder()
				router.ServeHTTP(w, req)

				// Then: Should include mission
				var response map[string]interface{}
				json.Unmarshal(w.Body.Bytes(), &response)
				dimensions := response["dimensions"].([]interface{})

				found := false
				for _, dim := range dimensions {
					dimension := dim.(map[string]interface{})
					if dimension["id"] == "mission" {
						found = true
						Expect(dimension["name"]).To(Equal("Mission"))
						break
					}
				}
				Expect(found).To(BeTrue(), "Mission dimension should be present")
			})
		})
	})

	Describe("GET /api/v1/health-checks/:id", func() {
		Context("when fetching an existing session", func() {
			It("should return the session with all responses", func() {
				// Given: An existing session
				submission := map[string]interface{}{
					"teamId": "team1",
					"userId": "user123",
					"date":   time.Now().Format(time.RFC3339),
					"responses": []map[string]interface{}{
						{"dimensionId": "mission", "score": 3, "trend": "improving", "comment": "Great"},
					},
					"completed": true,
				}

				body, _ := json.Marshal(submission)
				req := httptest.NewRequest(http.MethodPost, "/api/v1/health-checks", bytes.NewBuffer(body))
				req.Header.Set("Content-Type", "application/json")
				req.Header.Set("Authorization", "Bearer "+userToken)
				w := httptest.NewRecorder()
				router.ServeHTTP(w, req)

				var createResponse map[string]interface{}
				json.Unmarshal(w.Body.Bytes(), &createResponse)
				sessionId := createResponse["id"].(string)

				// When: Getting by ID
				req = httptest.NewRequest(http.MethodGet, "/api/v1/health-checks/"+sessionId, nil)
				req.Header.Set("Authorization", "Bearer "+userToken)
				w = httptest.NewRecorder()
				router.ServeHTTP(w, req)

				// Then: Should return 200 OK
				Expect(w.Code).To(Equal(http.StatusOK))

				// And: Should have session details
				var getResponse map[string]interface{}
				json.Unmarshal(w.Body.Bytes(), &getResponse)
				Expect(getResponse["id"]).To(Equal(sessionId))
				Expect(getResponse["teamId"]).To(Equal("team1"))

				// And: Should have responses
				responses := getResponse["responses"].([]interface{})
				Expect(responses).To(HaveLen(1))
			})
		})

		Context("when session doesn't exist", func() {
			It("should return 404 Not Found", func() {
				// When: Getting non-existent session
				req := httptest.NewRequest(http.MethodGet, "/api/v1/health-checks/non-existent-id", nil)
				req.Header.Set("Authorization", "Bearer "+userToken)
				w := httptest.NewRecorder()
				router.ServeHTTP(w, req)

				// Then: Should return 404
				Expect(w.Code).To(Equal(http.StatusNotFound))
			})
		})
	})

	Describe("GET /api/v1/health-checks/team/:id", func() {
		Context("when team has multiple sessions", func() {
			It("should return all sessions for the team", func() {
				// Given: Multiple sessions for team1
				for i := 0; i < 3; i++ {
					submission := map[string]interface{}{
						"teamId": "team1",
						"userId": "user" + string(rune(i+1)),
						"date":   time.Now().Format(time.RFC3339),
						"responses": []map[string]interface{}{
							{"dimensionId": "mission", "score": 3, "trend": "improving"},
						},
						"completed": true,
					}

					body, _ := json.Marshal(submission)
					req := httptest.NewRequest(http.MethodPost, "/api/v1/health-checks", bytes.NewBuffer(body))
					req.Header.Set("Content-Type", "application/json")
					req.Header.Set("Authorization", "Bearer "+userToken)
					w := httptest.NewRecorder()
					router.ServeHTTP(w, req)
				}

				// When: Getting team sessions
				req := httptest.NewRequest(http.MethodGet, "/api/v1/health-checks/team/team1", nil)
				req.Header.Set("Authorization", "Bearer "+userToken)
				w := httptest.NewRecorder()
				router.ServeHTTP(w, req)

				// Then: Should return 200 OK
				Expect(w.Code).To(Equal(http.StatusOK))

				// And: Should have 3 sessions
				var response map[string]interface{}
				json.Unmarshal(w.Body.Bytes(), &response)
				sessions := response["sessions"].([]interface{})
				Expect(sessions).To(HaveLen(3))
			})
		})

		Context("when filtering by assessment period", func() {
			It("should return only sessions from that period", func() {
				// Given: Sessions from different periods
				submission1 := map[string]interface{}{
					"teamId":           "team1",
					"userId":           "user1",
					"date":             "2024-01-15T10:00:00Z",
					"assessmentPeriod": "2023 - 2nd Half",
					"responses": []map[string]interface{}{
						{"dimensionId": "mission", "score": 3, "trend": "improving"},
					},
					"completed": true,
				}

				submission2 := map[string]interface{}{
					"teamId":           "team1",
					"userId":           "user2",
					"date":             "2024-07-15T10:00:00Z",
					"assessmentPeriod": "2024 - 1st Half",
					"responses": []map[string]interface{}{
						{"dimensionId": "value", "score": 2, "trend": "stable"},
					},
					"completed": true,
				}

				// Post both submissions
				for _, sub := range []map[string]interface{}{submission1, submission2} {
					body, _ := json.Marshal(sub)
					req := httptest.NewRequest(http.MethodPost, "/api/v1/health-checks", bytes.NewBuffer(body))
					req.Header.Set("Content-Type", "application/json")
					req.Header.Set("Authorization", "Bearer "+userToken)
					w := httptest.NewRecorder()
					router.ServeHTTP(w, req)
				}

				// When: Filtering by period
				req := httptest.NewRequest(http.MethodGet, "/api/v1/health-checks/team/team1?assessmentPeriod=2024+-+1st+Half", nil)
				req.Header.Set("Authorization", "Bearer "+userToken)
				w := httptest.NewRecorder()
				router.ServeHTTP(w, req)

				// Then: Should return only matching sessions
				Expect(w.Code).To(Equal(http.StatusOK))
				var response map[string]interface{}
				json.Unmarshal(w.Body.Bytes(), &response)
				sessions := response["sessions"].([]interface{})
				Expect(sessions).To(HaveLen(1))

				session := sessions[0].(map[string]interface{})
				Expect(session["assessmentPeriod"]).To(Equal("2024 - 1st Half"))
			})
		})
	})

	Describe("POST /api/v1/health-checks - duplicate quarter prevention", func() {
		submitPayload := func(payload map[string]interface{}) *httptest.ResponseRecorder {
			body, _ := json.Marshal(payload)
			req := httptest.NewRequest(http.MethodPost, "/api/v1/health-checks", bytes.NewBuffer(body))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Authorization", "Bearer "+userToken)
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)
			return w
		}

		individualSubmission := func(userID, teamID, period string) map[string]interface{} {
			return map[string]interface{}{
				"teamId":           teamID,
				"userId":           userID,
				"date":             time.Now().Format(time.RFC3339),
				"assessmentPeriod": period,
				"surveyType":       "individual",
				"responses": []map[string]interface{}{
					{"dimensionId": "mission", "score": 3, "trend": "improving"},
				},
				"completed": true,
			}
		}

		postWorkshopSubmission := func(userID, teamID, period string) map[string]interface{} {
			s := individualSubmission(userID, teamID, period)
			s["surveyType"] = "post_workshop"
			return s
		}

		Context("individual survey", func() {
			It("rejects a second submission by the same user for the same quarter with 409 Conflict", func() {
				w1 := submitPayload(individualSubmission("dup-user-1", "team1", "2026 Q1"))
				Expect(w1.Code).To(Equal(http.StatusCreated))

				w2 := submitPayload(individualSubmission("dup-user-1", "team1", "2026 Q1"))
				Expect(w2.Code).To(Equal(http.StatusConflict))

				var response map[string]interface{}
				Expect(json.Unmarshal(w2.Body.Bytes(), &response)).To(Succeed())
				Expect(response["submittedPeriod"]).To(Equal("Q1 2026"))
				Expect(response["nextEligiblePeriod"]).To(Equal("Q3 2026"))
				Expect(response["message"]).To(ContainSubstring("Individual Survey"))
				Expect(response["message"]).To(ContainSubstring("Q1 2026"))
				Expect(response["message"]).To(ContainSubstring("Q3 2026"))
			})

			It("allows a second submission by a different user for the same quarter", func() {
				w1 := submitPayload(individualSubmission("dup-user-2", "team1", "2026 Q1"))
				Expect(w1.Code).To(Equal(http.StatusCreated))

				w2 := submitPayload(individualSubmission("dup-user-3", "team1", "2026 Q1"))
				Expect(w2.Code).To(Equal(http.StatusCreated))
			})

			It("allows the same user to submit again once the next eligible quarter arrives", func() {
				w1 := submitPayload(individualSubmission("dup-user-4", "team1", "2026 Q1"))
				Expect(w1.Code).To(Equal(http.StatusCreated))

				w2 := submitPayload(individualSubmission("dup-user-4", "team1", "2026 Q3"))
				Expect(w2.Code).To(Equal(http.StatusCreated))
			})
		})

		Context("post-workshop survey", func() {
			It("rejects a second submission for the same team and quarter with 409 Conflict, even from a different Team Lead", func() {
				w1 := submitPayload(postWorkshopSubmission("lead-1", "dup-team-1", "2026 Q2"))
				Expect(w1.Code).To(Equal(http.StatusCreated))

				w2 := submitPayload(postWorkshopSubmission("lead-2", "dup-team-1", "2026 Q2"))
				Expect(w2.Code).To(Equal(http.StatusConflict))

				var response map[string]interface{}
				Expect(json.Unmarshal(w2.Body.Bytes(), &response)).To(Succeed())
				Expect(response["submittedPeriod"]).To(Equal("Q2 2026"))
				Expect(response["nextEligiblePeriod"]).To(Equal("Q4 2026"))
				Expect(response["message"]).To(ContainSubstring("Post-Workshop Survey"))
			})

			It("allows a different team to submit for the same quarter", func() {
				w1 := submitPayload(postWorkshopSubmission("lead-1", "dup-team-2", "2026 Q2"))
				Expect(w1.Code).To(Equal(http.StatusCreated))

				w2 := submitPayload(postWorkshopSubmission("lead-1", "dup-team-3", "2026 Q2"))
				Expect(w2.Code).To(Equal(http.StatusCreated))
			})
		})

		Context("separation between survey types", func() {
			It("does not let a post-workshop submission block an individual submission for the same user/team/quarter, or vice versa", func() {
				w1 := submitPayload(individualSubmission("dup-user-5", "dup-team-4", "2026 Q4"))
				Expect(w1.Code).To(Equal(http.StatusCreated))

				w2 := submitPayload(postWorkshopSubmission("dup-user-5", "dup-team-4", "2026 Q4"))
				Expect(w2.Code).To(Equal(http.StatusCreated))
			})
		})

		Context("concurrent submission attempts", func() {
			It("allows exactly one of two concurrent submissions for the same user/quarter to succeed", func() {
				const attempts = 5
				codes := make(chan int, attempts)

				var wg sync.WaitGroup
				for i := 0; i < attempts; i++ {
					wg.Add(1)
					go func() {
						defer GinkgoRecover()
						defer wg.Done()
						w := submitPayload(individualSubmission("dup-user-concurrent", "team1", "2026 Q1"))
						codes <- w.Code
					}()
				}
				wg.Wait()
				close(codes)

				created, conflict := 0, 0
				for code := range codes {
					switch code {
					case http.StatusCreated:
						created++
					case http.StatusConflict:
						conflict++
					}
				}
				Expect(created).To(Equal(1), "exactly one concurrent submission should be accepted")
				Expect(conflict).To(Equal(attempts-1), "every other concurrent submission should be rejected as a duplicate")
			})
		})
	})

	Describe("GET /api/v1/health-checks/eligibility", func() {
		Context("individual survey", func() {
			It("reports eligible when no prior submission exists for the quarter", func() {
				req := httptest.NewRequest(http.MethodGet,
					"/api/v1/health-checks/eligibility?surveyType=individual&userId=elig-user-1&assessmentPeriod=2026+Q1", nil)
				req.Header.Set("Authorization", "Bearer "+userToken)
				w := httptest.NewRecorder()
				router.ServeHTTP(w, req)

				Expect(w.Code).To(Equal(http.StatusOK))
				var response map[string]interface{}
				Expect(json.Unmarshal(w.Body.Bytes(), &response)).To(Succeed())
				Expect(response["eligible"]).To(BeTrue())
			})

			It("reports ineligible with the submitted and next-eligible periods after a submission", func() {
				body, _ := json.Marshal(individualSubmissionPayload("elig-user-2", "team1", "2026 Q4"))
				req := httptest.NewRequest(http.MethodPost, "/api/v1/health-checks", bytes.NewBuffer(body))
				req.Header.Set("Content-Type", "application/json")
				req.Header.Set("Authorization", "Bearer "+userToken)
				w := httptest.NewRecorder()
				router.ServeHTTP(w, req)
				Expect(w.Code).To(Equal(http.StatusCreated))

				req2 := httptest.NewRequest(http.MethodGet,
					"/api/v1/health-checks/eligibility?surveyType=individual&userId=elig-user-2&assessmentPeriod=2026+Q4", nil)
				req2.Header.Set("Authorization", "Bearer "+userToken)
				w2 := httptest.NewRecorder()
				router.ServeHTTP(w2, req2)

				Expect(w2.Code).To(Equal(http.StatusOK))
				var response map[string]interface{}
				Expect(json.Unmarshal(w2.Body.Bytes(), &response)).To(Succeed())
				Expect(response["eligible"]).To(BeFalse())
				Expect(response["submittedPeriod"]).To(Equal("Q4 2026"))
				Expect(response["nextEligiblePeriod"]).To(Equal("Q2 2027"))
			})
		})

		Context("missing or invalid parameters", func() {
			It("returns 400 when assessmentPeriod is missing", func() {
				req := httptest.NewRequest(http.MethodGet,
					"/api/v1/health-checks/eligibility?surveyType=individual&userId=elig-user-3", nil)
				req.Header.Set("Authorization", "Bearer "+userToken)
				w := httptest.NewRecorder()
				router.ServeHTTP(w, req)
				Expect(w.Code).To(Equal(http.StatusBadRequest))
			})

			It("returns 400 when assessmentPeriod is not a recognized format", func() {
				req := httptest.NewRequest(http.MethodGet,
					"/api/v1/health-checks/eligibility?surveyType=individual&userId=elig-user-4&assessmentPeriod=not-a-period", nil)
				req.Header.Set("Authorization", "Bearer "+userToken)
				w := httptest.NewRecorder()
				router.ServeHTTP(w, req)
				Expect(w.Code).To(Equal(http.StatusBadRequest))
			})

			It("returns 400 when teamId is missing for a post_workshop check", func() {
				req := httptest.NewRequest(http.MethodGet,
					"/api/v1/health-checks/eligibility?surveyType=post_workshop&assessmentPeriod=2026+Q1", nil)
				req.Header.Set("Authorization", "Bearer "+userToken)
				w := httptest.NewRecorder()
				router.ServeHTTP(w, req)
				Expect(w.Code).To(Equal(http.StatusBadRequest))
			})

			It("returns 400 when userId is missing for an individual check", func() {
				req := httptest.NewRequest(http.MethodGet,
					"/api/v1/health-checks/eligibility?surveyType=individual&assessmentPeriod=2026+Q1", nil)
				req.Header.Set("Authorization", "Bearer "+userToken)
				w := httptest.NewRecorder()
				router.ServeHTTP(w, req)
				Expect(w.Code).To(Equal(http.StatusBadRequest))
			})
		})
	})

	Describe("POST /api/v1/health-checks - consecutive quarter prevention (Individual Survey)", func() {
		submitIndividual := func(userID, teamID, period string) *httptest.ResponseRecorder {
			body, _ := json.Marshal(individualSubmissionPayload(userID, teamID, period))
			req := httptest.NewRequest(http.MethodPost, "/api/v1/health-checks", bytes.NewBuffer(body))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Authorization", "Bearer "+userToken)
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)
			return w
		}

		DescribeTable("rejects a submission for the quarter immediately after the last one",
			func(userID, from, to, wantSubmitted, wantNextEligible string) {
				w1 := submitIndividual(userID, "team1", from)
				Expect(w1.Code).To(Equal(http.StatusCreated))

				w2 := submitIndividual(userID, "team1", to)
				Expect(w2.Code).To(Equal(http.StatusConflict))

				var response map[string]interface{}
				Expect(json.Unmarshal(w2.Body.Bytes(), &response)).To(Succeed())
				Expect(response["code"]).To(Equal("consecutive_quarter_submission"))
				Expect(response["submittedPeriod"]).To(Equal(wantSubmitted))
				Expect(response["nextEligiblePeriod"]).To(Equal(wantNextEligible))
				Expect(response["message"]).To(ContainSubstring("consecutive quarters"))
			},
			Entry("Q1 to Q2", "consec-user-1", "2026 Q1", "2026 Q2", "Q1 2026", "Q3 2026"),
			Entry("Q2 to Q3", "consec-user-2", "2026 Q2", "2026 Q3", "Q2 2026", "Q4 2026"),
			Entry("Q3 to Q4", "consec-user-3", "2026 Q3", "2026 Q4", "Q3 2026", "Q1 2027"),
			Entry("Q4 to Q1 of the following year", "consec-user-4", "2025 Q4", "2026 Q1", "Q4 2025", "Q2 2026"),
		)

		DescribeTable("allows a submission once at least one full quarter has been skipped",
			func(userID, from, to string) {
				w1 := submitIndividual(userID, "team1", from)
				Expect(w1.Code).To(Equal(http.StatusCreated))

				w2 := submitIndividual(userID, "team1", to)
				Expect(w2.Code).To(Equal(http.StatusCreated))
			},
			Entry("Q1 to Q3", "consec-user-5", "2026 Q1", "2026 Q3"),
			Entry("Q2 to Q4", "consec-user-6", "2026 Q2", "2026 Q4"),
			Entry("Q1 to Q4", "consec-user-7", "2026 Q1", "2026 Q4"),
			Entry("Q3 to Q1 of the following year", "consec-user-8", "2025 Q3", "2026 Q1"),
			Entry("Q4 to Q2 of the following year", "consec-user-9", "2025 Q4", "2026 Q2"),
		)

		It("does not apply the consecutive-quarter restriction to Post-Workshop Survey submissions", func() {
			body1, _ := json.Marshal(map[string]interface{}{
				"teamId": "consec-team-1", "userId": "consec-lead-1",
				"date": time.Now().Format(time.RFC3339), "assessmentPeriod": "2026 Q1",
				"surveyType": "post_workshop",
				"responses": []map[string]interface{}{
					{"dimensionId": "mission", "score": 3, "trend": "improving"},
				},
				"completed": true,
			})
			req1 := httptest.NewRequest(http.MethodPost, "/api/v1/health-checks", bytes.NewBuffer(body1))
			req1.Header.Set("Content-Type", "application/json")
			req1.Header.Set("Authorization", "Bearer "+userToken)
			w1 := httptest.NewRecorder()
			router.ServeHTTP(w1, req1)
			Expect(w1.Code).To(Equal(http.StatusCreated))

			body2, _ := json.Marshal(map[string]interface{}{
				"teamId": "consec-team-1", "userId": "consec-lead-1",
				"date": time.Now().Format(time.RFC3339), "assessmentPeriod": "2026 Q2",
				"surveyType": "post_workshop",
				"responses": []map[string]interface{}{
					{"dimensionId": "mission", "score": 3, "trend": "improving"},
				},
				"completed": true,
			})
			req2 := httptest.NewRequest(http.MethodPost, "/api/v1/health-checks", bytes.NewBuffer(body2))
			req2.Header.Set("Content-Type", "application/json")
			req2.Header.Set("Authorization", "Bearer "+userToken)
			w2 := httptest.NewRecorder()
			router.ServeHTTP(w2, req2)
			Expect(w2.Code).To(Equal(http.StatusCreated))
		})
	})

	Describe("GET /api/v1/health-checks/eligibility - consecutive quarter (Individual Survey)", func() {
		It("reports ineligible with reason 'consecutive_quarter' after a submission for the immediately preceding quarter", func() {
			w1 := httptest.NewRecorder()
			body, _ := json.Marshal(individualSubmissionPayload("consec-elig-1", "team1", "2026 Q1"))
			req1 := httptest.NewRequest(http.MethodPost, "/api/v1/health-checks", bytes.NewBuffer(body))
			req1.Header.Set("Content-Type", "application/json")
			req1.Header.Set("Authorization", "Bearer "+userToken)
			router.ServeHTTP(w1, req1)
			Expect(w1.Code).To(Equal(http.StatusCreated))

			req2 := httptest.NewRequest(http.MethodGet,
				"/api/v1/health-checks/eligibility?surveyType=individual&userId=consec-elig-1&assessmentPeriod=2026+Q2", nil)
			req2.Header.Set("Authorization", "Bearer "+userToken)
			w2 := httptest.NewRecorder()
			router.ServeHTTP(w2, req2)

			Expect(w2.Code).To(Equal(http.StatusOK))
			var response map[string]interface{}
			Expect(json.Unmarshal(w2.Body.Bytes(), &response)).To(Succeed())
			Expect(response["eligible"]).To(BeFalse())
			Expect(response["reason"]).To(Equal("consecutive_quarter"))
			Expect(response["submittedPeriod"]).To(Equal("Q1 2026"))
			Expect(response["nextEligiblePeriod"]).To(Equal("Q3 2026"))
		})

		It("reports eligible for a quarter that skips at least one full quarter", func() {
			w1 := httptest.NewRecorder()
			body, _ := json.Marshal(individualSubmissionPayload("consec-elig-2", "team1", "2026 Q1"))
			req1 := httptest.NewRequest(http.MethodPost, "/api/v1/health-checks", bytes.NewBuffer(body))
			req1.Header.Set("Content-Type", "application/json")
			req1.Header.Set("Authorization", "Bearer "+userToken)
			router.ServeHTTP(w1, req1)
			Expect(w1.Code).To(Equal(http.StatusCreated))

			req2 := httptest.NewRequest(http.MethodGet,
				"/api/v1/health-checks/eligibility?surveyType=individual&userId=consec-elig-2&assessmentPeriod=2026+Q3", nil)
			req2.Header.Set("Authorization", "Bearer "+userToken)
			w2 := httptest.NewRecorder()
			router.ServeHTTP(w2, req2)

			Expect(w2.Code).To(Equal(http.StatusOK))
			var response map[string]interface{}
			Expect(json.Unmarshal(w2.Body.Bytes(), &response)).To(Succeed())
			Expect(response["eligible"]).To(BeTrue())
		})
	})
})

func individualSubmissionPayload(userID, teamID, period string) map[string]interface{} {
	return map[string]interface{}{
		"teamId":           teamID,
		"userId":           userID,
		"date":             time.Now().Format(time.RFC3339),
		"assessmentPeriod": period,
		"surveyType":       "individual",
		"responses": []map[string]interface{}{
			{"dimensionId": "mission", "score": 3, "trend": "improving"},
		},
		"completed": true,
	}
}
