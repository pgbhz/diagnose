package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
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
}

func TestLoadConversation(t *testing.T) {
	resetGlobals()

	dir := t.TempDir()
	path := dir + "/conv.json"
	data := `{"messages":[{"id":"start","type":"start_message","text":"start here","next":"q"},{"id":"q","type":"question","text":"question","next":"end"},{"id":"end","type":"end_message","text":"bye"}]}`

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
	if startNode.Type != "start_message" {
		t.Fatalf("unexpected start node type %q", startNode.Type)
	}

	if nodes["q"].Next == nil || *nodes["q"].Next != "end" {
		t.Fatalf("question node next not wired correctly")
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

	nodes = map[string]Node{
		"start": {ID: "start", Type: "start_message", Text: "Greeting", Next: strPtr("wrap")},
		"wrap":  {ID: "wrap", Type: "end_message", Text: "Wrap"},
	}
	startNodeID = "start"

	chatID := int64(123)
	msg := &Message{Date: 0, Chat: Chat{ID: chatID, Username: "test"}, Text: "hello"}
	out := captureOutput(t, func() { printMessage(msg) })

	if !strings.Contains(out, "[conversation] chat:123 start: Greeting") {
		t.Fatalf("start output missing, got: %s", out)
	}

	st := chatStateFor(chatID)
	if st.Awaiting != "start" {
		t.Fatalf("expected awaiting start node, got %q", st.Awaiting)
	}

	if len(sent) != 2 {
		t.Fatalf("expected two messages sent, got %d: %v", len(sent), sent)
	}
	if sent[0] != "123:Greeting" {
		t.Fatalf("unexpected greeting message: %s", sent[0])
	}
	if sent[1] != "123:I need a clear photo of the inside of your mouth to continue. Please try sending an image." {
		t.Fatalf("unexpected reminder message: %s", sent[1])
	}

	msg2 := &Message{Date: 1, Chat: Chat{ID: chatID, Username: "test"}, Photo: []PhotoSize{{FileID: "file"}}}
	out2 := captureOutput(t, func() { printMessage(msg2) })

	if classifyCalled != 1 {
		t.Fatalf("expected classifier to be called once, got %d", classifyCalled)
	}

	if !strings.Contains(out2, "[conversation] chat:123 end: Wrap") {
		t.Fatalf("wrap output missing, got: %s", out2)
	}

	if len(sent) != 4 {
		t.Fatalf("expected four messages sent, got %d: %v", len(sent), sent)
	}
	if !strings.Contains(sent[2], "Gemini assessment: YES") {
		t.Fatalf("unexpected diagnosis message: %s", sent[2])
	}
	if sent[3] != "123:Wrap" {
		t.Fatalf("unexpected wrap message: %s", sent[3])
	}

	st = chatStateFor(chatID)
	if st.Awaiting != "" {
		t.Fatalf("expected awaiting cleared, got %q", st.Awaiting)
	}
	if st.HasPending {
		t.Fatalf("expected no pending state")
	}
	if !st.Started {
		t.Fatalf("expected started to remain true")
	}
}

func strPtr(s string) *string {
	return &s
}
