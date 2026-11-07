package app

import (
	"context"
	"strings"
	"testing"
	"time"

	"yingce/backend/internal/model"
)

func TestAdvertisingScriptPlanOutput(t *testing.T) {
	source := AdvertisingDraft{SchemaVersion: 1, Brief: AdvertisingBrief{ProductName: "素色杯", Description: "白色陶瓷杯", DurationMs: 15000}, ProductResourceIDs: []string{"owned-image"}}
	valid := `{"shots":[{"visual":"把商品置于桌面中央"},{"visual":"镜头环绕商品外观"},{"visual":"背景渐隐展示商品"}],"evidence":[{"claim":"白色陶瓷杯","source":"user"}],"uncertainties":[]}`
	result, err := advertisingPlanOutput(valid, source)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Draft.Shots) != 3 || result.Draft.Shots[0].DurationMs != 5000 || result.Draft.Shots[0].OnScreenText != "" || result.Draft.Shots[0].Voiceover != "" || result.Draft.Brief.CTA != "" || len(result.Uncertainties) == 0 {
		t.Fatalf("unsafe candidate: %+v", result)
	}
	cases := []struct{ name, raw string }{
		{"trailing", valid + ` {}`},
		{"malformed", `{broken`},
		{"invented claim", strings.Replace(valid, "把商品置于桌面中央", "零糖半价立即购买", 1)},
		{"unsupported endorsement", strings.Replace(valid, "把商品置于桌面中央", "官方认证商品", 1)},
		{"model text", strings.Replace(valid, `"visual":"把商品置于桌面中央"`, `"visual":"把商品置于桌面中央","onScreenText":"半价"`, 1)},
		{"false evidence", strings.Replace(valid, `"claim":"白色陶瓷杯"`, `"claim":"官方认证"`, 1)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := advertisingPlanOutput(tc.raw, source); err == nil {
				t.Fatal("accepted unsafe model output")
			}
		})
	}
	source.Brief.Goal = "conversion"
	if _, err := advertisingPlanOutput(valid, source); err == nil {
		t.Fatal("converted a conversion goal into awareness")
	}
	source.Brief.Goal = ""
	source.ProductResourceIDs = []string{"owned-image", "second-image"}
	if _, err := advertisingPlanOutput(valid, source); err == nil {
		t.Fatal("accepted multiple product images")
	}
}

func TestAdvertisingScriptPlanReplayAndIsolation(t *testing.T) {
	svc, db := advertisingTestService(t)
	seedResource(t, db, "resource", "owner", "image/png")
	draft := advertisingTestDraft()
	project, err := svc.CreateAdvertisingProject("owner", AdvertisingCreateRequest{Name: "Ad", Draft: &draft})
	if err != nil {
		t.Fatal(err)
	}
	op := "10000000-0000-4000-8000-000000000777"
	row := model.AdvertisingScriptPlan{ID: newID(), UserID: "owner", ProjectID: project.Project.ID, OperationID: op, SourceRevision: 1, Status: "running", SnapshotJSON: "{}", CreatedAt: time.Now(), UpdatedAt: time.Now()}
	if err := svc.repo.CreateAdvertisingPlan(&row); err != nil {
		t.Fatal(err)
	}
	got, err := svc.CreateAdvertisingScriptPlan(context.Background(), "owner", project.Project.ID, AdvertisingScriptPlanRequest{ExpectedRevision: 1, ClientOperationID: op})
	if err != nil || got.Status != "running" || got.OperationID != op {
		t.Fatalf("replay: %+v %v", got, err)
	}
	if _, err := svc.GetAdvertisingScriptPlan("other", project.Project.ID, op); err == nil {
		t.Fatal("cross-user operation read")
	}
	var tasks, plans int64
	if err := db.Model(&model.Task{}).Count(&tasks).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.AdvertisingScriptPlan{}).Count(&plans).Error; err != nil {
		t.Fatal(err)
	}
	if plans != 1 || tasks != 0 {
		t.Fatalf("replay charged or inserted rows: plans=%d tasks=%d", plans, tasks)
	}
	if err := db.Model(&row).Update("created_at", time.Now().Add(-time.Minute)).Error; err != nil {
		t.Fatal(err)
	}
	got, err = svc.GetAdvertisingScriptPlan("owner", project.Project.ID, op)
	if err != nil || got.Status != "failed" {
		t.Fatalf("stale operation: %+v %v", got, err)
	}
	if err := svc.repo.FinishAdvertisingPlan(project.Project.ID, op, "ready", `{"draft":{}}`, ""); err == nil {
		t.Fatal("stale operation accepted a late candidate")
	}
}
