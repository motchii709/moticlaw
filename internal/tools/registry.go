package tools

import (
	"context"

	"github.com/bwmarrin/discordgo"
	"github.com/moti/moticlaw/internal/security"
)

// Tool はツールのインターフェース
type Tool interface {
	Name() string
	Description() string
	Execute(ctx context.Context, params map[string]interface{}) (interface{}, error)
}

// Registry はツールのレジストリ
type Registry struct {
	tools map[string]Tool
}

// NewRegistry は新しいレジストリを作成する
func NewRegistry() *Registry {
	return &Registry{
		tools: make(map[string]Tool),
	}
}

// Register はツールを登録する
func (r *Registry) Register(tool Tool) {
	r.tools[tool.Name()] = tool
}

// Get はツールを取得する
func (r *Registry) Get(name string) (Tool, bool) {
	tool, ok := r.tools[name]
	return tool, ok
}

// List は登録済みツールの一覧を返す
func (r *Registry) List() []string {
	names := make([]string, 0, len(r.tools))
	for name := range r.tools {
		names = append(names, name)
	}
	return names
}

// DefaultRegistry はデフォルトのツールレジストリを作成する
// searchLimiter と fetchLimiter は共有のレート制限インスタンス（Botレベルで管理）
// session は discord_fetch ツールの権限チェックに使用する。nil の場合は全権限チェックが失敗する。
func DefaultRegistry(workDir, userID string, queue chan struct{}, searchLimiter, fetchLimiter *security.RateLimiter, session *discordgo.Session) (*Registry, error) {
	if err := validateUserID(userID); err != nil {
		return nil, err
	}

	r := NewRegistry()

	// 各ツールを登録
	r.Register(NewShellExecTool(workDir, userID, queue))
	r.Register(NewFileReadTool(workDir, userID))
	r.Register(NewFileWriteTool(workDir, userID))
	r.Register(NewFileDeleteTool(workDir, userID))
	r.Register(NewWebSearchTool(searchLimiter))
	r.Register(NewWebFetchTool(fetchLimiter))
	r.Register(NewMemoryReadTool(userID))
	r.Register(NewMemoryWriteTool(userID))
	r.Register(NewCronRegisterTool(userID))
	r.Register(NewDiscordFetchTool(userID, session))
	r.Register(NewGithubFetchTool())
	r.Register(NewModrinthFetchTool())
	r.Register(NewCurseForgeFetchTool())
	r.Register(NewPiStatusTool())

	return r, nil
}

// ToolDefinition はツールの定義（function calling用）
type ToolDefinition struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	Parameters  map[string]interface{} `json:"parameters"`
}

// GetDefinitions は全ツールの定義を返す
func (r *Registry) GetDefinitions() []ToolDefinition {
	defs := make([]ToolDefinition, 0, len(r.tools))
	for _, tool := range r.tools {
		defs = append(defs, ToolDefinition{
			Name:        tool.Name(),
			Description: tool.Description(),
			Parameters:  getParameters(tool.Name()),
		})
	}
	return defs
}

// getParameters はツールごとのパラメータ定義を返す
func getParameters(toolName string) map[string]interface{} {
	switch toolName {
	case "shell_exec":
		return map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"command": map[string]interface{}{
					"type":        "string",
					"description": "実行するコマンド",
				},
			},
			"required": []string{"command"},
		}
	case "file_read":
		return map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"path": map[string]interface{}{
					"type":        "string",
					"description": "ファイルパス",
				},
			},
			"required": []string{"path"},
		}
	case "file_write":
		return map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"path": map[string]interface{}{
					"type":        "string",
					"description": "ファイルパス",
				},
				"content": map[string]interface{}{
					"type":        "string",
					"description": "ファイル内容",
				},
			},
			"required": []string{"path", "content"},
		}
	case "file_delete":
		return map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"path": map[string]interface{}{
					"type":        "string",
					"description": "ファイルパス",
				},
			},
			"required": []string{"path"},
		}
	case "web_search":
		return map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"query": map[string]interface{}{
					"type":        "string",
					"description": "検索クエリ",
				},
			},
			"required": []string{"query"},
		}
	case "web_fetch":
		return map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"url": map[string]interface{}{
					"type":        "string",
					"description": "取得するURL",
				},
			},
			"required": []string{"url"},
		}
	case "memory_read":
		return map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{},
		}
	case "memory_write":
		return map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"content": map[string]interface{}{
					"type":        "string",
					"description": "書き込む内容",
				},
			},
			"required": []string{"content"},
		}
	case "cron_register":
		return map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"interval": map[string]interface{}{
					"type":        "string",
					"description": "実行間隔（例: 5m, 1h）",
				},
				"command": map[string]interface{}{
					"type":        "string",
					"description": "実行するコマンド",
				},
			},
			"required": []string{"interval", "command"},
		}
	case "discord_fetch":
		return map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"action": map[string]interface{}{
					"type":        "string",
					"description": "アクション",
					"enum":        []string{"get_messages", "get_user", "get_channel_info", "get_guild_info", "search_messages"},
				},
				"channel_id": map[string]interface{}{
					"type":        "string",
					"description": "チャンネルID",
				},
				"user_id": map[string]interface{}{
					"type":        "string",
					"description": "ユーザーID",
				},
				"guild_id": map[string]interface{}{
					"type":        "string",
					"description": "ギルドID（get_guild_infoで必須）",
				},
				"query": map[string]interface{}{
					"type":        "string",
					"description": "検索クエリ",
				},
			},
			"required": []string{"action"},
		}
	case "github_fetch":
		return map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"action": map[string]interface{}{
					"type":        "string",
					"description": "アクション（trending: GitHub Trendingを取得、空の場合はREST API）",
					"enum":        []string{"trending"},
				},
				"endpoint": map[string]interface{}{
					"type":        "string",
					"description": "APIエンドポイント（actionが空の場合に必須）",
				},
				"language": map[string]interface{}{
					"type":        "string",
					"description": "プログラミング言語（trendingアクションの場合、例: go, python）",
				},
				"since": map[string]interface{}{
					"type":        "string",
					"description": "期間（trendingアクションの場合、daily/weekly/monthly）",
					"enum":        []string{"daily", "weekly", "monthly"},
				},
			},
		}
	case "curseforge_fetch":
		return map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"query": map[string]interface{}{
					"type":        "string",
					"description": "Search query (ignored - CurseForge is blocked)",
				},
			},
			"required": []string{"query"},
		}
	case "modrinth_fetch":
		return map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"endpoint": map[string]interface{}{
					"type":        "string",
					"description": "APIエンドポイント",
				},
			},
			"required": []string{"endpoint"},
		}
	case "pi_status":
		return map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{},
		}
	default:
		return map[string]interface{}{}
	}
}
