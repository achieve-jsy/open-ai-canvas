package app

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"strings"
	"testing"
	"time"
	"yingce/backend/internal/model"
	"yingce/backend/internal/platform"
	"yingce/backend/internal/protocol"
)

func TestAdvertisingTTSQuotation(t *testing.T) {
	s, db, _ := agentMediaFixture(t)
	for _, row := range []any{&model.ChannelModel{ID: "audio-cm", ChannelID: "channel", ModelKey: "tts-1", Capability: "audio", Protocol: model.ChannelInterfaceOpenAIAudio, BillingMode: "fixed_request", PriceConfigured: true, Enabled: true}, &model.ChannelModelPriceTier{ID: "audio-tier", ChannelModelID: "audio-cm", SelectorKey: "{}", SelectorJSON: "{}", BillingMode: "fixed_request", UnitPriceMicrocredits: 100, PriceConfigured: true, Enabled: true}} {
		if err := db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	draft := advertisingTestDraft()
	draft.ProductResourceIDs = []string{"ref-one"}
	draft.Shots[0].ReferenceResourceIDs = []string{"ref-one"}
	draft.Shots[0].Voiceover = "让每一天更轻松"
	ad, err := s.CreateAdvertisingProject("user", AdvertisingCreateRequest{Name: "TTS", Draft: &draft})
	if err != nil {
		t.Fatal(err)
	}
	confirmed, err := s.ConfirmAdvertisingProject("user", ad.Project.ID, 1)
	if err != nil {
		t.Fatal(err)
	}
	p, err := s.CreateAdvertisingProduction("user", ad.Project.ID, confirmed.Snapshot.ID)
	if err != nil {
		t.Fatal(err)
	}
	req := AdvertisingSubmissionRequest{ClientOperationID: "10000000-0000-4000-8000-000000000020", ShotID: draft.Shots[0].ID, Kind: "audio", ModelID: "audio-cm", VoiceID: "alloy"}
	q, err := s.PrepareAdvertisingSubmission("user", ad.Project.ID, p.Production.ID, req)
	if err != nil {
		meta, _ := s.protocolMetadata("openai-audio")
		t.Fatalf("TTS quote %v, metadata=%+v", err, meta.Parameters)
	}
	if q.PublicRequest["prompt"] != draft.Shots[0].Voiceover || q.Quote.AmountMicrocredits != 100 {
		t.Fatalf("TTS quote %+v", q)
	}
	req.ClientOperationID = "10000000-0000-4000-8000-000000000021"
	req.VoiceID = "unlisted"
	if _, err := s.PrepareAdvertisingSubmission("user", ad.Project.ID, p.Production.ID, req); err == nil {
		t.Fatal("unlisted voice accepted")
	}
}

func TestAdvertisingSubmissionLifecycle(t *testing.T) {
	svc, db, _ := agentMediaFixture(t)
	draft := advertisingTestDraft()
	draft.ProductResourceIDs = []string{"ref-one"}
	draft.Shots[0].ReferenceResourceIDs = []string{"ref-one"}
	ad, err := svc.CreateAdvertisingProject("user", AdvertisingCreateRequest{Name: "Generate", Draft: &draft})
	if err != nil {
		t.Fatal(err)
	}
	confirmed, err := svc.ConfirmAdvertisingProject("user", ad.Project.ID, 1)
	if err != nil {
		t.Fatal(err)
	}
	production, err := svc.CreateAdvertisingProduction("user", ad.Project.ID, confirmed.Snapshot.ID)
	if err != nil {
		t.Fatal(err)
	}
	id := production.Production.ID
	req := AdvertisingSubmissionRequest{ClientOperationID: "10000000-0000-4000-8000-000000000010", ShotID: draft.Shots[0].ID, Kind: "video", ModelID: "video-cm"}
	request, err := svc.advertisingTaskRequest("user", ad.Project.ID, production.ConfirmedSnapshot.Snapshot, req)
	if err != nil {
		t.Fatal(err)
	}
	bare := production.ConfirmedSnapshot.Snapshot
	bare.Draft.Brief.ProductName, bare.Draft.Brief.Audience, bare.Draft.Brief.CTA = "", "", ""
	bare.Draft.Brief.SellingPoints = nil
	bareRequest, err := svc.advertisingTaskRequest("user", ad.Project.ID, bare, req)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(bareRequest.Prompt, "卖点：") || strings.Contains(bareRequest.Prompt, "行动号召：") || !strings.Contains(bareRequest.Prompt, "不添加未经确认") {
		t.Fatalf("unverified product claims leaked into video prompt: %s", bareRequest.Prompt)
	}
	raw, err := json.Marshal(request.Input)
	if err != nil {
		t.Fatal(err)
	}
	var decoded canvasGenerationInput
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("worker cannot decode advertised request: %v", err)
	}
	sub, err := svc.PrepareAdvertisingSubmission("user", ad.Project.ID, id, req)
	if err != nil {
		t.Fatal(err)
	}
	if sub.Quote.AmountMicrocredits != 5 || sub.Task != nil || sub.PublicRequest["referenceResourceIds"] == nil || sub.PublicRequest["resolution"] == nil || sub.PublicRequest["config"] != nil {
		t.Fatalf("quote: %+v", sub)
	}
	var n int64
	if err := db.Model(&model.Task{}).Count(&n).Error; err != nil || n != 0 {
		t.Fatalf("prepare queued task: %d %v", n, err)
	}
	changed := req
	changed.ShotID = draft.Shots[1].ID
	if _, err := svc.PrepareAdvertisingSubmission("user", ad.Project.ID, id, changed); err == nil {
		t.Fatal("operation key changed meaning")
	}
	var wg sync.WaitGroup
	results := make(chan AdvertisingSubmissionDetail, 2)
	errs := make(chan error, 2)
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r, e := svc.ExecuteAdvertisingSubmission("user", ad.Project.ID, id, sub.ID, sub.Quote.QuoteHash)
			results <- r
			errs <- e
		}()
	}
	wg.Wait()
	close(results)
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	taskID := ""
	for r := range results {
		if r.Task == nil {
			t.Fatal("no task")
		}
		if taskID != "" && taskID != r.Task.ID {
			t.Fatal("duplicate task")
		}
		taskID = r.Task.ID
	}
	if err := db.Model(&model.Task{}).Count(&n).Error; err != nil || n != 1 {
		t.Fatalf("tasks=%d %v", n, err)
	}
	if err := db.Model(&model.BillingOrder{}).Count(&n).Error; err != nil || n != 1 {
		t.Fatalf("orders=%d %v", n, err)
	}
	if err := db.Model(&model.AdvertisingSubmission{}).Where("id = ?", sub.ID).Update("expires_at", time.Now().Add(-time.Hour)).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.ChannelModelPriceTier{}).Where("id = ?", "video-tier").Update("unit_price_microcredits", 999).Error; err != nil {
		t.Fatal(err)
	}
	replay, err := svc.ExecuteAdvertisingSubmission("user", ad.Project.ID, id, sub.ID, sub.Quote.QuoteHash)
	if err != nil || replay.Task.ID != taskID {
		t.Fatalf("lost response replay: %+v %v", replay, err)
	}
	if _, err := svc.ExecuteAdvertisingSubmission("other", ad.Project.ID, id, sub.ID, sub.Quote.QuoteHash); err == nil {
		t.Fatal("foreign execute")
	}

	var calls atomic.Int32
	svc.coordinator = platform.NewCoordinatorWithRedis(nil, "advertising-retry-test")
	svc.platform = nil
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1); w.WriteHeader(503) }))
	defer upstream.Close()
	t.Setenv("CANVAS_ALLOWED_PRIVATE_UPSTREAM_HOSTS", "127.0.0.1")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	ctx = context.WithValue(ctx, providerAnalyticsKey{}, providerAnalyticsContext{Service: svc, UserID: "user", TaskID: taskID})
	_, _, createErr := executeIdempotentProtocolCreate(ctx, canvasGenerationInput{Config: providerConfig{BaseURL: upstream.URL}}, protocol.RequestSpec{Method: "POST", Path: "/create", ContentType: "application/json", Body: map[string]any{}}, true)
	if createErr == nil || calls.Load() != 1 || ctx.Err() != nil {
		t.Fatalf("advertising create must stop on first failed attempt: calls=%d ctx=%v err=%v", calls.Load(), ctx.Err(), createErr)
	}
	if err := db.Model(&model.Task{}).Where("id = ?", taskID).Update("status", model.TaskStatusFailed).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := svc.RetryTask("user", taskID); err == nil {
		t.Fatal("retry bypassed approval")
	}
	if _, err := svc.CreateTask("user", CreateTaskRequest{ProjectID: ad.Project.ID, Type: "canvas_video", Prompt: "bypass", Input: map[string]any{"mode": "video"}}); err == nil {
		t.Fatal("direct create bypassed approval")
	}
	// A refunded queued task must remain approval-bound after its project is deleted.
	req.ClientOperationID = "10000000-0000-4000-8000-000000000099"
	quote, err := svc.PrepareAdvertisingSubmission("user", ad.Project.ID, id, req)
	if err != nil {
		t.Fatal(err)
	}
	approved, err := svc.ExecuteAdvertisingSubmission("user", ad.Project.ID, id, quote.ID, quote.Quote.QuoteHash)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CancelTask(context.Background(), "user", approved.Task.ID); err != nil {
		t.Fatal(err)
	}
	if err := svc.DeleteAdvertisingProject("user", ad.Project.ID, confirmed.CurrentRevision); err != nil {
		t.Fatal(err)
	}
	var ordersBefore, ordersAfter int64
	if err := db.Model(&model.BillingOrder{}).Count(&ordersBefore).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := svc.RetryTask("user", approved.Task.ID); err == nil {
		t.Fatal("deleted ad task bypassed fresh quote approval")
	}
	if err := db.Model(&model.BillingOrder{}).Count(&ordersAfter).Error; err != nil {
		t.Fatal(err)
	}
	retained, err := svc.repo.TaskForUser("user", approved.Task.ID)
	if err != nil || retained.Status != model.TaskStatusCancelled || ordersAfter != ordersBefore {
		t.Fatalf("retry changed cancelled task or billing: %+v orders %d->%d err=%v", retained, ordersBefore, ordersAfter, err)
	}
}
