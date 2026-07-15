package discord

import (
	"context"
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
					Name:        "action",
					Description: "set or list",
					Type:        discordgo.ApplicationCommandOptionString,
					Required:    true,
					Choices: []*discordgo.ApplicationCommandOptionChoice{
						{Name: "set", Value: "set"},
						{Name: "list", Value: "list"},
					},
				},
				{
					Name:        "model",
					Description: "モデル名",
					Type:        discordgo.ApplicationCommandOptionString,
					Required:    false,
					Choices: []*discordgo.ApplicationCommandOptionChoice{
						{Name: "gemma-4-26b", Value: "gemma-4-26b-a4b-it"},
						{Name: "gemma-4-31b", Value: "gemma-4-31b-it"},
						{Name: "gemini-lite", Value: "gemini-lite"},
					},
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
					Name:        "action",
					Description: "install, list, or remove",
					Type:        discordgo.ApplicationCommandOptionString,
					Required:    true,
					Choices: []*discordgo.ApplicationCommandOptionChoice{
						{Name: "install", Value: "install"},
						{Name: "list", Value: "list"},
						{Name: "remove", Value: "remove"},
					},
				},
				{
					Name:        "name",
					Description: "Skill名",
					Type:        discordgo.ApplicationCommandOptionString,
					Required:    false,
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

// handleMessage はメッセージを処理する（ツール実行ループ付き）
func (b *Bot) handleMessage(s *discordgo.Session, m *discordgo.MessageCreate) {
	// 1. ユーザーのLLMクライアントを取得
	client, err := b.getLLMClient(m.Author.ID)
	if err != nil {
		log.Printf("Failed to get LLM client for user %s: %v", m.Author.ID, err)
		s.ChannelMessageSend(m.ChannelID, "エラーが発生しました。しばらくしてから再試行してください。")
		return
	}

	// 2. 会話セッションを取得
	session, err := b.convManager.GetSession(m.Author.ID)
	if err != nil {
		log.Printf("Failed to get session for user %s: %v", m.Author.ID, err)
		s.ChannelMessageSend(m.ChannelID, "エラーが発生しました。しばらくしてから再試行してください。")
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
	registry, err := tools.DefaultRegistry(b.workDir, m.Author.ID, b.convManager.SandboxQueue(), b.webSearchLimiter, b.webFetchLimiter, s)
	if err != nil {
		log.Printf("Failed to create tool registry for user %s: %v", m.Author.ID, err)
		s.ChannelMessageSend(m.ChannelID, "エラーが発生しました。しばらくしてから再試行してください。")
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

	var finalResponse string
	for i := 0; i < 10; i++ {
		// 入力中インジケータを表示（Discordで「入力中…」）
		s.ChannelTyping(m.ChannelID)

		req := &llm.Request{
			SystemPrompt: systemPrompt,
			Messages:     messages,
			Tools:        llmTools,
			MaxTokens:    1024,
		}

		resp, err := client.Generate(ctx, req)
		if err != nil {
			log.Printf("LLM generate error for user %s: %v", m.Author.ID, err)
			s.ChannelMessageSend(m.ChannelID, "エラーが発生しました。しばらくしてから再試行してください。")
			return
		}

		// ツール呼び出しがない → 最終テキスト応答
		if len(resp.ToolCalls) == 0 {
			finalResponse = resp.Content
			break
		}

		// モデルの関数呼び出しをメッセージに追加
		for _, tc := range resp.ToolCalls {
			messages = append(messages, llm.Message{
				Role: "assistant",
				FunctionCall: &llm.FunctionCallContent{
					Name: tc.Name,
					Args: tc.Arguments,
				},
			})
		}

		// 各ツールを実行して結果を追加
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
		finalResponse = "（ツール実行回数の上限に達しました）"
	}

	// 10. AI応答をセッションに追加（テキストのみ、FunctionCallは保存しない）
	session.AppendMessage(llm.Message{
		Role:    "assistant",
		Content: finalResponse,
	})

	// 11. Discord向けフォーマットを適用
	formatted := formatForDiscord(finalResponse)

	// 12. Discordに送信（2000文字超の場合は分割）
	if len(formatted) > 2000 {
		chunks := splitMessage(formatted, 2000)
		for _, chunk := range chunks {
			s.ChannelMessageSend(m.ChannelID, chunk)
		}
	} else {
		s.ChannelMessageSend(m.ChannelID, formatted)
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
	options := i.ApplicationCommandData().Options
	action := options[0].StringValue()

	var response string
	switch action {
	case "set":
		if len(options) < 2 {
			response = "モデル名を指定してください"
		} else {
			modelName := options[1].StringValue()
			if err := b.store.SaveUserModel(i.Member.User.ID, modelName); err != nil {
				response = fmt.Sprintf("モデルの保存に失敗しました: %v", err)
			} else {
				response = fmt.Sprintf("モデルを %s に設定しました", modelName)
			}
			// キャッシュを無効化して次回メッセージで新しいモデルを使う
			b.mu.Lock()
			delete(b.llmClients, i.Member.User.ID)
			b.mu.Unlock()
		}
	case "list":
		// 利用可能なモデル一覧を表示
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
	// TODO: ゴミ箱の中身を表示
	s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Content: "ゴミ箱は空です",
		},
	})
}

// handleTrashclearCommand は/trashclearコマンドを処理する
func (b *Bot) handleTrashclearCommand(s *discordgo.Session, i *discordgo.InteractionCreate) {
	// TODO: ゴミ箱を完全削除
	s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Content: "ゴミ箱を完全に削除しました",
		},
	})
}

// handleSkillCommand は/skillコマンドを処理する
func (b *Bot) handleSkillCommand(s *discordgo.Session, i *discordgo.InteractionCreate) {
	options := i.ApplicationCommandData().Options
	action := options[0].StringValue()

	var response string
	switch action {
	case "install":
		if len(options) < 2 {
			response = "Skill名を指定してください"
		} else {
			skillName := options[1].StringValue()
			// TODO: Skillのインストール
			response = fmt.Sprintf("Skill %s をインストールしました", skillName)
		}
	case "list":
		// TODO: インストール済みSkillの一覧を表示
		response = "インストール済みSkillはありません"
	case "remove":
		if len(options) < 2 {
			response = "Skill名を指定してください"
		} else {
			skillName := options[1].StringValue()
			// TODO: Skillの削除
			response = fmt.Sprintf("Skill %s を削除しました", skillName)
		}
	}

	s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Content: response,
		},
	})
}
