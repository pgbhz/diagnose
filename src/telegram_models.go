package main

// Update mirrors the Telegram update payload that wraps incoming messages.
type Update struct {
	UpdateID      int      `json:"update_id"`
	Message       *Message `json:"message"`
	EditedMessage *Message `json:"edited_message"`
}

// Message captures the relevant parts of a Telegram chat message.
type Message struct {
	MessageID int         `json:"message_id"`
	From      *User       `json:"from"`
	Chat      Chat        `json:"chat"`
	Date      int64       `json:"date"`
	Text      string      `json:"text"`
	Photo     []PhotoSize `json:"photo"`
}

// PhotoSize captures the photo variants Telegram sends with a message.
type PhotoSize struct {
	FileID       string `json:"file_id"`
	FileUniqueID string `json:"file_unique_id"`
	Width        int    `json:"width"`
	Height       int    `json:"height"`
	FileSize     int    `json:"file_size"`
}

// User represents the Telegram account that sent a message.
type User struct {
	ID        int    `json:"id"`
	IsBot     bool   `json:"is_bot"`
	FirstName string `json:"first_name"`
	Username  string `json:"username"`
}

// Chat contains the destination chat metadata Telegram includes per message.
type Chat struct {
	ID       int64  `json:"id"`
	Type     string `json:"type"`
	Title    string `json:"title"`
	Username string `json:"username"`
}

// getUpdatesResp holds the raw Telegram response for getUpdates polling.
type getUpdatesResp struct {
	OK     bool     `json:"ok"`
	Result []Update `json:"result"`
}

// getFileResp mirrors the Telegram getFile response payload.
type getFileResp struct {
	OK     bool          `json:"ok"`
	Result *telegramFile `json:"result"`
}

// telegramFile contains the file path used to download media from Telegram.
type telegramFile struct {
	FileID       string `json:"file_id"`
	FileUniqueID string `json:"file_unique_id"`
	FileSize     int    `json:"file_size"`
	FilePath     string `json:"file_path"`
}

// ConversationFile models the conversation.json structure.
type ConversationFile struct {
	Messages []ConvMessage `json:"messages"`
}

// ConvMessage defines an individual conversation node from conversation.json.
type ConvMessage struct {
	ID                string  `json:"id"`
	Type              string  `json:"type"`
	Text              string  `json:"text"`
	SuccessTransition *string `json:"success_transition"`
	FailTransition    *string `json:"fail_transition"`
}

// Node stores a normalized conversation node for runtime use.
type Node struct {
	ID                string
	Type              string
	Text              string
	SuccessTransition *string
	FailTransition    *string
}

// ChatState tracks where a chat is within the scripted conversation flow.
type ChatState struct {
	Awaiting string            // node ID awaiting a response
	Answers  map[string]string // questionID -> answer text (reserved for future use)
	Started  bool              // true once we've sent the initial greeting
}
