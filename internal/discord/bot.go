package discord

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/moti/moticlaw/internal/conversation"
	"github.com/moti/moticlaw/internal/llm"
	"github.com/moti/moticlaw/internal/sandbox"
	"github.com/moti/moticlaw/internal/security"
	"github.com/moti/moticlaw/internal/store"
	"github.com/moti/moticlaw/internal/tools"
)

// Bot はDiscordボットのメイン構造体
type Bot struct {
	session     *discordgo.Session
	store       *store.Store
	convManager *conversation.Manager // 会話セッション管理
	workDir     string                // サンドボックス作業ディレクトリ (data/workdir)

	mu         sync.RWMutex
	llmClients map[string]*llm.Client // ユーザーごとのLLMクライアント

	// 共有レート制限インスタンス（全メッセージで共有、per-requestで新規作成しない）
	webSearchLimiter *security.RateLimiter
	webFetchLimiter  *security.RateLimiter
}

// New は新しいBotインスタンスを作成する
func New(st *store.Store, workDir string) (*Bot, error) {
	token := os.Getenv("DISCORD_BOT_TOKEN")
	if token == "" {
		return nil, fmt.Errorf("DISCORD_BOT_TOKEN environment variable is required")
	}

	session, err := discordgo.New("Bot " + token)
	if err != nil {
		return nil, fmt.Errorf("failed to create Discord session: %w", err)
	}

	// dataディレクトリ（workDirの親）を会話マネージャに渡す
	dataDir := filepath.Dir(workDir)
	convManager := conversation.NewManager(st, dataDir)
	convManager.Start()

	return &Bot{
		session:           session,
		store:             st,
		convManager:       convManager,
		workDir:           workDir,
		llmClients:        make(map[string]*llm.Client),
		webSearchLimiter:  security.NewRateLimiter(10), // 10回/分
		webFetchLimiter:   security.NewRateLimiter(10), // 10回/分
	}, nil
}

// Run はボットを起動する
func (b *Bot) Run(ctx context.Context) error {
	// シャットダウン時に全セッションをディスクにフラッシュ
	defer b.convManager.Stop()
	// レート制限のgoroutineをクリーンアップ
	defer b.webSearchLimiter.Stop()
	defer b.webFetchLimiter.Stop()

	// イベントハンドラの登録
	b.session.AddHandler(b.onMessageCreate)
	b.session.AddHandler(b.onInteractionCreate)

	// ステートの有効化（権限チェックに必要）
	b.session.StateEnabled = true

	// WebSocket接続
	if err := b.session.Open(); err != nil {
		return fmt.Errorf("failed to open Discord connection: %w", err)
	}
	defer b.session.Close()

	// スラッシュコマンドを登録
	if err := b.registerCommands(); err != nil {
		log.Printf("Warning: failed to register commands: %v", err)
	}

	// cronスケジューラを起動
	cronCtx, cronCancel := context.WithCancel(ctx)
	defer cronCancel()
	go b.cronScheduler(cronCtx)

	log.Println("Bot is now running. Press Ctrl+C to exit.")
	<-ctx.Done()
	return nil
}

