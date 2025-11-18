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
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// getUpdates polls the Telegram Bot API for new updates, respecting offset.
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

// printMessage logs a message and advances the conversation if needed.
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
	startedNow := false
	if !st.Started && startNodeID != "" {
		startedNow = true
		advanceChatState(chID, startNodeID)
	}
	if startedNow {
		return
	}

	var photoPath string

	if len(m.Photo) > 0 {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		path, err := savePhoto(ctx, m)
		cancel()
		if err != nil {
			log.Printf("error saving photo for chat %d: %v", chID, err)
		} else {
			log.Printf("[photo] chat:%d message:%d saved:%s", chID, m.MessageID, path)
			photoPath = path
		}
	}

	currentNodeID := st.Awaiting
	if currentNodeID != "" {
		node, ok := nodes[currentNodeID]
		if ok {
			if node.Type == "question" {
				text := strings.TrimSpace(m.Text)
				if text == "" {
					if err := sendReply(chID, "Please reply with text so we can continue."); err != nil {
						log.Printf("send text reminder error: %v", err)
					}
					return
				}
				handleQuestionAnswer(chID, currentNodeID, text)
				return
			}
			if node.ExpectPhoto {
				if photoPath == "" {
					if err := sendReply(chID, "I need a clear photo of the inside of your mouth to continue. Please try sending an image."); err != nil {
						log.Printf("send reminder error: %v", err)
					}
					return
				}
				handlePhotoMessage(chID, m, photoPath)
				return
			}
		}
	}
}

// advanceChatState handles visiting a node ID for a chat.
func advanceChatState(chatID int64, nodeID string) {
	n, ok := nodes[nodeID]
	if !ok {
		log.Printf("unknown node %s", nodeID)
		return
	}
	st := chatStateFor(chatID)
	switch n.Type {
	case "start_message":
		st.Started = true
		if n.ExpectPhoto {
			st.Awaiting = n.ID
		} else {
			st.Awaiting = ""
		}

		// print the start message text
		fmt.Printf("[conversation] chat:%d start: %s\n", chatID, n.Text)
		if err := sendReply(chatID, n.Text); err != nil {
			log.Printf("send start message error: %v", err)
		}
		if !n.ExpectPhoto && n.SuccessTransition != nil && *n.SuccessTransition != "" {
			if !applyTransition(chatID, n.ID, true) {
				log.Printf("unable to follow success transition from %s", n.ID)
			}
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
		states[chatID] = &ChatState{Answers: make(map[string]string), Started: true}

		if n.SuccessTransition != nil && *n.SuccessTransition != "" {
			advanceChatState(chatID, *n.SuccessTransition)
		}
	default:
		log.Printf("unhandled node type %s", n.Type)
	}
}

// applyTransition advances the chat based on the outcome of an awaiting node.
func applyTransition(chatID int64, nodeID string, success bool) bool {
	if nodeID == "" {
		return false
	}
	n, ok := nodes[nodeID]
	if !ok {
		log.Printf("transition requested for unknown node %s", nodeID)
		return false
	}

	st := chatStateFor(chatID)
	var nextID *string
	if success {
		nextID = n.SuccessTransition
		if st.Awaiting == nodeID {
			st.Awaiting = ""
		}
	} else {
		nextID = n.FailTransition
		if nextID != nil && *nextID != "" && st.Awaiting == nodeID {
			st.Awaiting = ""
		}
	}

	if nextID == nil || *nextID == "" {
		return false
	}

	advanceChatState(chatID, *nextID)
	return true
}

// handleQuestionAnswer directs username/password inputs through authentication checks.
func handleQuestionAnswer(chatID int64, nodeID, answer string) {
	st := chatStateFor(chatID)
	trimmed := strings.TrimSpace(answer)
	switch nodeID {
	case "login_username":
		if trimmed == "" || !userExists(trimmed) {
			if err := sendReply(chatID, "I couldn't find that username. Please try again."); err != nil {
				log.Printf("send username failure: %v", err)
			}
			applyTransition(chatID, nodeID, false)
			return
		}
		st.Username = trimmed
		applyTransition(chatID, nodeID, true)
	case "login_password":
		if st.Username == "" {
			if err := sendReply(chatID, "Please provide your username before sending the password."); err != nil {
				log.Printf("send username reminder: %v", err)
			}
			applyTransition(chatID, nodeID, false)
			return
		}
		if !verifyPassword(st.Username, trimmed) {
			if err := sendReply(chatID, "The password did not match. Please try again."); err != nil {
				log.Printf("send password failure: %v", err)
			}
			applyTransition(chatID, nodeID, false)
			return
		}
		st.Authed = true
		applyTransition(chatID, nodeID, true)
	default:
		if trimmed != "" {
			st.Answers[nodeID] = trimmed
		}
		applyTransition(chatID, nodeID, true)
	}
}

// loadAuth reads credentials from disk to enable authentication checks.
func loadAuth(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	var af AuthFile
	dec := json.NewDecoder(f)
	if err := dec.Decode(&af); err != nil {
		return err
	}
	authUsers = make(map[string]string, len(af.Users))
	for _, u := range af.Users {
		authUsers[u.Username] = u.Password
	}
	return nil
}

func userExists(username string) bool {
	if authUsers == nil || username == "" {
		return false
	}
	_, ok := authUsers[username]
	return ok
}

func verifyPassword(username, password string) bool {
	if authUsers == nil {
		return false
	}
	stored, ok := authUsers[username]
	if !ok {
		return false
	}
	return stored == password
}

// loadDiagnosis initialises the diagnosis log from disk, creating the file if needed.
func loadDiagnosis(path string) error {
	diagnosisMu.Lock()
	defer diagnosisMu.Unlock()
	diagnosisFile = path
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			diagnosisLog = make(map[string][]DiagnosisEntry)
			return persistDiagnosisLocked()
		}
		return err
	}
	if len(data) == 0 {
		diagnosisLog = make(map[string][]DiagnosisEntry)
		return persistDiagnosisLocked()
	}
	if err := json.Unmarshal(data, &diagnosisLog); err != nil {
		return err
	}
	if diagnosisLog == nil {
		diagnosisLog = make(map[string][]DiagnosisEntry)
	}
	return nil
}

