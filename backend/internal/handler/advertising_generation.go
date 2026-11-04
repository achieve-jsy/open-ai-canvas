package handler

import (
	"github.com/gin-gonic/gin"
	"net/http"
	"strings"
	"yingce/backend/internal/service"
)

func RegisterAdvertisingGenerationRoutes(r *gin.RouterGroup, svc *service.Service) {
	base := "/ad-projects/:id/productions"
	for _, route := range []struct{ method, path string }{
		{"POST", ""}, {"GET", "/:productionId"}, {"PATCH", "/:productionId"},
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
