package tools

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"
)

// DiscordFetchTool はDiscord情報を取得するツール
type DiscordFetchTool struct {
	session *discordgo.Session
	userID  string
}

// NewDiscordFetchTool は新しいDiscordFetchToolを作成する
// session は省略可能（後から SetSession で設定可）。nil の場合は全権限チェックが失敗する。
func NewDiscordFetchTool(userID string, session *discordgo.Session) *DiscordFetchTool {
	return &DiscordFetchTool{
		userID:  userID,
		session: session,
	}
}

// Name はツール名を返す
func (t *DiscordFetchTool) Name() string {
	return "discord_fetch"
}

// Description はツールの説明を返す
func (t *DiscordFetchTool) Description() string {
	return "Discord情報を取得します。権限チェックがあります。"
}

// SetSession はDiscordセッションを設定する
func (t *DiscordFetchTool) SetSession(session *discordgo.Session) {
	t.session = session
}

// canUserAccessChannel はユーザーが指定チャンネルにアクセス可能か確認する
func (t *DiscordFetchTool) canUserAccessChannel(userID, channelID string) bool {
	if t.session == nil {
		return false
	}
	// Bot must be able to see the channel
	ch, err := t.session.Channel(channelID)
	if err != nil {
		return false
	}
	// DM channels: only the recipient and bot can access
	if ch.Type == discordgo.ChannelTypeDM {
		for _, recipient := range ch.Recipients {
			if recipient.ID == userID {
				return true
			}
		}
		return false
	}
	// Guild channels: check user's permissions
	perms, err := t.session.State.UserChannelPermissions(userID, channelID)
	if err != nil {
		return false
	}
	return perms&discordgo.PermissionViewChannel != 0
}

// canUserSeeUser はリクエストユーザーと対象ユーザーが少なくとも1つの共通ギルドに所属しているか確認する
func (t *DiscordFetchTool) canUserSeeUser(requestingUserID, targetUserID string) bool {
	if t.session == nil {
		return false
	}
	for _, guild := range t.session.State.Guilds {
		// リクエストユーザーがこのギルドに所属しているか
		_, err := t.session.GuildMember(guild.ID, requestingUserID)
		if err != nil {
			continue
		}
		// 対象ユーザーも同じギルドに所属しているか
		_, err = t.session.GuildMember(guild.ID, targetUserID)
		if err == nil {
			return true
		}
	}
	return false
}

// canUserAccessGuild はリクエストユーザーが指定ギルドのメンバーか確認する
func (t *DiscordFetchTool) canUserAccessGuild(userID, guildID string) bool {
	if t.session == nil {
		return false
	}
	_, err := t.session.GuildMember(guildID, userID)
	return err == nil
}

// Execute はDiscord情報を取得する
// 全サブアクションは AGENTS.md の要件に従い、呼び出し元ユーザーの権限チェックを必ず通過する。
func (t *DiscordFetchTool) Execute(ctx context.Context, params map[string]interface{}) (interface{}, error) {
	action, ok := params["action"].(string)
	if !ok {
		return nil, fmt.Errorf("action parameter is required")
	}

	// アクションごとに必要なパラメータを検証（session不要）
	switch action {
	case "get_messages", "get_channel_info":
		channelID, _ := params["channel_id"].(string)
		if channelID == "" {
			return nil, fmt.Errorf("channel_id is required for %s", action)
		}

	case "search_messages":
		channelID, _ := params["channel_id"].(string)
		if channelID == "" {
			return nil, fmt.Errorf("channel_id is required for search_messages")
		}

	case "get_user":
		targetUserID, _ := params["user_id"].(string)
		if targetUserID == "" {
			// user_id 未指定 → 自分の情報を取得（常に許可）
			params["user_id"] = t.userID
		}

	case "get_guild_info":
		guildID, _ := params["guild_id"].(string)
		if guildID == "" {
			return nil, fmt.Errorf("guild_id is required for get_guild_info")
		}

	default:
		return nil, fmt.Errorf("unknown action: %s", action)
	}

	// session がなければここでエラー（パラメータ検証より後なので、より具体的なエラーが優先される）
	if t.session == nil {
		return nil, fmt.Errorf("Discord session not initialized")
	}

	// 権限チェック（session必須）
	switch action {
	case "get_messages", "get_channel_info":
		channelID := params["channel_id"].(string)
		if !t.canUserAccessChannel(t.userID, channelID) {
			return nil, fmt.Errorf("access denied: you do not have access to this channel")
		}

	case "search_messages":
		channelID := params["channel_id"].(string)
		if !t.canUserAccessChannel(t.userID, channelID) {
			return nil, fmt.Errorf("access denied: you do not have access to this channel")
		}

	case "get_user":
		targetUserID := params["user_id"].(string)
		if targetUserID != t.userID {
			// 他人の情報を取得するには、同じギルドに所属している必要がある
			if !t.canUserSeeUser(t.userID, targetUserID) {
				return nil, fmt.Errorf("access denied: cannot access this user's information")
			}
		}

	case "get_guild_info":
		guildID := params["guild_id"].(string)
		if !t.canUserAccessGuild(t.userID, guildID) {
			return nil, fmt.Errorf("access denied: you are not a member of this guild")
		}
	}

	switch action {
	case "get_messages":
		return t.getMessages(params)
	case "get_user":
		return t.getUser(params)
	case "get_channel_info":
		return t.getChannelInfo(params)
	case "get_guild_info":
		return t.getGuildInfo(params)
	case "search_messages":
		return t.searchMessages(params)
	default:
		return nil, fmt.Errorf("unknown action: %s", action)
	}
}

