package handler

import (
	"github.com/gin-gonic/gin"
	"net/http"
	"strings"
	"yingce/backend/internal/service"
)

func RegisterAdvertisingGenerationRoutes(r *gin.RouterGroup, svc *service.Service) {
	r.GET("/ad-projects/:id/events", func(c *gin.Context) {
		user, err := currentUser(c, svc)
		if err != nil {
			failService(c, err)
			return
		}
		page, size, err := parsePaginationQuery(c, 20)
		if err != nil {
			failService(c, err)
			return
		}
		result, err := svc.ListAdvertisingEvents(user.ID, c.Param("id"), c.Query("productionId"), c.Query("shotId"), c.Query("kind"), page, size)
		if err != nil {
			failService(c, err)
			return
		}
		ok(c, result)
	})
	base := "/ad-projects/:id/productions"
	for _, route := range []struct{ method, path string }{
		{"POST", ""}, {"GET", "/:productionId"}, {"PATCH", "/:productionId"},
		{"GET", "/:productionId/reviews"}, {"POST", "/:productionId/reviews"},
		{"GET", "/:productionId/issues"}, {"POST", "/:productionId/issues"}, {"PATCH", "/:productionId/issues/:issueId"},
		{"POST", "/:productionId/batches"}, {"GET", "/:productionId/batches"}, {"GET", "/:productionId/batches/:batchId"},
		{"POST", "/:productionId/batches/:batchId/execute"}, {"POST", "/:productionId/batches/:batchId/close"}, {"GET", "/:productionId/activity"},
		{"GET", "/:productionId/submissions"}, {"POST", "/:productionId/submissions"}, {"POST", "/:productionId/submissions/:submissionId/execute"},
		{"GET", "/:productionId/assemblies"}, {"POST", "/:productionId/assemblies"}, {"GET", "/:productionId/assemblies/:assemblyId"},
		{"GET", "/:productionId/assemblies/:assemblyId/renders"}, {"POST", "/:productionId/assemblies/:assemblyId/renders"},
	} {
		r.Handle(route.method, base+route.path, func(c *gin.Context) {
			user, err := currentUser(c, svc)
			if err != nil {
				failService(c, err)
				return
			}
			id, p, a := c.Param("id"), c.Param("productionId"), c.Param("assemblyId")
			var result any
			switch {
			case p == "":
				var req struct {
					ConfirmedRevisionID string `json:"confirmedRevisionId"`
				}
				if err = decodeAdvertisingBody(c, &req); err == nil {
					result, err = svc.CreateAdvertisingProduction(user.ID, id, req.ConfirmedRevisionID)
				}
			case strings.HasSuffix(c.FullPath(), "/activity"):
				result, err = svc.GetAdvertisingActivity(user.ID, id, p)
			case c.Param("batchId") != "":
				batchID := c.Param("batchId")
				switch {
				case c.Request.Method == http.MethodGet:
					result, err = svc.GetAdvertisingBatch(user.ID, id, p, batchID)
				case strings.HasSuffix(c.FullPath(), "/close"):
					var req struct{}
					if err = decodeAdvertisingBody(c, &req); err == nil {
						result, err = svc.CloseAdvertisingBatch(user.ID, id, p, batchID)
					}
				default:
					var req struct {
						Items []service.AdvertisingBatchApproval `json:"items"`
					}
					if err = decodeAdvertisingBody(c, &req); err == nil {
						result, err = svc.ExecuteAdvertisingBatch(user.ID, id, p, batchID, req.Items)
					}
				}
			case strings.HasSuffix(c.FullPath(), "/batches"):
				switch {
				case c.Request.Method == http.MethodPost:
					var req service.AdvertisingBatchRequest
					if err = decodeAdvertisingBody(c, &req); err == nil {
						result, err = svc.PrepareAdvertisingBatch(user.ID, id, p, req)
					}
				case c.Query("clientOperationId") != "":
					result, err = svc.GetAdvertisingBatchForOperation(user.ID, id, p, c.Query("clientOperationId"))
				default:
					var page, size int
					if page, size, err = parsePaginationQuery(c, 20); err == nil {
						result, err = svc.ListAdvertisingBatches(user.ID, id, p, page, size)
					}
				}
			case c.Param("issueId") != "":
				var req service.AdvertisingIssueResolution
				if err = decodeAdvertisingBody(c, &req); err == nil {
					result, err = svc.ResolveAdvertisingIssue(user.ID, id, p, c.Param("issueId"), req)
				}
			case strings.HasSuffix(c.FullPath(), "/reviews"):
				switch {
				case c.Request.Method == http.MethodPost:
					var req service.AdvertisingReviewRequest
					if err = decodeAdvertisingBody(c, &req); err == nil {
						result, err = svc.CreateAdvertisingReview(user.ID, id, p, req)
					}
				case c.Query("clientOperationId") != "":
					result, err = svc.GetAdvertisingReviewForOperation(user.ID, id, p, c.Query("clientOperationId"))
				default:
					var page, size int
					if page, size, err = parsePaginationQuery(c, 20); err == nil {
						result, err = svc.ListAdvertisingReviews(user.ID, id, p, page, size)
					}
				}
			case strings.HasSuffix(c.FullPath(), "/issues"):
				switch {
				case c.Request.Method == http.MethodPost:
					var req service.AdvertisingIssueRequest
					if err = decodeAdvertisingBody(c, &req); err == nil {
						result, err = svc.CreateAdvertisingIssue(user.ID, id, p, req)
					}
				case c.Query("clientOperationId") != "":
					result, err = svc.GetAdvertisingIssueForOperation(user.ID, id, p, c.Query("clientOperationId"))
				default:
					var page, size int
					if page, size, err = parsePaginationQuery(c, 20); err == nil {
						result, err = svc.ListAdvertisingIssues(user.ID, id, p, page, size)
					}
				}
			case c.Request.Method == http.MethodPatch:
				var req struct {
					ExpectedRevision int64                                 `json:"expectedRevision"`
					Settings         service.AdvertisingProductionSettings `json:"settings"`
				}
				if err = decodeAdvertisingBody(c, &req); err == nil {
					result, err = svc.SaveAdvertisingProductionSettings(user.ID, id, p, req.ExpectedRevision, req.Settings)
				}
			case c.Param("submissionId") != "":
				var req struct {
					QuoteHash string `json:"quoteHash"`
				}
				if err = decodeAdvertisingBody(c, &req); err == nil {
					result, err = svc.ExecuteAdvertisingSubmission(user.ID, id, p, c.Param("submissionId"), req.QuoteHash)
				}
			case c.Request.Method == http.MethodPost && strings.HasSuffix(c.FullPath(), "/submissions"):
				var req service.AdvertisingSubmissionRequest
				if err = decodeAdvertisingBody(c, &req); err == nil {
					result, err = svc.PrepareAdvertisingSubmission(user.ID, id, p, req)
				}
			case c.Request.Method == http.MethodPost && a != "":
				var req struct {
					ClientOperationID string `json:"clientOperationId"`
				}
				if err = decodeAdvertisingBody(c, &req); err == nil {
					result, err = svc.CreateAdvertisingRender(user.ID, id, p, a, req.ClientOperationID)
				}
			case c.Request.Method == http.MethodPost:
				var req struct {
					ExpectedRevision  int64  `json:"expectedRevision"`
					ClientOperationID string `json:"clientOperationId"`
					Fit               string `json:"fit"`
				}
				if err = decodeAdvertisingBody(c, &req); err == nil {
					result, err = svc.CreateAdvertisingAssembly(user.ID, id, p, req.ExpectedRevision, req.ClientOperationID, req.Fit)
				}
			case strings.HasSuffix(c.FullPath(), "/submissions") || strings.HasSuffix(c.FullPath(), "/assemblies") || strings.HasSuffix(c.FullPath(), "/renders"):
				var page, size int
				page, size, err = parsePaginationQuery(c, 20)
				if err == nil {
					switch {
					case a != "":
						result, err = svc.ListAdvertisingRenderAttempts(user.ID, id, p, a, page, size)
					case strings.HasSuffix(c.FullPath(), "/submissions"):
						result, err = svc.ListAdvertisingSubmissions(user.ID, id, p, page, size)
					default:
						result, err = svc.ListAdvertisingAssemblies(user.ID, id, p, page, size)
					}
				}
			case a != "":
				result, err = svc.GetAdvertisingAssembly(user.ID, id, p, a)
			default:
				result, err = svc.GetAdvertisingProduction(user.ID, id, p)
			}
			if err != nil {
				failService(c, err)
				return
			}
			ok(c, result)
		})
	}
}
