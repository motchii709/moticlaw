package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/moti/moticlaw/internal/discord"
	"github.com/moti/moticlaw/internal/store"
)

func main() {
	// ロガーの設定
	log.SetFlags(log.LstdFlags | log.Lshortfile)

	// 環境変数の読み込み（.envファイルから）
	if err := loadEnv(); err != nil {
		log.Printf("Warning: .env file not found or invalid: %v", err)
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
// 実際の実装ではenvconfigやgodotenvを使う
func loadEnv() error {
	// TODO: .envファイルの読み込み実装
	// 現在はプレースホルダー
	return nil
}