// registerCommands はスラッシュコマンドを登録する
func (b *Bot) registerCommands() error {
	commands := []*discordgo.ApplicationCommand{
		{
			Name:        "channel",
			Description: "常時応答チャンネルの管理",
			Options: []*discordgo.ApplicationCommandOption{
				{
					Name:        "action",
					Description: "add or remove",
					Type:        discordgo.ApplicationCommandOptionString,
					Required:    true,
					Choices: []*discordgo.ApplicationCommandOptionChoice{
						{Name: "add", Value: "add"},
						{Name: "remove", Value: "remove"},
					},
				},
				{
					Name:        "channel",
					Description: "対象チャンネル",
					Type:        discordgo.ApplicationCommandOptionChannel,
					Required:    true,
				},
			},
		},
		{
			Name:        "model",
			Description: "使用するプライマリモデルの設定",
			Options: []*discordgo.ApplicationCommandOption{
				{
					Type:        discordgo.ApplicationCommandOptionSubCommand,
					Name:        "set",
					Description: "モデルを設定",
					Options: []*discordgo.ApplicationCommandOption{
						{
							Name:        "model",
							Description: "モデル名",
							Type:        discordgo.ApplicationCommandOptionString,
							Required:    true,
							Choices: []*discordgo.ApplicationCommandOptionChoice{
								{Name: "gemma-4-26b", Value: "gemma-4-26b-a4b-it"},
								{Name: "gemma-4-31b", Value: "gemma-4-31b-it"},
								{Name: "gemini-lite", Value: "gemini-lite"},
							},
						},
					},
				},
				{
					Type:        discordgo.ApplicationCommandOptionSubCommand,
					Name:        "list",
					Description: "利用可能なモデル一覧を表示",
				},
			},
		},
		{
			Name:        "trash",
			Description: "ゴミ箱の中身を確認",
		},
		{
			Name:        "trashclear",
			Description: "ゴミ箱を完全削除",
		},
		{
			Name:        "skill",
			Description: "Skill/MCPの管理",
			Options: []*discordgo.ApplicationCommandOption{
				{
					Type:        discordgo.ApplicationCommandOptionSubCommand,
					Name:        "install",
					Description: "Skillをインストール",
					Options: []*discordgo.ApplicationCommandOption{
						{
							Name:        "name",
							Description: "Skill名",
							Type:        discordgo.ApplicationCommandOptionString,
							Required:    true,
						},
					},
				},
				{
					Type:        discordgo.ApplicationCommandOptionSubCommand,
					Name:        "list",
					Description: "インストール済みSkillの一覧",
				},
				{
					Type:        discordgo.ApplicationCommandOptionSubCommand,
					Name:        "remove",
					Description: "Skillを削除",
					Options: []*discordgo.ApplicationCommandOption{
						{
							Name:        "name",
							Description: "Skill名",
							Type:        discordgo.ApplicationCommandOptionString,
							Required:    true,
						},
					},
				},
			},
		},
	}

	for _, cmd := range commands {
		_, err := b.session.ApplicationCommandCreate(b.session.State.User.ID, "", cmd)
		if err != nil {
			return fmt.Errorf("failed to register command %s: %w", cmd.Name, err)
		}
	}

	return nil
}

// onMessageCreate はメッセージ作成イベントのハンドラ
func (b *Bot) onMessageCreate(s *discordgo.Session, m *discordgo.MessageCreate) {
	// 自分自身のメッセージは無視
	if m.Author.ID == s.State.User.ID {
		return
	}

	// Webhookメッセージは無視（Author.Bot==falseでもWebhookIDが空でない）
	if m.WebhookID != "" {
		return
	}

	// 他のbotのメッセージは無視（無限ループ防止）
	if m.Author.Bot {
		return
	}

	// 応答トリガーの判定
	if !b.shouldRespond(s, m) {
		return
	}

	// LLMへのリクエスト送信
	go b.handleMessage(s, m)
}

// shouldRespond は応答すべきか判定する
func (b *Bot) shouldRespond(s *discordgo.Session, m *discordgo.MessageCreate) bool {
	// 登録済みチャンネルなら全メッセージに応答
	if b.store.IsChannelRegistered(m.ChannelID) {
		return true
	}

	// @メンション時のみ応答
	for _, user := range m.Mentions {
		if user.ID == s.State.User.ID {
			return true
		}
	}

	return false
}

// systemPrompt はDiscord出力フォーマット規則とボットの性格定義
const systemPrompt = `あなたはmoticlaw、Raspberry Pi Zero 2Wで動作するパーソナルAIエージェントです。
技術的な質問に正確に答え、必要に応じてツールを使います。

## Discord出力フォーマット規則（厳守）

DiscordのメッセージボックスはMarkdownのサブセットしかサポートしていません。
以下の構文のみ使用してください：

### 使えるもの（✅）
- 太字: **text**
- 斜体: *text*
- 下線: __text__
- 取り消し線: ~~text~~
- スポイラー: ||text||
- インラインコード: ` + "`" + `code` + "`" + `
- コードブロック: ` + "```" + `lang` + "\ncode\n" + "```" + `
- 引用: > text
- リスト: - item または 1. item
- リンク: [text](url)

### 使えないもの（❌）
- テーブル (| col | col |) → コードブロックに整形
- ヘッダー (# heading) → 太字で代替
- ネストリスト → インデントで表現
- 画像埋め込み → URLをテキストで
- HTMLタグ

### テーブルの代替
コードブロック内で整形するか、箇条書き(- **key**: value)で表現すること。

### その他
- JSONはコードブロックに包むこと
- 長いリストは分割送信（Discordの2000文字制限）
- ツールの戻り値を見せる場合もコードブロックを使う`

