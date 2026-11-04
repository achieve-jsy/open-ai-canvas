package handler

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"yingce/backend/internal/service"
)

func RegisterAdvertisingProjectRoutes(r *gin.RouterGroup, svc *service.Service) {
	for _, route := range []struct{ method, path string }{{"GET", "/ad-projects"}, {"POST", "/ad-projects"}, {"GET", "/ad-projects/:id"}, {"PUT", "/ad-projects/:id"}, {"POST", "/ad-projects/:id/confirm"}, {"GET", "/ad-projects/:id/revisions"}, {"GET", "/ad-projects/:id/revisions/:revisionId"}, {"DELETE", "/ad-projects/:id"}} {
		r.Handle(route.method, route.path, func(c *gin.Context) {
			user, err := currentUser(c, svc)
			if err != nil {
				failService(c, err)
				return
			}
			var result any
			id := c.Param("id")
			switch {
			case c.Request.Method == http.MethodPost && id == "":
				var req service.AdvertisingCreateRequest
				if err = decodeAdvertisingBody(c, &req); err == nil {
					result, err = svc.CreateAdvertisingProject(user.ID, req)
				}
			case c.Request.Method == http.MethodPut:
				var req service.AdvertisingSaveRequest
				if err = decodeAdvertisingBody(c, &req); err == nil {
					result, err = svc.SaveAdvertisingProject(user.ID, id, req)
				}
			case c.Request.Method == http.MethodPost:
				var req struct {
					ExpectedRevision int64 `json:"expectedRevision"`
				}
				if err = decodeAdvertisingBody(c, &req); err == nil {
					result, err = svc.ConfirmAdvertisingProject(user.ID, id, req.ExpectedRevision)
				}
			case c.Request.Method == http.MethodDelete:
				var revision int64
				revision, err = strconv.ParseInt(c.Query("expectedRevision"), 10, 64)
				if err != nil {
					err = service.BadAuthRequest("expectedRevision必须为正整数")
				} else {
					err = svc.DeleteAdvertisingProject(user.ID, id, revision)
					result = gin.H{"deleted": err == nil}
				}
			case c.Param("revisionId") != "":
				result, err = svc.GetAdvertisingRevision(user.ID, id, c.Param("revisionId"))
			case id == "" || strings.HasSuffix(c.FullPath(), "/revisions"):
				var page, size int
				page, size, err = parsePaginationQuery(c, 20)
				if err == nil {
					if id == "" {
						result, err = svc.ListAdvertisingProjects(user.ID, page, size)
					} else {
						result, err = svc.ListAdvertisingRevisions(user.ID, id, page, size)
					}
				}
			default:
				result, err = svc.GetAdvertisingProject(user.ID, id)
			}
			if err != nil {
				failService(c, err)
				return
			}
			ok(c, result)
		})
	}
}

func decodeAdvertisingBody(c *gin.Context, dest any) error {
	invalid := func(err error) error {
		e := service.BadAuthRequest("广告请求格式无效: " + err.Error())
		e.Reason = service.ReasonAdvertisingInvalidDocument
		return e
	}
	raw, err := io.ReadAll(http.MaxBytesReader(c.Writer, c.Request.Body, 256<<10))
	if err != nil {
		return invalid(err)
	}
	if !utf8.Valid(raw) || len(bytes.TrimSpace(raw)) == 0 || bytes.TrimSpace(raw)[0] != '{' {
		return invalid(fmt.Errorf("需要JSON对象"))
	}
	tokens := json.NewDecoder(bytes.NewReader(raw))
	tokens.UseNumber()
	if err := checkAdvertisingJSONValue(tokens, 0); err != nil {
		return invalid(err)
	}
	if _, err := tokens.Token(); err != io.EOF {
		return invalid(fmt.Errorf("只允许一个JSON对象"))
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(dest); err != nil {
		return invalid(err)
	}
	return nil
}

// encoding/json accepts duplicate and case-insensitive keys; reject ambiguous writes.
func checkAdvertisingJSONValue(dec *json.Decoder, depth int) error {
	if depth > 16 {
		return fmt.Errorf("JSON嵌套过深")
	}
	token, err := dec.Token()
	if err != nil {
		return err
	}
	delimiter, ok := token.(json.Delim)
	if !ok {
		return nil
	}
	switch delimiter {
	case '{':
		seen := map[string]bool{}
		for dec.More() {
			key, err := dec.Token()
			if err != nil {
				return err
			}
			name, ok := key.(string)
			if !ok {
				return fmt.Errorf("无效对象字段")
			}
			folded := strings.ToLower(name)
			if seen[folded] {
				return fmt.Errorf("重复字段 %s", name)
			}
			seen[folded] = true
			if err := checkAdvertisingJSONValue(dec, depth+1); err != nil {
				return err
			}
		}
	case '[':
		for dec.More() {
			if err := checkAdvertisingJSONValue(dec, depth+1); err != nil {
				return err
			}
		}
	default:
		return fmt.Errorf("无效JSON分隔符")
	}
	_, err = dec.Token()
	return err
}
