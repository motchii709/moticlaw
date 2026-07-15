# moticlaw

OpenClawにインスパイアされた、Raspberry Pi Zero 2Wで動かすDiscord AIエージェント。
Goで書いてる。

## 概要

- Discordで話しかけるとLLMが応答（Gemini API / Gemma 4）
- シェルコマンド実行（bwrapサンドボックス）、ファイル操作、Web検索等14ツール
- 会話履歴はユーザー別に保存、30分で忘れる
- sandbox + cgroups + SSRFガードで安全性確保

## ビルド

```bash
go build ./...
GOOS=linux GOARCH=arm64 go build -o bin/moticlaw ./cmd/moticlaw
```

## デプロイ

`deploy/firstrun.sh` をSDカードのbootfsに置いてPiを起動すると、勝手にセットアップされる。
詳しくは `AGENTS.md` 参照。

## Discordコマンド

- `/channel add|remove #channel` — 常時応答チャンネル（管理者）
- `/model set|list` — モデル変更
- `/trash` / `/trashclear` — ゴミ箱
- `/skill install|list|remove` — Skill

## ライセンス

MIT