// getMessages はメッセージを取得する
func (t *DiscordFetchTool) getMessages(params map[string]interface{}) (interface{}, error) {
	channelID, ok := params["channel_id"].(string)
	if !ok {
		return nil, fmt.Errorf("channel_id parameter is required")
	}

	messages, err := t.session.ChannelMessages(channelID, 100, "", "", "")
	if err != nil {
		return nil, fmt.Errorf("failed to get messages: %w", err)
	}

	// メッセージを簡潔に変換
	result := make([]map[string]interface{}, 0, len(messages))
	for _, msg := range messages {
		result = append(result, map[string]interface{}{
			"id":        msg.ID,
			"content":   msg.Content,
			"author":    msg.Author.Username,
			"timestamp": msg.Timestamp.Format(time.RFC3339),
		})
	}

	return map[string]interface{}{
		"messages": result,
	}, nil
}

// getUser はユーザー情報を取得する
func (t *DiscordFetchTool) getUser(params map[string]interface{}) (interface{}, error) {
	userID, ok := params["user_id"].(string)
	if !ok {
		return nil, fmt.Errorf("user_id parameter is required")
	}

	user, err := t.session.User(userID)
	if err != nil {
		return nil, fmt.Errorf("failed to get user: %w", err)
	}

	return map[string]interface{}{
		"id":            user.ID,
		"username":      user.Username,
		"discriminator": user.Discriminator,
		"avatar":        user.AvatarURL(""),
	}, nil
}

// getChannelInfo はチャンネル情報を取得する
func (t *DiscordFetchTool) getChannelInfo(params map[string]interface{}) (interface{}, error) {
	channelID, ok := params["channel_id"].(string)
	if !ok {
		return nil, fmt.Errorf("channel_id parameter is required")
	}

	channel, err := t.session.Channel(channelID)
	if err != nil {
		return nil, fmt.Errorf("failed to get channel: %w", err)
	}

	return map[string]interface{}{
		"id":      channel.ID,
		"name":    channel.Name,
		"type":    channel.Type,
		"guild_id": channel.GuildID,
	}, nil
}

// getGuildInfo はギルド情報を取得する
func (t *DiscordFetchTool) getGuildInfo(params map[string]interface{}) (interface{}, error) {
	guildID, ok := params["guild_id"].(string)
	if !ok {
		return nil, fmt.Errorf("guild_id parameter is required")
	}

	guild, err := t.session.Guild(guildID)
	if err != nil {
		return nil, fmt.Errorf("failed to get guild: %w", err)
	}

	return map[string]interface{}{
		"id":      guild.ID,
		"name":    guild.Name,
		"owner_id": guild.OwnerID,
	}, nil
}

// searchMessages はチャンネルのメッセージを検索する
func (t *DiscordFetchTool) searchMessages(params map[string]interface{}) (interface{}, error) {
	channelID, ok := params["channel_id"].(string)
	if !ok {
		return nil, fmt.Errorf("channel_id parameter is required")
	}

	query, ok := params["query"].(string)
	if !ok {
		return nil, fmt.Errorf("query parameter is required")
	}

	if !t.canUserAccessChannel(t.userID, channelID) {
		return nil, fmt.Errorf("access denied: cannot access channel %s", channelID)
	}

	limit := 100
	if l, ok := params["limit"].(float64); ok {
		if li := int(l); li > 0 && li <= 100 {
			limit = li
		}
	}

	msgs, err := t.session.ChannelMessages(channelID, limit, "", "", "")
	if err != nil {
		return nil, fmt.Errorf("failed to fetch messages: %w", err)
	}

	var results []map[string]interface{}
	for _, m := range msgs {
		if !strings.Contains(m.Content, query) {
			continue
		}
		results = append(results, map[string]interface{}{
			"id":        m.ID,
			"author":    m.Author.Username,
			"author_id": m.Author.ID,
			"content":   m.Content,
			"timestamp": m.Timestamp,
		})
	}

	return map[string]interface{}{
		"query":   query,
		"channel": channelID,
		"results": results,
		"total":   len(results),
	}, nil
}
