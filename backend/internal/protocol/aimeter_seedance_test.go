package protocol

import (
	"context"
	"reflect"
	"testing"
)

func TestOfficialAIMeterSeedanceUsesGatewayPaths(t *testing.T) {
	adapter := officialPackageAdapter(t, "aimeter-seedance.yingce-plugin", "aimeter-seedance")
	info := adapter.Metadata()
	if len(info.Categories) != 1 || info.Categories[0] != CapabilityVideo || info.Execution != "declarative" {
		t.Fatalf("metadata = %#v", info)
	}
	request := GenerationRequest{Model: "seedance-2-0-fast", Prompt: "一只橘猫在窗台上伸懒腰", Duration: 5, AspectRatio: "9:16", Resolution: "480p"}
	spec, err := adapter.BuildCreate(context.Background(), RequestContext{BaseURL: "https://aimeter.xk-devops.com", Request: request})
	if err != nil {
		t.Fatal(err)
	}
	if spec.Method != "POST" || spec.Path != "/v1/contents/generations/tasks" {
		t.Fatalf("create spec = %#v", spec)
	}
	body := manifestTestBody(t, spec)
	want := map[string]any{
		"model":      "seedance-2-0-fast",
		"content":    []any{map[string]any{"type": "text", "text": "一只橘猫在窗台上伸懒腰"}},
		"ratio":      "9:16",
		"resolution": "480p",
		"duration":   float64(5),
		// Declarative templates always emit these Ark booleans; verified against the live gateway.
		"generate_audio": false,
		"watermark":      false,
	}
	if !reflect.DeepEqual(body, want) {
		t.Fatalf("body = %#v, want %#v", body, want)
	}
	poll, err := adapter.BuildPoll(context.Background(), PollContext{BaseURL: "https://aimeter.xk-devops.com", TaskID: "JX-1", Model: "seedance-2-0-fast"})
	if err != nil {
		t.Fatal(err)
	}
	if poll.Method != "GET" || poll.Path != "/v1/contents/generations/tasks/JX-1" {
		t.Fatalf("poll spec = %#v", poll)
	}
}
