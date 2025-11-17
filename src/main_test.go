package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func captureOutput(t *testing.T, fn func()) string {
	t.Helper()

	originalStdout := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("failed to create pipe: %v", err)
	}

	os.Stdout = w

	outC := make(chan string)
	go func() {
		var buf bytes.Buffer
		_, _ = io.Copy(&buf, r)
		outC <- buf.String()
	}()

	fn()

	_ = w.Close()
	os.Stdout = originalStdout

	return <-outC
}

func resetGlobals() {
	nodes = nil
	startNodeID = ""
	states = make(map[int64]*ChatState)
	authUsers = nil
	diagnosisLog = nil
	diagnosisFile = ""
}

func TestLoadConversation(t *testing.T) {
	resetGlobals()

	dir := t.TempDir()
	path := dir + "/conv.json"
	data := `{"messages":[{"id":"start","type":"start_message","text":"start here","success_transition":"login","fail_transition":"start"},{"id":"login","type":"question","text":"question","success_transition":"photo","fail_transition":"start"},{"id":"photo","type":"start_message","text":"send photo","success_transition":"end","fail_transition":"photo","expect_photo":true}]}`

	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatalf("failed to write temp conversation: %v", err)
	}

	if err := loadConversation(path); err != nil {
		t.Fatalf("loadConversation returned error: %v", err)
	}

	if startNodeID != "start" {
		t.Fatalf("expected startNodeID 'start', got %q", startNodeID)
	}

	startNode, ok := nodes["start"]
	if !ok {
		t.Fatalf("start node not loaded")
	}
	if startNode.SuccessTransition == nil || *startNode.SuccessTransition != "login" {
		t.Fatalf("start success transition not wired correctly")
	}
	photo := nodes["photo"]
	if !photo.ExpectPhoto {
		t.Fatalf("photo node should expect a photo")
	}
}

