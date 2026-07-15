package discord

import (
	"regexp"
	"strings"
	"unicode"
)

var htmlTagRegex = regexp.MustCompile(`<[^>]*>`)

// formatForDiscord はモデル出力をDiscord安全なMarkdownに変換する
//
// DiscordのMarkdownサポートは限定的 — テーブル |...| と見出し # は動作しない。
// AGENTS.md > Discord 出力フォーマット規則を参照。
//
// この関数は:
//   - 見出し (# heading) を太字 (**heading**) に置換
//   - Markdownテーブルをコロン区切りのリストに変換
//   - テーブルの区切り行 (| --- | --- |) をスキップ
//   - コードブロック (``` ... ```) はそのまま保持
//   - 他の書式はそのまま維持
func formatForDiscord(text string) string {
	// 前処理: HTMLタグを除去
	text = htmlTagRegex.ReplaceAllString(text, "")

	lines := strings.Split(text, "\n")
	var result []string
	inCodeBlock := false

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		// コードブロックの状態を追跡 — 内部はそのまま保持
		if strings.HasPrefix(trimmed, "```") {
			inCodeBlock = !inCodeBlock
			result = append(result, line)
			continue
		}

		if inCodeBlock {
			result = append(result, line)
			continue
		}

		// ネストされたリストをフラット化（コードブロック外のみ）
		if isNestedListItem(line) {
			line = flattenNestedList(line)
			trimmed = strings.TrimSpace(line)
		}

		// 見出しを太字に置換
		if strings.HasPrefix(trimmed, "#") {
			content := strings.TrimLeft(trimmed, "# ")
			result = append(result, "**"+content+"**")
			continue
		}

		// テーブル行を検出: 2つ以上のパイプ文字を含む行
		pipeCount := strings.Count(trimmed, "|")
		if pipeCount >= 2 {
			// 区切り行かチェック (|, -, :, スペースのみ)
			stripped := strings.ReplaceAll(trimmed, "|", "")
			stripped = strings.ReplaceAll(stripped, "-", "")
			stripped = strings.ReplaceAll(stripped, ":", "")
			stripped = strings.ReplaceAll(stripped, " ", "")
			if stripped == "" {
				// テーブル区切り行 — スキップ
				continue
			}
			// リスト形式に変換: **Header**: value1, value2
			cells := strings.Split(trimmed, "|")
			var cellContents []string
			for _, c := range cells {
				c = strings.TrimSpace(c)
				if c != "" {
					cellContents = append(cellContents, c)
				}
			}
			if len(cellContents) >= 2 {
				result = append(result, "- **"+cellContents[0]+"**: "+strings.Join(cellContents[1:], ", "))
			} else if len(cellContents) == 1 {
				result = append(result, "- "+cellContents[0])
			} else {
				result = append(result, line)
			}
			continue
		}

		result = append(result, line)
	}

	return strings.Join(result, "\n")
}

// isNestedListItem は行がネストされたリスト項目かどうかを判定する
func isNestedListItem(line string) bool {
	if len(line) == 0 {
		return false
	}
	// 先頭が空白で始まり、その後ろにリストマーカー（-, *, 数字.）が続くか
	trimmed := strings.TrimLeft(line, " \t")
	if len(trimmed) == len(line) {
		return false // 先頭に空白がない
	}
	if len(trimmed) >= 2 && (trimmed[0] == '-' || trimmed[0] == '*') && (trimmed[1] == ' ' || trimmed[1] == '\t') {
		return true
	}
	// 数字付きリスト: "1. item"
	if len(trimmed) >= 2 && unicode.IsDigit(rune(trimmed[0])) {
		rest := trimmed[1:]
		if len(rest) >= 2 && (rest[0] == '.' || rest[0] == ')') && (rest[1] == ' ' || rest[1] == '\t') {
			return true
		}
	}
	return false
}

// flattenNestedList はネストされたリスト項目の先頭の空白を除去する
func flattenNestedList(line string) string {
	return strings.TrimLeft(line, " \t")
}