const customEmojiThinking = "<a:thinking_spin:1527104850632904725>"

// handleMessage はメッセージを処理する（ツール実行ループ付き）
func (b *Bot) handleMessage(s *discordgo.Session, m *discordgo.MessageCreate) {
	// ステータスメッセージを即座に送信
	statusMsg, _ := s.ChannelMessageSend(m.ChannelID, "\U0001f440")

	// ステータス編集を安全に行うヘルパー
	editStatus := func(content string) {
		if statusMsg != nil {
			if _, err := s.ChannelMessageEdit(m.ChannelID, statusMsg.ID, content); err != nil {
				log.Printf("Failed to edit status message: %v", err)
			}
		}
	}

	// エラー報告ヘルパー（ステータス編集 + ログ + return前提）
	sendError := func(logMsg string) {
		log.Println(logMsg)
		if statusMsg != nil {
			s.ChannelMessageEdit(m.ChannelID, statusMsg.ID, "\u274c\n```\n"+logMsg+"\n```")
		}
	}

	// 1. ユーザーのLLMクライアントを取得
	client, err := b.getLLMClient(m.Author.ID)
	if err != nil {
		sendError(fmt.Sprintf("Failed to get LLM client for user %s: %v", m.Author.ID, err))
		return
	}

	// 2. 会話セッションを取得
	session, err := b.convManager.GetSession(m.Author.ID)
	if err != nil {
		sendError(fmt.Sprintf("Failed to get session for user %s: %v", m.Author.ID, err))
		return
	}

	// 3. メンションを除去してメッセージをクリーンアップ
	content := cleanMessage(m.Content, s.State.User.ID)

	// 4. ユーザーメッセージをセッションに追加
	session.AppendMessage(llm.Message{
		Role:    "user",
		Content: content,
	})

	// 5. ユーザーごとのツールレジストリを作成
	registry, err := tools.DefaultRegistry(b.workDir, m.Author.ID, m.ChannelID, b.convManager.SandboxQueue(), b.webSearchLimiter, b.webFetchLimiter, s)
	if err != nil {
		sendError(fmt.Sprintf("Failed to create tool registry for user %s: %v", m.Author.ID, err))
		return
	}

	// 6. ツール定義をLLM用に変換
	toolDefs := registry.GetDefinitions()
	llmTools := make([]llm.Tool, len(toolDefs))
	for i, td := range toolDefs {
		llmTools[i] = llm.Tool{
			Name:        td.Name,
			Description: td.Description,
			Parameters:  td.Parameters,
		}
	}

	// 7. セッション履歴からメッセージを取得
	historyMsgs := session.GetMessages()

	// 8. タイムアウトコンテキストを作成
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	// 9. ツール実行ループ（最大10回）
	messages := make([]llm.Message, len(historyMsgs))
	copy(messages, historyMsgs)

	editStatus(customEmojiThinking)
	s.ChannelTyping(m.ChannelID)

	var finalResponse string
	hasToolCalls := false
	for i := 0; i < 10; i++ {
		req := &llm.Request{
			SystemPrompt: systemPrompt,
			Messages:     messages,
			Tools:        llmTools,
			MaxTokens:    1024,
		}

		resp, err := client.Generate(ctx, req)
		if err != nil {
			sendError(fmt.Sprintf("LLM generate error for user %s: %v", m.Author.ID, err))
			return
		}

		if len(resp.ToolCalls) == 0 {
			finalResponse = resp.Content
			break
		}

		if !hasToolCalls {
			hasToolCalls = true
			editStatus("\U0001f527")
		}

		for _, tc := range resp.ToolCalls {
			messages = append(messages, llm.Message{
				Role: "assistant",
				FunctionCall: &llm.FunctionCallContent{
					Name: tc.Name,
					Args: tc.Arguments,
				},
			})
		}

		for _, tc := range resp.ToolCalls {
			tool, ok := registry.Get(tc.Name)
			if !ok {
				messages = append(messages, llm.Message{
					Role: "function",
					FunctionResponse: &llm.FunctionResponseContent{
						Name:     tc.Name,
						Response: map[string]interface{}{"error": "unknown tool: " + tc.Name},
					},
				})
				continue
			}

			result, err := tool.Execute(ctx, tc.Arguments)
			if err != nil {
				messages = append(messages, llm.Message{
					Role: "function",
					FunctionResponse: &llm.FunctionResponseContent{
						Name:     tc.Name,
						Response: map[string]interface{}{"error": err.Error()},
					},
				})
			} else {
				messages = append(messages, llm.Message{
					Role: "function",
					FunctionResponse: &llm.FunctionResponseContent{
						Name:     tc.Name,
						Response: result,
					},
				})
			}
		}
	}

	if finalResponse == "" {
		finalResponse = "\uff08\u30c4\u30fc\u30eb\u5b9f\u884c\u56de\u6570\u306e\u4e0a\u9650\u306b\u9054\u3057\u307e\u3057\u305f\uff09"
	}

	// ステータスメッセージを削除（応答送信をもって完了とする）
	if statusMsg != nil {
		s.ChannelMessageDelete(m.ChannelID, statusMsg.ID)
	}

	// 10. AI応答をセッションに追加
	session.AppendMessage(llm.Message{
		Role:    "assistant",
		Content: finalResponse,
	})

	// 11. Discord向けフォーマットを適用
	formatted := formatForDiscord(finalResponse)

	// 12. 応答を送信（2000文字超の場合は分割）
	if len(formatted) > 2000 {
		chunks := splitMessage(formatted, 2000)
		for _, chunk := range chunks {
			if _, err := s.ChannelMessageSend(m.ChannelID, chunk); err != nil {
				log.Printf("Failed to send response chunk: %v", err)
			}
		}
	} else {
		if _, err := s.ChannelMessageSend(m.ChannelID, formatted); err != nil {
			log.Printf("Failed to send response: %v", err)
		}
	}
}

