package app

import (
	"encoding/json"
	"testing"
	"yingce/backend/internal/model"
)

func TestAdvertisingManagedPreparation(t *testing.T) {
	s, db, _ := agentMediaFixture(t)
	for _, row := range []any{
		&model.ChannelModel{ID: "audio-cm", ChannelID: "channel", ModelKey: "tts-1", Capability: "audio", Protocol: model.ChannelInterfaceOpenAIAudio, BillingMode: "fixed_request", PriceConfigured: true, Enabled: true},
		&model.ChannelModelPriceTier{ID: "audio-tier", ChannelModelID: "audio-cm", SelectorKey: "{}", SelectorJSON: "{}", BillingMode: "fixed_request", UnitPriceMicrocredits: 100, PriceConfigured: true, Enabled: true},
	} {
		if err := db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	for _, mode := range []string{"video", "audio"} {
		config := map[string]any{"channelId": "channel", "model": "seedance-test", "size": "9:16", "videoSeconds": "5"}
		if mode == "audio" {
			config = map[string]any{"channelId": "channel", "model": "tts-1", "audioVoice": "alloy", "audioFormat": "mp3", "audioSpeed": "1"}
		}
		req := CreateTaskRequest{Type: "canvas_" + mode, Prompt: "商品展示", Input: map[string]any{"mode": mode, "prompt": "商品展示", "config": config}}
		task, order, sig, err := s.prepareManagedTask("user", req)
		if err != nil || task == nil || order == nil || sig == "" {
			t.Fatalf("%s prepare: task=%+v order=%+v signature=%s err=%v", mode, task, order, sig, err)
		}
		var stored map[string]any
		if err := json.Unmarshal([]byte(task.InputJSON), &stored); err != nil {
			t.Fatal(err)
		}
		if mode == "audio" && stored["config"].(map[string]any)["audioVoice"] != "alloy" {
			t.Fatal("lost audio voice")
		}
	}
	for _, table := range []string{"tasks", "billing_orders", "credit_ledger_entries"} {
		var n int64
		if err := db.Table(table).Count(&n).Error; err != nil || n != 0 {
			t.Fatalf("preparation wrote %s=%d: %v", table, n, err)
		}
	}
	bad := creationTextRequest()
	bad.Input["config"] = map[string]any{"baseUrl": "https://unmanaged.invalid", "apiKey": "must-not-persist", "model": "text-test"}
	if _, _, _, err := s.prepareManagedTask("user", bad); err == nil {
		t.Fatal("unmanaged model accepted")
	}
}

func TestAdvertisingChargeAuthorization(t *testing.T) {
	s, _, _ := agentMediaFixture(t)
	req := CreateTaskRequest{Type: "canvas_video", Prompt: "商品", Input: map[string]any{"mode": "video", "prompt": "商品", "config": map[string]any{"channelId": "channel", "model": "seedance-test", "size": "9:16", "videoSeconds": "5"}}}
	req.advertisingAdmission = &advertisingTaskAdmission{SubmissionID: "submission", MaxCharge: 5}
	task, order, _, err := s.prepareManagedTask("user", req)
	if err != nil {
		t.Fatal(err)
	}
	if !order.ChargeLimitSet || order.ChargeLimitMicrocredits != 5 || task.AuthorizedChargeMicrocredits != 5 || task.AgentRunID != "" {
		t.Fatalf("advertising authorization missing or masquerading as Agent: %+v %+v", task, order)
	}
	req.advertisingAdmission.MaxCharge = 4
	if _, _, _, err := s.prepareManagedTask("user", req); err == nil {
		t.Fatal("approved charge exceeded")
	}
}

func TestAdvertisingBillingCeiling(t *testing.T) {
	s, db, _, _ := creationTestService(t)
	order := model.BillingOrder{ID: "ad-order", UserID: "user", Capability: "audio", BillingMode: "per_second", Status: model.BillingStatusRunning, UnitPriceMicrocredits: 100, Quantity: 1, MultiplierBasisPoints: 10000, AmountMicrocredits: 100, ReservedAmountMicrocredits: 100, ChargeLimitSet: true, ChargeLimitMicrocredits: 100}
	if err := db.Create(&order).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.CreditAccount{}).Where("user_id = ?", "user").Updates(map[string]any{"available_microcredits": 9900, "reserved_microcredits": 100}).Error; err != nil {
		t.Fatal(err)
	}
	if err := s.repo.SettleBillingOrderWithAudioDuration(order.ID, "", 2100); err == nil {
		t.Fatal("over ceiling audio silently settled")
	}
	var account model.CreditAccount
	if err := db.First(&account, "user_id = ?", "user").Error; err != nil {
		t.Fatal(err)
	}
	if account.AvailableMicrocredits != 9900 || account.ReservedMicrocredits != 100 {
		t.Fatalf("over ceiling changed credits: %+v", account)
	}
}
