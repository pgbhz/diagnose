package main

import (
	"context"
	"encoding/json"
	"log"
	"os"
	"sync"

	"github.com/redis/go-redis/v9"
)

var (
	queueClient   *redis.Client
	queueInitOnce sync.Once
	queueName     string
)

// initQueue configures the Redis client used to push chat notifications.
func initQueue() {
	queueInitOnce.Do(func() {
		addr := os.Getenv("REDIS_ADDR")
		if addr == "" {
			addr = "localhost:6379"
		}
		queueName = os.Getenv("CHAT_EVENT_QUEUE")
		if queueName == "" {
			queueName = "diagnosis:chat_events"
		}
		queueClient = redis.NewClient(&redis.Options{Addr: addr})
		if err := queueClient.Ping(context.Background()).Err(); err != nil {
			log.Printf("warning: redis queue ping failed: %v", err)
		} else {
			log.Printf("redis queue ready (%s -> %s)", addr, queueName)
		}
	})
}

// enqueueChatEvent pushes a JSON event (chat_id and optional path) into the Redis list queue.
func enqueueChatEvent(ctx context.Context, chatID int64, path string) {
	if queueClient == nil {
		return
	}
	event := map[string]any{
		"chat_id": chatID,
	}
	if path != "" {
		event["path"] = path
	}
	data, err := json.Marshal(event)
	if err != nil {
		log.Printf("redis marshal error: %v", err)
		return
	}
	if err := queueClient.RPush(ctx, queueName, string(data)).Err(); err != nil {
		log.Printf("redis enqueue error: %v", err)
	}
}

// enqueueChatID kept for backward compatibility; enqueues only the chat ID.
func enqueueChatID(ctx context.Context, chatID int64) {
	enqueueChatEvent(ctx, chatID, "")
}