func persistDiagnosisLocked() error {
	if diagnosisFile == "" {
		return fmt.Errorf("diagnosis file path not configured")
	}
	if diagnosisLog == nil {
		diagnosisLog = make(map[string][]DiagnosisEntry)
	}
	data, err := json.MarshalIndent(diagnosisLog, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(diagnosisFile, data, 0600)
}

func recordDiagnosis(username, photoPath string, verdict bool, rationale string) error {
	if username == "" {
		return fmt.Errorf("username is required to record diagnosis")
	}
	entry := DiagnosisEntry{
		PhotoPath: photoPath,
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		Verdict:   verdict,
		Rationale: rationale,
	}
	diagnosisMu.Lock()
	if diagnosisLog == nil {
		diagnosisLog = make(map[string][]DiagnosisEntry)
	}
	diagnosisLog[username] = append(diagnosisLog[username], entry)
	err := persistDiagnosisLocked()
	diagnosisMu.Unlock()
	return err
}

// saveIncomingPhoto retrieves the largest photo variant from a message and writes
// it to the assets directory, returning the saved file path.
func saveIncomingPhoto(ctx context.Context, msg *Message) (string, error) {
	if httpClient == nil || apiBase == "" {
		return "", fmt.Errorf("telegram client not initialised")
	}
	if len(msg.Photo) == 0 {
		return "", fmt.Errorf("message does not contain photo data")
	}

	photo := msg.Photo[len(msg.Photo)-1]
	reqURL := fmt.Sprintf("%sgetFile?file_id=%s", apiBase, url.QueryEscape(photo.FileID))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return "", fmt.Errorf("create getFile request: %w", err)
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("getFile call: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("getFile status %d: %s", resp.StatusCode, string(body))
	}

	var fileResp getFileResp
	dec := json.NewDecoder(resp.Body)
	if err := dec.Decode(&fileResp); err != nil {
		return "", fmt.Errorf("decode getFile response: %w", err)
	}
	if !fileResp.OK || fileResp.Result == nil || fileResp.Result.FilePath == "" {
		return "", fmt.Errorf("telegram getFile returned empty result")
	}

	// If the API provided file size and it's larger than our configured
	// maximum, reject early without attempting to download the file.
	if fileResp.Result.FileSize > 0 && maxDownloadBytes > 0 {
		if int64(fileResp.Result.FileSize) > maxDownloadBytes {
			return "", fmt.Errorf("file size %d exceeds max allowed %d", fileResp.Result.FileSize, maxDownloadBytes)
		}
	}

	downloadBase := strings.Replace(apiBase, "/bot", "/file/bot", 1)
	if downloadBase == apiBase {
		return "", fmt.Errorf("could not derive download base from api base")
	}
	downloadURL := downloadBase + fileResp.Result.FilePath

	dlReq, err := http.NewRequestWithContext(ctx, http.MethodGet, downloadURL, nil)
	if err != nil {
		return "", fmt.Errorf("create download request: %w", err)
	}

	dlResp, err := httpClient.Do(dlReq)
	if err != nil {
		return "", fmt.Errorf("download photo: %w", err)
	}
	defer dlResp.Body.Close()

	if dlResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(dlResp.Body)
		return "", fmt.Errorf("download status %d: %s", dlResp.StatusCode, string(body))
	}

	data, err := io.ReadAll(dlResp.Body)
	if err != nil {
		return "", fmt.Errorf("read photo data: %w", err)
	}

	if err := os.MkdirAll(assetsDir, 0755); err != nil {
		return "", fmt.Errorf("create assets dir: %w", err)
	}

	ext := filepath.Ext(fileResp.Result.FilePath)
	if ext == "" {
		ext = ".jpg"
	}
	fileName := fmt.Sprintf("%d_%d_%d%s", msg.Chat.ID, msg.MessageID, msg.Date, ext)
	localPath := filepath.Join(assetsDir, fileName)

	if err := os.WriteFile(localPath, data, 0644); err != nil {
		return "", fmt.Errorf("write photo: %w", err)
	}

	return localPath, nil
}

