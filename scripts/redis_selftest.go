package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

type redisConfig struct {
	Enabled   bool   `json:"enabled"`
	Addr      string `json:"addr"`
	Password  string `json:"password"`
	DB        int    `json:"db"`
	KeyPrefix string `json:"keyPrefix"`
}

type config struct {
	Redis redisConfig `json:"redis"`
}

func main() {
	configPath := flag.String("config", "config.json", "path to config json")
	timeoutSec := flag.Int("timeout", 3, "request timeout in seconds")
	flag.Parse()

	cfg, err := loadConfig(*configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "load config failed: %v\n", err)
		os.Exit(1)
	}

	addr := strings.TrimSpace(cfg.Redis.Addr)
	if addr == "" {
		addr = "127.0.0.1:6379"
	}
	keyPrefix := strings.TrimSpace(cfg.Redis.KeyPrefix)
	if keyPrefix == "" {
		keyPrefix = "tool:"
	}

	fmt.Printf("redis.enabled=%v\n", cfg.Redis.Enabled)
	fmt.Printf("redis.addr=%s\n", addr)
	fmt.Printf("redis.db=%d\n", cfg.Redis.DB)
	fmt.Printf("redis.keyPrefix=%s\n", keyPrefix)

	client := redis.NewClient(&redis.Options{
		Addr:         addr,
		Password:     cfg.Redis.Password,
		DB:           cfg.Redis.DB,
		DialTimeout:  time.Duration(*timeoutSec) * time.Second,
		ReadTimeout:  time.Duration(*timeoutSec) * time.Second,
		WriteTimeout: time.Duration(*timeoutSec) * time.Second,
	})
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(*timeoutSec)*time.Second)
	defer cancel()

	if err := client.Ping(ctx).Err(); err != nil {
		fmt.Fprintf(os.Stderr, "PING failed: %v\n", err)
		os.Exit(2)
	}
	fmt.Println("PING ok")

	key := keyPrefix + "codex:redis:selftest"
	value := fmt.Sprintf("ok-%d", time.Now().Unix())
	if err := client.Set(ctx, key, value, 60*time.Second).Err(); err != nil {
		fmt.Fprintf(os.Stderr, "SET failed: %v\n", err)
		os.Exit(3)
	}
	fmt.Printf("SET ok key=%s\n", key)

	got, err := client.Get(ctx, key).Result()
	if err != nil {
		fmt.Fprintf(os.Stderr, "GET failed: %v\n", err)
		os.Exit(4)
	}
	if got != value {
		fmt.Fprintf(os.Stderr, "GET mismatch: got=%q want=%q\n", got, value)
		os.Exit(5)
	}
	fmt.Println("GET ok")

	if err := client.Del(ctx, key).Err(); err != nil {
		fmt.Fprintf(os.Stderr, "DEL failed: %v\n", err)
		os.Exit(6)
	}
	fmt.Println("DEL ok")
	fmt.Println("Redis self-test passed")
}

func loadConfig(path string) (*config, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cfg config
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}