// getLLMClient はユーザーのLLMクライアントを取得する
func (b *Bot) getLLMClient(userID string) (*llm.Client, error) {
	// キャッシュを確認（RLockで読み取り）
	b.mu.RLock()
	if client, ok := b.llmClients[userID]; ok {
		b.mu.RUnlock()
		return client, nil
	}
	b.mu.RUnlock()

	// Load user's model preference from store
	modelName, err := b.store.LoadUserModel(userID)
	if err != nil {
		log.Printf("Failed to load model for user %s: %v, using default", userID, err)
	}

	model := llm.ModelGemma4_26B // default
	if modelName != "" {
		switch modelName {
		case "gemma-4-26b-a4b-it":
			model = llm.ModelGemma4_26B
		case "gemma-4-31b-it":
			model = llm.ModelGemma4_31B
		case "gemini-lite":
			model = llm.ModelGeminiLite
		}
	}

	client, err := llm.NewClient(model)
	if err != nil {
		return nil, err
	}

	// 書き込みはLockで排他
	b.mu.Lock()
	b.llmClients[userID] = client
	b.mu.Unlock()
	return client, nil
}

// cleanMessage はメッセージからメンションを除去する
func cleanMessage(content, botID string) string {
	// メンション形式を除去
	content = strings.Replace(content, fmt.Sprintf("<@%s>", botID), "", -1)
	content = strings.Replace(content, fmt.Sprintf("<@!%s>", botID), "", -1)
	return strings.TrimSpace(content)
}

// splitMessage はメッセージを指定長に分割する
func splitMessage(content string, maxLen int) []string {
	if len(content) <= maxLen {
		return []string{content}
	}

	var chunks []string
	for len(content) > 0 {
		if len(content) <= maxLen {
			chunks = append(chunks, content)
			break
		}

		// 改行位置を探す
		idx := strings.LastIndex(content[:maxLen], "\n")
		if idx == -1 {
			idx = maxLen
		}

		chunks = append(chunks, content[:idx])
		content = content[idx:]
	}

	return chunks
}

// onInteractionCreate はインタラクション（スラッシュコマンド）のハンドラ
func (b *Bot) onInteractionCreate(s *discordgo.Session, i *discordgo.InteractionCreate) {
	// 権限チェック
	if !b.hasPermission(s, i) {
		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Content: "権限がありません",
				Flags:   discordgo.MessageFlagsEphemeral,
			},
		})
		return
	}

	switch i.ApplicationCommandData().Name {
	case "channel":
		b.handleChannelCommand(s, i)
	case "model":
		b.handleModelCommand(s, i)
	case "trash":
		b.handleTrashCommand(s, i)
	case "trashclear":
		b.handleTrashclearCommand(s, i)
	case "skill":
		b.handleSkillCommand(s, i)
	}
}

