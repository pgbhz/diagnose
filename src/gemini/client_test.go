package gemini

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestNewClientRequiresAPIKey(t *testing.T) {
	t.Setenv("GEMINI_API_KEY", "")

	_, err := NewClient()
	if err == nil {
		t.Fatal("expected error when API key is missing")
	}
}

func TestAskSuccess(t *testing.T) {
	t.Setenv("GEMINI_API_KEY", "test-key")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("expected POST, got %s", r.Method)
		}

		if r.URL.Path != "/v1beta/models/gemini-2.0-flash-lite:generateContent" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}

		if got := r.URL.Query().Get("key"); got != "test-key" {
			t.Fatalf("unexpected API key: %s", got)
		}

		var req generateRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}

		if len(req.Contents) == 0 || len(req.Contents[0].Parts) == 0 || req.Contents[0].Parts[0].Text != "hello" {
			t.Fatalf("unexpected prompt payload: %#v", req)
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"candidates":[{"content":{"parts":[{"text":"hi there"}]}}]}`))
	}))
	defer server.Close()

	client, err := NewClient(
		WithBaseURL(server.URL),
		WithHTTPClient(server.Client()),
	)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	got, err := client.Ask(context.Background(), "hello", nil)
	if err != nil {
		t.Fatalf("Ask returned error: %v", err)
	}

	if got != "hi there" {
		t.Fatalf("unexpected response: %s", got)
	}
}

func TestAskRejectsEmptyPrompt(t *testing.T) {
	t.Setenv("GEMINI_API_KEY", "test-key")

	client, err := NewClient()
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	if _, err := client.Ask(context.Background(), "", nil); err == nil {
		t.Fatal("expected error for empty prompt")
	}
}

func TestAskHandlesHTTPError(t *testing.T) {
	t.Setenv("GEMINI_API_KEY", "test-key")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"bad request"}`))
	}))
	defer server.Close()

	client, err := NewClient(
		WithBaseURL(server.URL),
		WithHTTPClient(server.Client()),
	)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	if _, err := client.Ask(context.Background(), "hello", nil); err == nil {
		t.Fatal("expected error for HTTP failure")
	}
}

func TestAskWithPartsInlineData(t *testing.T) {
	t.Setenv("GEMINI_API_KEY", "test-key")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req generateRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}

		if len(req.Contents) != 1 {
			t.Fatalf("expected one content, got %d", len(req.Contents))
		}

		parts := req.Contents[0].Parts
		if len(parts) != 2 {
			t.Fatalf("expected two parts, got %d", len(parts))
		}

		if got := parts[0].Text; got != "describe" {
			t.Fatalf("unexpected text part: %s", got)
		}

		if parts[1].InlineData == nil {
			t.Fatal("expected inline data part")
		}

		if parts[1].InlineData.MimeType != "image/jpeg" {
			t.Fatalf("unexpected mime type: %s", parts[1].InlineData.MimeType)
		}

		if parts[1].InlineData.Data != "YWJj" {
			t.Fatalf("unexpected data payload: %s", parts[1].InlineData.Data)
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"candidates":[{"content":{"parts":[{"text":"ok"}]}}]}`))
	}))
	defer server.Close()

	client, err := NewClient(
		WithBaseURL(server.URL),
		WithHTTPClient(server.Client()),
	)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	parts := []Part{
		{Text: "describe"},
		{InlineData: &InlineData{MimeType: "image/jpeg", Data: "YWJj"}},
	}

	got, err := client.AskWithParts(context.Background(), parts, nil)
	if err != nil {
		t.Fatalf("AskWithParts: %v", err)
	}

	if got != "ok" {
		t.Fatalf("unexpected response: %s", got)
	}
}
