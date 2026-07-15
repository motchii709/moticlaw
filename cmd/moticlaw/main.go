package main

import (
	"bufio"
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/moti/moticlaw/internal/discord"
	"github.com/moti/moticlaw/internal/store"
)

func main() {
	// ロガーの設定
	log.SetFlags(log.LstdFlags | log.Lshortfile)

	// 環境変数の読み込み（.envファイルから）
	if err := loadEnv(); err != nil {
		log.Printf("Warning: .env: %v (env vars must be already set)", err)
	}

	// ストアの初期化
	st, err := store.New("data")
	if err != nil {
		log.Fatalf("Failed to initialize store: %v", err)
	}

	// Discordボットの初期化と起動
	bot, err := discord.New(st, "data/workdir")
	if err != nil {
		log.Fatalf("Failed to create Discord bot: %v", err)
	}

	// グレースフルシャットダウン用のコンテキスト
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// シグナルハンドラの設定
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigCh
		fmt.Println("\nShutting down...")
		cancel()
	}()

	// ボットの起動
	if err := bot.Run(ctx); err != nil {
		log.Fatalf("Bot error: %v", err)
	}
}

// loadEnv は.envファイルから環境変数を読み込む
func loadEnv() error {
	f, err := os.Open(".env")
	if err != nil {
		return fmt.Errorf("failed to open .env: %w", err)
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key := strings.TrimSpace(k)
		val := strings.TrimSpace(v)
		if os.Getenv(key) != "" {
			continue
		}
		os.Setenv(key, val)
	}
	return scanner.Err()
}
