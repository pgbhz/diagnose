package main

import (
	"log"
	"net/http"
	"os"
	"strconv"
	"sync"
	"time"

	"telbot/gemini"

	"github.com/joho/godotenv"
)

// Global runtime state and test seams.
var (
	nodes            map[string]Node
	startNodeID      string
	states           map[int64]*ChatState
	apiBase          string
	httpClient       *http.Client
	assetsDir        string
	maxDownloadBytes int64

	sendReply = sendMessage
	savePhoto = saveIncomingPhoto

	classifyPhoto CancerClassifier = classifyWithGemini

	geminiClient     *gemini.Client
	geminiClientOnce sync.Once
	geminiClientErr  error
)

// chatStateFor retrieves or initializes the state tracking for a chat ID.
func chatStateFor(chatID int64) *ChatState {
	st := states[chatID]
	if st == nil {
		st = &ChatState{Answers: make(map[string]string)}
		states[chatID] = st
	}
	return st
}

// main boots the Telegram polling loop and optional Gemini prompt handling.
func main() {
	_ = godotenv.Load()

	token := os.Getenv("TELEGRAM_TOKEN")
	if token == "" {
		log.Fatal("TELEGRAM_TOKEN not set in environment")
	}

	base := "https://api.telegram.org/bot" + token + "/"
	apiBase = base
	// Configure runtime assets directory and download limits from environment.
	assetsDir = os.Getenv("ASSETS_DIR")
	if assetsDir == "" {
		assetsDir = "assets"
	}

	// MAX_FILE_BYTES is optional, default to 20MB if not set or invalid.
	if v := os.Getenv("MAX_FILE_BYTES"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil && n > 0 {
			maxDownloadBytes = n
		} else {
			maxDownloadBytes = 20 * 1024 * 1024
		}
	} else {
		maxDownloadBytes = 20 * 1024 * 1024
	}
	log.Println("Starting telbot long-polling...")

	// load conversation.json if present
	states = make(map[int64]*ChatState)
	if err := loadConversation("configs/conversation.json"); err != nil {
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