func TestConversationFlow(t *testing.T) {
	resetGlobals()

	originalSend := sendReply
	originalClassifier := classifyPhoto
	originalSave := savePhoto
	defer func() {
		sendReply = originalSend
		classifyPhoto = originalClassifier
		savePhoto = originalSave
	}()

	var sent []string
	sendReply = func(id int64, text string) error {
		sent = append(sent, fmt.Sprintf("%d:%s", id, text))
		return nil
	}

	classifyCalled := 0
	classifyPhoto = func(ctx context.Context, path string) (bool, string, error) {
		classifyCalled++
		return true, "looks suspicious", nil
	}

	savePhoto = func(ctx context.Context, msg *Message) (string, error) {
		return "/tmp/fake.jpg", nil
	}

	authUsers = map[string]string{"patient1": "secret"}
	nodes = map[string]Node{
		"start": {
			ID:                "start",
			Type:              "start_message",
			Text:              "Please begin by authenticating with the patient’s username.",
			SuccessTransition: strPtr("login_username"),
			FailTransition:    strPtr("start"),
		},
		"login_username": {
			ID:                "login_username",
			Type:              "question",
			Text:              "Provide the patient’s registered username.",
			SuccessTransition: strPtr("login_password"),
			FailTransition:    strPtr("login_username"),
		},
		"login_password": {
			ID:                "login_password",
			Type:              "question",
			Text:              "Now provide the corresponding password to confirm identity.",
			SuccessTransition: strPtr("photo_prompt"),
			FailTransition:    strPtr("login_username"),
		},
		"photo_prompt": {
			ID:                "photo_prompt",
			Type:              "start_message",
			Text:              "Authentication confirmed. Please upload a photo.",
			SuccessTransition: strPtr("wrap"),
			FailTransition:    strPtr("photo_prompt"),
			ExpectPhoto:       true,
		},
		"wrap": {
			ID:   "wrap",
			Type: "end_message",
			Text: "Wrap",
		},
	}
	startNodeID = "start"

	chatID := int64(123)
	msg := &Message{Date: 0, Chat: Chat{ID: chatID, Username: "test"}, Text: "hello"}
	out := captureOutput(t, func() { printMessage(msg) })

	if !strings.Contains(out, "[conversation] chat:123 start: Please begin") {
		t.Fatalf("start output missing, got: %s", out)
	}
	if len(sent) != 2 {
		t.Fatalf("expected two messages after start, got %d: %v", len(sent), sent)
	}
	if sent[1] != "123:Provide the patient’s registered username." {
		t.Fatalf("expected login username prompt, got %s", sent[1])
	}

	if st := chatStateFor(chatID); st.Awaiting != "login_username" {
		t.Fatalf("expected awaiting login_username, got %q", st.Awaiting)
	}

	// send username
	usernameMsg := &Message{Date: 1, Chat: Chat{ID: chatID, Username: "test"}, Text: "patient1"}
	captureOutput(t, func() { printMessage(usernameMsg) })
	if len(sent) != 3 {
		t.Fatalf("expected login password prompt, got %d messages: %v", len(sent), sent)
	}
	if sent[2] != "123:Now provide the corresponding password to confirm identity." {
		t.Fatalf("unexpected password prompt: %s", sent[2])
	}

	if st := chatStateFor(chatID); st.Username != "patient1" {
		t.Fatalf("expected store username, got %q", st.Username)
	}

	// send password
	passMsg := &Message{Date: 2, Chat: Chat{ID: chatID, Username: "test"}, Text: "secret"}
	captureOutput(t, func() { printMessage(passMsg) })
	if len(sent) != 4 {
		t.Fatalf("expected photo prompt, got %d messages", len(sent))
	}
	if sent[3] != "123:Authentication confirmed. Please upload a photo." {
		t.Fatalf("unexpected photo prompt: %s", sent[3])
	}

	authState := chatStateFor(chatID)
	if authState.Awaiting != "photo_prompt" {
		t.Fatalf("expected photo prompt awaiting, got %q", authState.Awaiting)
	}
	if !authState.Authed {
		t.Fatalf("expected session authenticated before photo upload")
	}

	msgPhoto := &Message{Date: 3, Chat: Chat{ID: chatID, Username: "test"}, Photo: []PhotoSize{{FileID: "file"}}}
	captureOutput(t, func() { printMessage(msgPhoto) })

	if classifyCalled != 1 {
		t.Fatalf("expected classifier to run once, got %d", classifyCalled)
	}

	if len(sent) != 6 {
		t.Fatalf("expected six replies, got %d: %v", len(sent), sent)
	}
	if !strings.Contains(sent[4], "Model's assessment") {
		t.Fatalf("unexpected diagnosis text: %s", sent[4])
	}
	if sent[5] != "123:Wrap" {
		t.Fatalf("unexpected wrap text: %s", sent[5])
	}

	finalState := chatStateFor(chatID)
	if finalState.Awaiting != "" {
		t.Fatalf("expected awaiting cleared, got %q", finalState.Awaiting)
	}
}

func TestRecordDiagnosis(t *testing.T) {
	resetGlobals()
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "diag.json")
	if err := loadDiagnosis(path); err != nil {
		t.Fatalf("loadDiagnosis returned error: %v", err)
	}
	if err := recordDiagnosis("patient1", "/tmp/photo.jpg", true, "looks suspicious"); err != nil {
		t.Fatalf("recordDiagnosis returned error: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed reading diagnosis file: %v", err)
	}
	var log map[string][]DiagnosisEntry
	if err := json.Unmarshal(data, &log); err != nil {
		t.Fatalf("failed to decode diagnosis file: %v", err)
	}
	entries := log["patient1"]
	if len(entries) != 1 {
		t.Fatalf("expected one entry, got %d", len(entries))
	}
	entry := entries[0]
	if entry.PhotoPath != "/tmp/photo.jpg" {
		t.Fatalf("photo path mismatch: %q", entry.PhotoPath)
	}
	if !entry.Verdict {
		t.Fatalf("expected verdict true")
	}
	if entry.Rationale != "looks suspicious" {
		t.Fatalf("rationale mismatch: %q", entry.Rationale)
	}
	if entry.Timestamp == "" {
		t.Fatalf("expected timestamp to be recorded")
	}
}

func strPtr(s string) *string {
	return &s
}
