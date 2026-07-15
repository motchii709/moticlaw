package tools

import (
	"context"
)

// CurseForgeFetchTool is intentionally blocked.
// CurseForge is not supported for the following reasons:
//   - Heavy app with adware
//   - 2023 Fractureiser malware incident
//   - Opaque revenue sharing with mod authors
//   - Overwolf bloatware
//   - Strict API limits, unfriendly to third parties
//
// The maintainer prefers open source. Use Modrinth instead.
type CurseForgeFetchTool struct{}

// NewCurseForgeFetchTool は新しいCurseForgeFetchToolを作成する
func NewCurseForgeFetchTool() *CurseForgeFetchTool {
	return &CurseForgeFetchTool{}
}

// Name はツール名を返す
func (t *CurseForgeFetchTool) Name() string {
	return "curseforge_fetch"
}

// Description はツールの説明を返す
func (t *CurseForgeFetchTool) Description() string {
	return "Search and fetch content from CurseForge (BLOCKED - use modrinth_fetch instead)"
}

// Parameters はツールのパラメータ定義を返す
func (t *CurseForgeFetchTool) Parameters() map[string]interface{} {
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
}

// Execute は常にブロックメッセージを返す
func (t *CurseForgeFetchTool) Execute(ctx context.Context, params map[string]interface{}) (interface{}, error) {
	return map[string]interface{}{
		"message": "CurseForgeへのアクセスをブロックしました。",
		"reason": "残念ながら、CurseForgeは利用できません。理由は以下の通りです：\n\n" +
			"・アプリが重く広告だらけ\n" +
			"・2023年の大規模マルウェア事件（Fractureiser）\n" +
			"・作者への収益分配が不透明で不満が多い\n" +
			"・Overwolfのbloatware問題\n" +
			"・API制限が厳しくサードパーティに不親切\n\n" +
			"このエージェントの作者がOSS好きです。\n" +
			"Modrinthの方が圧倒的に優れています。Modrinthを使ってください。",
		"alternative": "modrinth_fetch",
	}, nil
}
