package backend

import "testing"

func TestAgnesAdapterMapsOpenAIImageRequest(t *testing.T) {
	adapter := NewAgnesAdapter()
	outgoing, err := adapter.BuildRequest(RouteConfig{
		Platform: "agnes",
		BaseURL:  "https://apihub.agnes-ai.com/v1",
	}, OpenAIImageRequest{
		Model:          "agnes-image-2.1-flash",
		Prompt:         "golden hour city",
		Size:           "1536x1024",
		ResponseFormat: "b64_json",
		Quality:        "high",
		N:              1,
	})
	if err != nil {
		t.Fatalf("BuildRequest() error = %v", err)
	}
	if outgoing.Model != "agnes-image-2.1-flash" || outgoing.Size != "1K" || outgoing.Ratio != "3:2" || !outgoing.ReturnBase64 {
		t.Fatalf("outgoing request = %#v", outgoing)
	}
}

func TestAgnesAdapterNormalizesResponse(t *testing.T) {
	response, err := NewAgnesAdapter().ParseResponse([]byte(`{"created":1,"data":[{"url":"https://cdn.example/image.png","b64_json":null}]}`))
	if err != nil {
		t.Fatalf("ParseResponse() error = %v", err)
	}
	if response.Created != 1 || len(response.Data) != 1 || response.Data[0].URL != "https://cdn.example/image.png" {
		t.Fatalf("response = %#v", response)
	}
}

func TestAgnesAdapterBuildsAllOpenAICompatibleEndpoints(t *testing.T) {
	adapter := NewAgnesAdapter()
	config := RouteConfig{BaseURL: "https://apihub.agnes-ai.com/v1"}

	if got := adapter.ModelsEndpoint(config); got != "https://apihub.agnes-ai.com/v1/models" {
		t.Fatalf("ModelsEndpoint() = %q", got)
	}
	if got := adapter.ChatCompletionsEndpoint(config); got != "https://apihub.agnes-ai.com/v1/chat/completions" {
		t.Fatalf("ChatCompletionsEndpoint() = %q", got)
	}
}

func TestDefaultAdapterRegistryListsRegisteredPlatforms(t *testing.T) {
	platforms := NewDefaultAdapterRegistry().Platforms()
	if len(platforms) != 2 {
		t.Fatalf("platform count = %d, want 2", len(platforms))
	}
	want := []PlatformDescriptor{
		{Key: "agnes", DisplayName: "Agnes", Operation: "images/generations", Protocol: "agnes-image"},
		{Key: "openai", DisplayName: "OpenAI", Protocol: "transparent"},
	}
	for index, platform := range platforms {
		if platform != want[index] {
			t.Fatalf("platform[%d] = %#v, want %#v", index, platform, want[index])
		}
	}
}
