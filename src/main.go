package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"time"

	"telbot/gemini"

	"github.com/joho/godotenv"
)

type Update struct {
	UpdateID      int      `json:"update_id"`
	Message       *Message `json:"message"`
	EditedMessage *Message `json:"edited_message"`
}

type Message struct {
	MessageID int    `json:"message_id"`
	From      *User  `json:"from"`
	Chat      Chat   `json:"chat"`
	Date      int64  `json:"date"`
	Text      string `json:"text"`
}

type User struct {
	ID        int    `json:"id"`
	IsBot     bool   `json:"is_bot"`
	FirstName string `json:"first_name"`
	Username  string `json:"username"`
}

type Chat struct {
	ID       int64  `json:"id"`
	Type     string `json:"type"`
	Title    string `json:"title"`
	Username string `json:"username"`
}

type getUpdatesResp struct {
	OK     bool     `json:"ok"`
	Result []Update `json:"result"`
}

type ConversationFile struct {
	Messages []ConvMessage `json:"messages"`
}

type ConvMessage struct {
	ID   string  `json:"id"`
	Type string  `json:"type"`
	Text string  `json:"text"`
	Next *string `json:"next"`
}

type Node struct {
	ID   string
	Type string
	Text string
	Next *string
}

type ChatState struct {
	Awaiting    string            // question ID we're waiting answer for
	Answers     map[string]string // questionID -> answer text
	HasPending  bool              // true when awaiting any message before continuing
	PendingNext string            // next node to visit once a message arrives
}

// PoemReport represents the expected structured output from Gemini.
type PoemReport struct {
	Poem      string `json:"poem"`
	Rationale string `json:"rationale"`
}

var nodes map[string]Node
var startNodeID string
var states map[int64]*ChatState
var apiBase string
var httpClient *http.Client

var sendReply = sendMessage

func chatStateFor(chatID int64) *ChatState {
	st := states[chatID]
	if st == nil {
		st = &ChatState{Answers: make(map[string]string)}
		states[chatID] = st
	}
	return st
}

func main() {
	_ = godotenv.Load()

	token := os.Getenv("TELEGRAM_TOKEN")
	if token == "" {
		log.Fatal("TELEGRAM_TOKEN not set in environment")
	}

	base := "https://api.telegram.org/bot" + token + "/"
	apiBase = base
	log.Println("Starting telbot long-polling...")

	if prompt := os.Getenv("GEMINI_PROMPT"); prompt != "" {
		log.Printf("sending message to Gemini")
		sendPromptToGemini(prompt)
	} else {
		log.Printf("no GEMINI_PROMPT set, skipping Gemini request")
	}

	// load conversation.json if present
	states = make(map[int64]*ChatState)
	if err := loadConversation("conversation.json"); err != nil {
		log.Printf("warning: could not load conversation.json: %v", err)
	} else {
		log.Printf("conversation loaded, start node: %s", startNodeID)
	}

	offset := 0
	client := &http.Client{Timeout: 60 * time.Second}
	httpClient = client

	for {
		updates, err := getUpdates(client, base, offset, 30)
		if err != nil {
			log.Printf("getUpdates error: %v", err)
			time.Sleep(2 * time.Second)
			continue
		}

		for _, u := range updates {
			if u.UpdateID >= offset {
				offset = u.UpdateID + 1
			}
			if u.Message != nil {
				printMessage(u.Message)
			}
			if u.EditedMessage != nil {
				log.Printf("Edited message: ")
				printMessage(u.EditedMessage)
			}
		}
	}
}

// sendPromptToGemini forwards a prompt to the Gemini API and prints the response.
func sendPromptToGemini(prompt string) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	client, err := gemini.NewClient()
	if err != nil {
		log.Printf("gemini client init error: %v", err)
		return
	}

	schema := map[string]any{
		"type": "array",
		"items": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"poem":      map[string]string{"type": "string"},
				"rationale": map[string]string{"type": "string"},
			},
			"required": []string{"poem", "rationale"},
		},
	}

	opts := &gemini.GenerateOptions{
		ResponseMimeType: "application/json",
		ResponseSchema:   schema,
	}

	responseText, err := client.Ask(ctx, prompt, opts)
	if err != nil {
		log.Printf("gemini ask error: %v", err)
		return
	}

	fmt.Printf("[gemini] raw response: %s\n", responseText)

	var reports []PoemReport
	if err := json.Unmarshal([]byte(responseText), &reports); err != nil {
		log.Printf("gemini response parse error: %v", err)
		return
	}

	for idx, report := range reports {
		fmt.Printf("[gemini] report %d: poem=%s rationale=%s\n", idx+1, report.Poem, report.Rationale)
	}
}

