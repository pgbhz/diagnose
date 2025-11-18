package main

import (
	"context"
	"log"
	"os"
	"strconv"
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

// enqueueChatID pushes the chat ID into the configured Redis list queue.
func enqueueChatID(ctx context.Context, chatID int64) {
	if queueClient == nil {
		return
	}
	payload := strconv.FormatInt(chatID, 10)
	if err := queueClient.RPush(ctx, queueName, payload).Err(); err != nil {
		log.Printf("redis enqueue error: %v", err)
	}
}
