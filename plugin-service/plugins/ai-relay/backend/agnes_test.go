package backend

import "testing"

func TestAgnesAdapterMapsOpenAIImageRequest(t *testing.T) {
	adapter := NewAgnesAdapter()
	outgoing, err := adapter.BuildRequest(RouteConfig{
		Platform:     "agnes",
		BaseURL:      "https://apihub.agnes-ai.com/v1",
		DefaultModel: "agnes-image-2.1-flash",
		QualityMap:   map[string]string{"high": "3K"},
	}, OpenAIImageRequest{
		Model:          "gpt-image-1",
		Prompt:         "golden hour city",
		Size:           "1536x1024",
		ResponseFormat: "b64_json",
		Quality:        "high",
		N:              1,
	})
	if err != nil {
		t.Fatalf("BuildRequest() error = %v", err)
	}
	if outgoing.Model != "agnes-image-2.1-flash" || outgoing.Size != "3K" || outgoing.Ratio != "3:2" || !outgoing.ReturnBase64 {
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
