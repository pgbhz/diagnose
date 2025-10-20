package main

import (
	"bytes"
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
	sent := []string{}
	sendReply = func(id int64, text string) error {
		sent = append(sent, fmt.Sprintf("%d:%s", id, text))
		return nil
	}
	defer func() { sendReply = originalSend }()

	nodes = map[string]Node{
		"0": {ID: "0", Type: "start_message", Text: "Welcome", Next: strPtr("1")},
		"1": {ID: "1", Type: "question", Text: "Answer?", Next: strPtr("2")},
		"2": {ID: "2", Type: "end_message", Text: "Done"},
	}
	startNodeID = "0"

	chatID := int64(123)
	msg := &Message{Date: 0, Chat: Chat{ID: chatID, Username: "test"}, Text: "hi"}
	out := captureOutput(t, func() { printMessage(msg) })

	if !strings.Contains(out, "[conversation] chat:123 start: Welcome") {
		t.Fatalf("start output missing, got: %s", out)
	}

	st := chatStateFor(chatID)
	if st.Awaiting != "0" {
		t.Fatalf("expected awaiting start node, got %q", st.Awaiting)
	}
	if !st.HasPending || st.PendingNext != "1" {
		t.Fatalf("expected pending next '1', got pending=%v next=%q", st.HasPending, st.PendingNext)
	}

	msg2 := &Message{Date: 1, Chat: Chat{ID: chatID, Username: "test"}, Text: "ok"}
	out2 := captureOutput(t, func() { printMessage(msg2) })

	if !strings.Contains(out2, "[conversation] chat:123 question(1): Answer?") {
		t.Fatalf("question output missing, got: %s", out2)
	}

	st = chatStateFor(chatID)
	if st.Awaiting != "1" {
		t.Fatalf("expected awaiting question '1', got %q", st.Awaiting)
	}
	if got := st.Answers["0"]; got != "ok" {
		t.Fatalf("expected answer for start node 'ok', got %q", got)
	}

	msg3 := &Message{Date: 2, Chat: Chat{ID: chatID, Username: "test"}, Text: "answer"}
	out3 := captureOutput(t, func() { printMessage(msg3) })

	if !strings.Contains(out3, "[conversation] chat:123 end: Done") {
		t.Fatalf("end output missing, got: %s", out3)
	}
	if !strings.Contains(out3, `[conversation] chat:123 answers: map[0:ok 1:answer]`) {
		t.Fatalf("answers output missing, got: %s", out3)
	}
	if !strings.Contains(out3, "[conversation] chat:123 start: Welcome") {
		t.Fatalf("restart output missing, got: %s", out3)
	}

	st = chatStateFor(chatID)
	if st.Awaiting != "0" || !st.HasPending || st.PendingNext != "1" {
		t.Fatalf("expected restarted state awaiting start, got awaiting=%q pending=%v next=%q", st.Awaiting, st.HasPending, st.PendingNext)
	}
	if len(st.Answers) != 0 {
		t.Fatalf("expected answers reset, got %v", st.Answers)
	}

	expectedSent := []string{
		"123:Welcome",
		"123:Answer?",
		"123:Done",
		"123:Welcome",
	}
	if fmt.Sprint(sent) != fmt.Sprint(expectedSent) {
		t.Fatalf("unexpected sent messages: got %v want %v", sent, expectedSent)
	}
}

func strPtr(s string) *string {
	return &s
}