// hasPermission はユーザーが権限を持っているか確認する
func (b *Bot) hasPermission(s *discordgo.Session, i *discordgo.InteractionCreate) bool {
	// /channelコマンドは管理者権限必須
	if i.ApplicationCommandData().Name == "channel" {
		// ギルドメンバーの権限を取得
		permissions, err := s.UserChannelPermissions(i.Member.User.ID, i.ChannelID)
		if err != nil {
			return false
		}

		// 管理者権限を確認
		return permissions&discordgo.PermissionAdministrator != 0
	}

	// その他のコマンドは誰でも使用可能
	return true
}

// handleChannelCommand は/channelコマンドを処理する
func (b *Bot) handleChannelCommand(s *discordgo.Session, i *discordgo.InteractionCreate) {
	options := i.ApplicationCommandData().Options
	action := options[0].StringValue()
	channelID := options[1].ChannelValue(nil).ID

	var response string
	switch action {
	case "add":
		if err := b.store.AddChannel(channelID); err != nil {
			response = fmt.Sprintf("エラー: %v", err)
		} else {
			response = fmt.Sprintf("<#%s> を常時応答チャンネルに追加しました", channelID)
		}
	case "remove":
		if err := b.store.RemoveChannel(channelID); err != nil {
			response = fmt.Sprintf("エラー: %v", err)
		} else {
			response = fmt.Sprintf("<#%s> を常時応答チャンネルから削除しました", channelID)
		}
	}

	s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Content: response,
		},
	})
}

// handleModelCommand は/modelコマンドを処理する
func (b *Bot) handleModelCommand(s *discordgo.Session, i *discordgo.InteractionCreate) {
	data := i.ApplicationCommandData()
	sub := data.Options[0]
	subName := sub.Name

	var response string
	switch subName {
	case "set":
		modelName := sub.Options[0].StringValue()
		if err := b.store.SaveUserModel(i.Member.User.ID, modelName); err != nil {
			response = fmt.Sprintf("モデルの保存に失敗しました: %v", err)
		} else {
			response = fmt.Sprintf("モデルを %s に設定しました", modelName)
		}
		b.mu.Lock()
		delete(b.llmClients, i.Member.User.ID)
		b.mu.Unlock()
	case "list":
		response = "利用可能なモデル:\n- gemma-4-26b-a4b-it\n- gemma-4-31b-it\n- gemini-lite"
	}

	s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Content: response,
		},
	})
}

// handleTrashCommand は/trashコマンドを処理する
func (b *Bot) handleTrashCommand(s *discordgo.Session, i *discordgo.InteractionCreate) {
	userID := i.Member.User.ID
	trashDir := filepath.Join(b.workDir, userID, ".trash")

	entries, err := os.ReadDir(trashDir)
	if err != nil || len(entries) == 0 {
		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Content: "ゴミ箱は空です",
			},
		})
		return
	}

	var bld strings.Builder
	bld.WriteString("**ゴミ箱:**\n")
	for _, e := range entries {
		info, _ := e.Info()
		size := info.Size()
		modTime := info.ModTime().Format("01/02 15:04")
		sizeStr := fmt.Sprintf("%d B", size)
		if size > 1024 {
			sizeStr = fmt.Sprintf("%.1f KB", float64(size)/1024)
		}
		bld.WriteString(fmt.Sprintf("- `%s` (%s, %s)\n", e.Name(), sizeStr, modTime))
	}

	s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Content: bld.String(),
		},
	})
}

// cronScheduler は定期的にcronジョブを確認して実行する
func (b *Bot) cronScheduler(ctx context.Context) {
	cronPath := filepath.Join(filepath.Dir(b.workDir), "config", "cron.json")
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			b.runDueJobs(ctx, cronPath)
		}
	}
}

