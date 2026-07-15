package tools

import (
	"context"
	"testing"
)

func TestDiscordFetchTool_CanUserAccessChannel_NilSession(t *testing.T) {
	tool := NewDiscordFetchTool("test-user", nil)
	// session が nil の場合、常に false を返す
	if tool.canUserAccessChannel("test-user", "123") {
		t.Error("expected false for nil session, got true")
	}
}

func TestDiscordFetchTool_Execute_NilSession(t *testing.T) {
	tool := NewDiscordFetchTool("test-user", nil)

	tests := []struct {
		name    string
		params  map[string]interface{}
		wantErr string
	}{
		{
			name: "nil session with valid params",
			params: map[string]interface{}{
				"action":     "get_messages",
				"channel_id": "123",
			},
			wantErr: "Discord session not initialized",
		},
		{
			name: "nil session with get_user (own info)",
			params: map[string]interface{}{
				"action": "get_user",
			},
			wantErr: "Discord session not initialized",
		},
		{
			name: "nil session with get_guild_info",
			params: map[string]interface{}{
				"action":   "get_guild_info",
				"guild_id": "456",
			},
			wantErr: "Discord session not initialized",
		},
		{
			name: "nil session with search_messages",
			params: map[string]interface{}{
				"action":     "search_messages",
				"channel_id": "123",
				"query":      "test",
			},
			wantErr: "Discord session not initialized",
		},
		{
			name: "nil session with get_channel_info",
			params: map[string]interface{}{
				"action":     "get_channel_info",
				"channel_id": "123",
			},
			wantErr: "Discord session not initialized",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := tool.Execute(context.Background(), tc.params)
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if err.Error() != tc.wantErr {
				t.Errorf("expected error %q, got %q", tc.wantErr, err.Error())
			}
		})
	}
}

func TestDiscordFetchTool_Name(t *testing.T) {
	tool := NewDiscordFetchTool("test-user", nil)
	if tool.Name() != "discord_fetch" {
		t.Errorf("expected name 'discord_fetch', got %q", tool.Name())
	}
}

func TestDiscordFetchTool_Description(t *testing.T) {
	tool := NewDiscordFetchTool("test-user", nil)
	if tool.Description() == "" {
		t.Error("expected non-empty description")
	}
}

func TestDiscordFetchTool_SetSession(t *testing.T) {
	tool := NewDiscordFetchTool("test-user", nil)
	if tool.session != nil {
		t.Error("expected session to be nil initially")
	}
	// SetSession が panic しないことを確認
	tool.SetSession(nil)
	if tool.session != nil {
		t.Error("expected session to remain nil after SetSession(nil)")
	}
}

func TestDiscordFetchTool_NewWithUserID(t *testing.T) {
	tool := NewDiscordFetchTool("user-42", nil)
	if tool.userID != "user-42" {
		t.Errorf("expected userID 'user-42', got %q", tool.userID)
	}
}

func TestDiscordFetchTool_Execute_RequiresChannelID(t *testing.T) {
	tool := NewDiscordFetchTool("test-user", nil)

	tests := []struct {
		name    string
		params  map[string]interface{}
		wantErr string
	}{
		{
			name: "get_messages without channel_id",
			params: map[string]interface{}{
				"action": "get_messages",
			},
			wantErr: "channel_id is required for get_messages",
		},
		{
			name: "get_channel_info without channel_id",
			params: map[string]interface{}{
				"action": "get_channel_info",
			},
			wantErr: "channel_id is required for get_channel_info",
		},
		{
			name: "search_messages without channel_id",
			params: map[string]interface{}{
				"action": "search_messages",
			},
			wantErr: "channel_id is required for search_messages",
		},
		{
			name: "get_guild_info without guild_id",
			params: map[string]interface{}{
				"action": "get_guild_info",
			},
			wantErr: "guild_id is required for get_guild_info",
		},
		{
			name: "unknown action",
			params: map[string]interface{}{
				"action": "nonexistent",
			},
			wantErr: "unknown action: nonexistent",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := tool.Execute(context.Background(), tc.params)
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if err.Error() != tc.wantErr {
				t.Errorf("expected error %q, got %q", tc.wantErr, err.Error())
			}
		})
	}
}

func TestDiscordFetchTool_CanUserSeeUser_NilSession(t *testing.T) {
	tool := NewDiscordFetchTool("test-user", nil)
	if tool.canUserSeeUser("user-a", "user-b") {
		t.Error("expected false for nil session, got true")
	}
}

func TestDiscordFetchTool_CanUserAccessGuild_NilSession(t *testing.T) {
	tool := NewDiscordFetchTool("test-user", nil)
	if tool.canUserAccessGuild("test-user", "guild-123") {
		t.Error("expected false for nil session, got true")
	}
}
