package config

import (
	"context"
	"log"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

var Redis *redis.Client

func ConnectRedis() {
	addr := strings.TrimSpace(os.Getenv("REDIS_ADDR"))
	if addr == "" {
		addr = "127.0.0.1:6379"
	}

	dbIndex := 0
	if raw := strings.TrimSpace(os.Getenv("REDIS_DB")); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed >= 0 {
			dbIndex = parsed
		}
	}

	timeout := time.Duration(getEnvInt("REDIS_TIMEOUT_MS", 2000)) * time.Millisecond
	client := redis.NewClient(&redis.Options{
		Addr:         addr,
		Password:     os.Getenv("REDIS_PASSWORD"),
		DB:           dbIndex,
		DialTimeout:  timeout,
		ReadTimeout:  timeout,
		WriteTimeout: timeout,
		PoolTimeout:  timeout,
		PoolSize:     getEnvInt("REDIS_POOL_SIZE", 20),
		MinIdleConns: getEnvInt("REDIS_MIN_IDLE_CONNS", 4),
	})

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	if err := client.Ping(ctx).Err(); err != nil {
		log.Printf("⚠️  Redis connection failed: %v", err)
		Redis = client
		return
	}

	log.Printf("✅ Redis connection successfully opened (%s, db=%d)", addr, dbIndex)
	Redis = client
}