// runDueJobs は実行すべきcronジョブを実行する
func (b *Bot) runDueJobs(ctx context.Context, cronPath string) {
	data, err := os.ReadFile(cronPath)
	if err != nil {
		return
	}

	var jobs []tools.CronJob
	if err := json.Unmarshal(data, &jobs); err != nil {
		log.Printf("Cron: failed to parse %s: %v", cronPath, err)
		return
	}

	now := time.Now()
	updated := false

	for i, job := range jobs {
		dur, err := time.ParseDuration(job.Interval)
		if err != nil {
			continue
		}

		lastRun := job.LastRunAt
		if lastRun.IsZero() {
			lastRun = job.CreatedAt
		}
		if lastRun.Add(dur).After(now) {
			continue
		}

		// サンドボックスでコマンドを実行
		sb := sandbox.New(b.workDir, job.UserID, b.convManager.SandboxQueue())
		execCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		output, execErr := sb.Exec(execCtx, job.Command)
		cancel()

		// 結果をDiscordに送信
		msg := fmt.Sprintf("**Cron実行:** `%s`\n", job.Command)
		if execErr != nil {
			msg += fmt.Sprintf("エラー: %v", execErr)
		} else {
			msg += fmt.Sprintf("```\n%s\n```", output)
		}
		if job.ChannelID != "" {
			if _, err := b.session.ChannelMessageSend(job.ChannelID, msg); err != nil {
				log.Printf("Cron: failed to send result to channel %s: %v", job.ChannelID, err)
			}
		}

		// LastRunAtを更新
		jobs[i].LastRunAt = now
		updated = true
	}

	if updated {
		b.saveCronJobs(cronPath, jobs)
	}
}

// saveCronJobs はcron.jsonをアトミックに保存する
func (b *Bot) saveCronJobs(cronPath string, jobs []tools.CronJob) {
	tmpPath := cronPath + ".tmp"
	data, err := json.MarshalIndent(jobs, "", "  ")
	if err != nil {
		log.Printf("Cron: failed to marshal jobs: %v", err)
		return
	}
	if err := os.WriteFile(tmpPath, data, 0644); err != nil {
		log.Printf("Cron: failed to write temp file: %v", err)
		return
	}
	if err := os.Rename(tmpPath, cronPath); err != nil {
		os.Remove(tmpPath)
		log.Printf("Cron: failed to rename: %v", err)
	}
}

// handleTrashclearCommand は/trashclearコマンドを処理する
func (b *Bot) handleTrashclearCommand(s *discordgo.Session, i *discordgo.InteractionCreate) {
	userID := i.Member.User.ID
	trashDir := filepath.Join(b.workDir, userID, ".trash")

	entries, err := os.ReadDir(trashDir)
	if err != nil || len(entries) == 0 {
		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Content: "ゴミ箱は空です",
			},
		})
		return
	}

	count := 0
	for _, e := range entries {
		os.RemoveAll(filepath.Join(trashDir, e.Name()))
		count++
	}

	s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Content: fmt.Sprintf("ゴミ箱を空にしました（%dファイル削除）", count),
		},
	})
}

// handleSkillCommand は/skillコマンドを処理する
func (b *Bot) handleSkillCommand(s *discordgo.Session, i *discordgo.InteractionCreate) {
	data := i.ApplicationCommandData()
	sub := data.Options[0]
	subName := sub.Name
	userID := i.Member.User.ID

	var response string
	switch subName {
	case "install":
		skillName := sub.Options[0].StringValue()
		if err := b.store.SaveUserSkill(userID, skillName); err != nil {
			response = fmt.Sprintf("Skillのインストールに失敗しました: %v", err)
		} else {
			response = fmt.Sprintf("Skill `%s` をインストールしました", skillName)
		}
	case "list":
		skills, err := b.store.LoadUserSkills(userID)
		if err != nil {
			response = fmt.Sprintf("Skill一覧の取得に失敗しました: %v", err)
		} else if len(skills) == 0 {
			response = "インストール済みSkillはありません"
		} else {
			var bld strings.Builder
			bld.WriteString("**インストール済みSkill:**\n")
			for _, sk := range skills {
				bld.WriteString(fmt.Sprintf("- `%s`\n", sk))
			}
			response = bld.String()
		}
	case "remove":
		skillName := sub.Options[0].StringValue()
		if err := b.store.RemoveUserSkill(userID, skillName); err != nil {
			response = fmt.Sprintf("Skillの削除に失敗しました: %v", err)
		} else {
			response = fmt.Sprintf("Skill `%s` を削除しました", skillName)
		}
	}

	s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Content: response,
		},
	})
}