func getUpdates(client *http.Client, base string, offset int, timeout int) ([]Update, error) {
	url := fmt.Sprintf(base+"getUpdates?offset=%d&timeout=%d", offset, timeout)
	resp, err := client.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("telegram API status %d: %s", resp.StatusCode, string(b))
	}

	var r getUpdatesResp
	dec := json.NewDecoder(resp.Body)
	if err := dec.Decode(&r); err != nil {
		return nil, err
	}

	if !r.OK {
		return nil, fmt.Errorf("telegram API returned ok=false")
	}

	return r.Result, nil
}

func printMessage(m *Message) {
	ts := time.Unix(m.Date, 0).Format(time.RFC3339)
	from := ""
	if m.From != nil {
		from = fmt.Sprintf("%s (@%s)", m.From.FirstName, m.From.Username)
	}
	chat := m.Chat.Username
	if chat == "" {
		chat = m.Chat.Title
	}
	// Print raw message
	fmt.Printf("[%s] chat:%s from:%s text:%s\n", ts, chat, from, strconv.Quote(m.Text))

	// If we have a conversation loaded, handle state transitions
	if nodes == nil || startNodeID == "" {
		return
	}

	chID := m.Chat.ID
	st := chatStateFor(chID)

	switch {
	case st.Awaiting != "":
		// Persist the answer and follow the question's next pointer
		qid := st.Awaiting
		st.Answers[qid] = m.Text
		st.Awaiting = ""
		// Move to next node after this question
		if n, ok := nodes[qid]; ok && n.Next != nil {
			advanceChatState(chID, *n.Next)
		}
		return
	case st.HasPending:
		// Consume pending reply and advance to the queued node
		nextID := st.PendingNext
		st.HasPending = false
		st.PendingNext = ""
		if nextID != "" {
			advanceChatState(chID, nextID)
		}
		return
	}

	// No outstanding state: begin conversation at the start node
	if startNodeID != "" {
		advanceChatState(chID, startNodeID)
	}
}

// advanceChatState handles visiting a node ID for a chat
func advanceChatState(chatID int64, nodeID string) {
	n, ok := nodes[nodeID]
	if !ok {
		log.Printf("unknown node %s", nodeID)
		return
	}
	st := chatStateFor(chatID)
	switch n.Type {
	case "start_message":
		// await any input
		st.Awaiting = n.ID

		// print the start message text and move to next if present
		fmt.Printf("[conversation] chat:%d start: %s\n", chatID, n.Text)
		if err := sendReply(chatID, n.Text); err != nil {
			log.Printf("send start message error: %v", err)
		}
		if n.Next != nil {
			st.PendingNext = *n.Next
			st.HasPending = true
		} else {
			st.HasPending = false
			st.PendingNext = ""
		}
	case "question":
		// set awaiting to this question id
		st.Awaiting = n.ID
		fmt.Printf("[conversation] chat:%d question(%s): %s\n", chatID, n.ID, n.Text)
		if err := sendReply(chatID, n.Text); err != nil {
			log.Printf("send question error: %v", err)
		}
	case "end_message":
		// print end text and restart (clear state)
		fmt.Printf("[conversation] chat:%d end: %s\n", chatID, n.Text)
		if err := sendReply(chatID, n.Text); err != nil {
			log.Printf("send end message error: %v", err)
		}

		// show answers stored
		fmt.Printf("[conversation] chat:%d answers: %v\n", chatID, st.Answers)

		// restart: clear state
		states[chatID] = &ChatState{Answers: make(map[string]string)}

		// after restart, call start again
		if startNodeID != "" {
			advanceChatState(chatID, startNodeID)
		}
	default:
		log.Printf("unhandled node type %s", n.Type)
	}
}

func sendMessage(chatID int64, text string) error {
	if httpClient == nil || apiBase == "" {
		return fmt.Errorf("telegram client not initialised")
	}

	values := url.Values{}
	values.Set("chat_id", strconv.FormatInt(chatID, 10))
	values.Set("text", text)

	resp, err := httpClient.PostForm(apiBase+"sendMessage", values)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("sendMessage status %d: %s", resp.StatusCode, string(body))
	}

	return nil
}

// loadConversation loads a conversation JSON file into nodes map
func loadConversation(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	var cf ConversationFile
	dec := json.NewDecoder(f)
	if err := dec.Decode(&cf); err != nil {
		return err
	}
	nodes = make(map[string]Node)
	// Map messages to nodes
	for i, m := range cf.Messages {
		nodes[m.ID] = Node(m)
		// pick first message of type start_message as start
		if startNodeID == "" && m.Type == "start_message" {
			startNodeID = m.ID
		}
		// fallback: if no explicit start, use first message
		if startNodeID == "" && i == 0 {
			startNodeID = m.ID
		}
	}
	return nil
}