// sendMessage posts a text reply to the Telegram Bot API.
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

// loadConversation loads a conversation JSON file into the nodes map.
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
	startNodeID = ""
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

func handlePhotoMessage(chatID int64, msg *Message, photoPath string) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	st := chatStateFor(chatID)
	awaitingID := st.Awaiting
	enqueueChatID(ctx, chatID)

	answer, rationale, err := classifyPhoto(ctx, photoPath)
	if err != nil {
		log.Printf("model analysis error chat:%d message:%d: %v", chatID, msg.MessageID, err)
		if sendErr := sendReply(chatID, "I couldn't analyse that photo. Please try again with a clearer picture or lighting."); sendErr != nil {
			log.Printf("send analysis failure message error: %v", sendErr)
		}
		if awaitingID != "" {
			_ = applyTransition(chatID, awaitingID, false)
		}
		return
	}

	verdict := "No. The image does not appear to show signs consistent with oral cancer."
	if answer {
		verdict = "Yes. The image may show signs consistent with oral cancer."
	}
	if awaitingID != "" {
		st.Answers[awaitingID] = verdict
	}
	if st.Username != "" {
		if err := recordDiagnosis(st.Username, photoPath, answer, rationale); err != nil {
			log.Printf("record diagnosis error: %v", err)
		}
	} else {
		log.Printf("skipping diagnosis log for chat:%d: username not set", chatID)
	}

	reply := fmt.Sprintf("Model's assessment: %s\n\nRationale: %s\n\nThis is an AI assessment and not a medical diagnosis.\nPlease consult a qualified professional for concerns.", verdict, rationale)
	if err := sendReply(chatID, reply); err != nil {
		log.Printf("send diagnosis message error: %v", err)
	}

	if awaitingID != "" {
		if !applyTransition(chatID, awaitingID, true) {
			log.Printf("no success transition defined for node %s", awaitingID)
		}
	}
}
