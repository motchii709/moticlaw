# AGENTS.md — moticlaw

このファイルはopencodeなど агентic coding toolがこのリポジトリで作業する際の参照ドキュメント。
**ここに書かれた制約は「あとで緩める前提のメモ」ではなく、設計上のハードルール。** 特にセキュリティ関連の項目は、機能追加や最適化のために安易に変更・迂回しないこと。変更したくなった場合はコードを書く前に立ち止まって人間に確認する。

## プロジェクト概要

- **名前**: moticlaw
- **何か**: OpenClaw（Discord/Telegram等から操作できるパーソナルAIエージェント）にインスパイアされた、ゼロから自作する個人用クローン
- **動作環境**: Raspberry Pi Zero 2W（aarch64, RAM 512MB）— 軽量であることが絶対条件
- **言語**: Go（単一の静的バイナリ、開発機からクロスコンパイルしてPiに転送）
- **LLM**: Google Gemini API経由の Gemma 4（26B A4B MoE / 31B Dense）と Gemini Lite（Flash-Lite系）。OpenAI互換APIは**スコープ外**（検討したが不採用）
- **想定ユーザー**: 最初は開発者本人のみ。将来複数人がDiscordサーバーを共有して使う前提で、最初からuser_id単位でデータ・権限を分離した設計にする

## 設計の核心思想：モデルの判断を信頼しない

**前提**: モデル（Gemma4/Gemini Lite）はジェイルブレイクされる。プロンプトインジェクション（Web検索結果、画像、外部APIレスポンス経由）も起こる。

したがって：

- 「危険な操作は拒否して」のようなシステムプロンプトでの制御は**しない**（突破されて終わりなので意味がない）
- 安全性はすべて**コード側で構造的に強制する**。モデルが何を言おうと、実行可能な範囲そのものを物理的に絞る
- 「ツールを呼べるかどうか」の判断はDiscordの権限システムやコード内のバリデーションで行い、モデルの自己判断には一切依存しない

新しいツールやコマンドを実装する時は、常に「このツールをジェイルブレイクされたモデルが好き勝手に呼んだら何が起きるか」を最初に検討すること。

## ビルド & 実行

```bash
# 開発機（x86_64）でのビルド・テスト
go build ./...
go vet ./...
go test ./...
go mod verify

# Pi Zero 2W (aarch64) 向けクロスコンパイル
GOOS=linux GOARCH=arm64 go build -o bin/moticlaw ./cmd/moticlaw
```

- Piへの配布: バイナリをscp/rsyncで転送し、`systemd --user`サービスとして登録。ヘッドレス運用のため`loginctl enable-linger`を有効化
- 依存関係は`go.sum`でチェックサム検証必須。サプライチェーン対策としてビルド時に`go mod verify`を通す
- PiにはGoのビルド環境を入れない（Pi上でビルドしない。常にクロスコンパイルしたバイナリを転送する）

## ディレクトリ構成

```
moticlaw/
├── cmd/moticlaw/main.go
├── internal/
│   ├── discord/         # discordgoのイベントハンドラ、スラッシュコマンド
│   ├── llm/             # Gemini/Gemmaクライアント、429検知とフォールバック
│   ├── tools/           # function calling用ツール実装
│   ├── sandbox/         # bubblewrapラッパー、cgroups v2セットアップ
│   ├── store/           # JSON設定 + Markdown永続化（atomic write）
│   └── security/        # Discord権限チェック、パス検証、SSRFガード、レート制限
├── data/                          # ランタイムデータ（.gitignore対象）
│   ├── config/
│   │   ├── channels.json          # /channel add で登録された常時応答チャンネル
│   │   ├── users/<user_id>/model.json
│   │   ├── users/<user_id>/skills.json
│   │   └── cron.json
│   ├── workdir/<user_id>/         # サンドボックスのルート、file_*ツールの操作範囲
│   │   └── .trash/                # file_deleteの退避先
│   ├── memory/<user_id>.md        # 永続メモリ
│   └── logs/exec-YYYYMMDD.md      # 実行ログ
├── .env                            # DISCORD_BOT_TOKEN, GEMINI_API_KEY, GITHUB_PAT(任意) — Piのみ、コミット禁止
├── go.mod / go.sum
└── AGENTS.md
```

## 確定済みの設計判断（変更時は要相談）

### 1. 応答トリガー

- `/channel add`（管理者権限必須）で登録したチャンネル → 全メッセージに応答
- それ以外 → `@メンション`時のみ応答
- 他のbot/webhookからのメッセージは無視する（`message.Author.Bot`チェック）。bot同士の無限ループ事故防止

### 2. モデルとフォールバック

- 利用モデル: `gemma-4-26b-a4b-it`, `gemma-4-31b-it`, Gemini Lite系（Gemini API経由）
- OpenAI互換バックエンドは実装しない
- `/model set`で**ユーザーごとに**使うモデル（プライマリ）を選べる（bot全体で1つの設定ではない）
- RPM/RPDの事前カウントは**しない**。HTTP 429を検知した時だけ対応：
  1. レスポンスヘッダ（`Retry-After`, `x-ratelimit-*`）があれば読んで待機時間を決定。なければ指数バックオフ
  2. 解消しなければ設定済みフォールバック順（例: 31B → 26B → Gemini Lite）で次のモデルへ自動切替
  3. 全モデルが429なら、ラップせず生のエラーをそのままDiscordに返す（ユーザーは技術者なので加工不要。長すぎる場合はコードブロックで整形する程度はOK）
- ユーザー単位・サーバー単位のスパム/コストクールダウンは**入れない**。429リアクティブのみで統一

### 3. 永続化

- 設定系（チャンネル、モデル選択、cronジョブ、skill/MCP登録）→ **JSON**
- 会話履歴 → **JSON**（機械が読み書きし直すため。role+contentの構造をパースする必要がある）
- メモリ・実行ログ → **Markdown**（人間にも読める）
- SQLiteなどのDBエンジンは使わない
- 全書き込みは一時ファイル書き込み→rename のatomic write + mutexで排他制御
- メモリをコンテキストに再注入する際は、**「過去ログ＝データ」として明示的に区切り、指示として解釈されない構造にする**（メモリポイズニング対策）

### 4. シークレット管理

- APIキー（Discord bot token, Gemini API key, 任意のGitHub PAT）は全部`.env`にPi上で直接書く
- Discordコマンドからのキー登録・参照機能は**実装しない**（`/apikey`系は廃止済み）
- モデルのコンテキストに生のキーを渡すことは絶対にしない。HTTPヘッダへの埋め込みはGoコード側でのみ行う

### 5. サンドボックス（shell_exec / file系ツール）

- `shell_exec`は**例外なく**bubblewrap経由で実行する。バイパスする実装経路を作らない
- bwrapの最低限フラグ: `--unshare-all`, `--cap-drop ALL`, `--die-with-parent`, `--new-session`
- ファイルシステム: `/usr`, `/lib`等のシステム領域はread-only bind。書き込み可能なのは`data/workdir/<user_id>/`のみ
- ネットワーク: サンドボックス内は**完全遮断**（ドメイン許可リストは実装しない。複雑さに対して得られる価値が低いと判断）
- リソース制限: `systemd-run`/cgroups v2でCPU・メモリ・プロセス数を上限設定
- ファイル削除は物理削除しない。`file_delete`は`data/workdir/<user_id>/.trash/<timestamp>_<name>`へのmoveのみ。人間が`/trashclear`を実行した時だけ完全削除
- **サンドボックス実行キュー**: グローバル1本（バッファサイズ1）。同時実行1つのみ。他ユーザーのshell_execは待機（60秒タイムアウト）
- **ユーザーRAM予算**: 1人256MB（サンドボックス128MB + 会話キャッシュ + LLMクライアント + バッファ）
  - 会話履歴はメモリ上にキャッシュ、5ターンごとにディスクへフラッシュ
  - 30分アイドルでディスクへ退避してメモリ解放
  - RAM予算超過時は古いセッションから退避

### 6. 外部ネットワークアクセス系ツール（サンドボックスとは別の脅威カテゴリ）

`web_search`/`web_fetch`はGo本体が直接HTTPクライアントで叩くネイティブ実装（bubblewrap経由ではない、ネットワークが必要なので）。これらは**外部に実害（DDoS加害等）を及ぼしうる**ため、以下を必須実装とする：

- グローバルなトークンバケット制限（fetch/search合計で1分あたりN回、ハードコード。モデルに自制を期待しない）
- 同一URLを直近60秒以内に再fetchしようとした場合はキャッシュを返す（同一標的への連打を構造的に防止）
- 1回の応答生成ターンあたりのツール呼び出し上限（暴走ループ対策）
- SSRFガード: ホスト名解決後のIPがプライベート/ループバック/リンクローカル範囲（10.x, 172.16-31.x, 192.168.x, 127.x, 169.254.x）なら接続拒否
- タイムアウトとレスポンスサイズ上限

`github_fetch`（`api.github.com`固定）と`modrinth_fetch`（`api.modrinth.com`固定）は、**接続先ドメインがコード側で固定されている専用クライアント**なので、上記のSSRF/ドメイン制限の対象外（任意URLを受け取らないため）。これらのAPI自体のレート制限に達したら429任せでよい（外部に実害を与えるタイプの問題ではないため）。

`discord_fetch`は全サブアクションが共通関数 `canUserAccessChannel(userID, channelID) bool` を必ず通過すること：

- discordgoの`State.UserChannelPermissions`等を使い、リクエスト送信者の**現在の**ギルドメンバーシップとチャンネル閲覧権限を毎回再検証（長期キャッシュ禁止、短いTTLなら可）
- Bot自身がそのチャンネルを見れない場合は無条件拒否（本人の権限がいくら高くてもBotの可視範囲が天井）
- DMは「そのDMの当事者（本人とBotのみ）」以外は対象にできないようハードコードでブロック
- スレッドは親チャンネルの権限チェック＋スレッド自体のメンバーシップも確認

### 7. Cron

- `cron_register`はモデルが呼べるツールとして実装する。ただし：
  - 通知先は**登録時のチャンネル/ユーザーに固定**。モデルが任意の宛先を指定する余地は構造的に無い
  - 最短実行間隔をコード側で強制（例: 5分未満の登録はエラーで拒否、警告で済ませない）

### 8. Skill / MCP

- ユーザーごとに登録・管理（`data/config/users/<user_id>/skills.json`）
- あるリクエストのツールリストを構築する際、**そのリクエストのuser_idが持つSkill/MCPのみ**を読み込む。他ユーザーのSkillを混在させない
- 組み込みツール（下記10種）は全ユーザー共通のコア機能として常に利用可能。Skill/MCPの有無に関係なく使える

### 9. 権限（Discordネイティブ権限のみで判定、モデル判断は介在しない）

- 管理者専用スラッシュコマンド（Discordの「サーバー管理」権限でチェック）: `/channel add|remove`
- 一般ユーザー向けスラッシュコマンド（特別な権限不要）: `/model set|list`, `/trash`, `/trashclear`, `/skill install|list|remove`
- 一般チャット（メンション/登録済みチャンネル）: 無制限、誰でも利用可

### 10. 組み込みツール一覧（function calling schema実装時の参照）

| ツール | スコープ | 備考 |
|---|---|---|
| `shell_exec` | サンドボックス, `workdir/<user_id>` | ネットワーク無し、bash -c経由（パイプ・リダイレクト可）、30秒タイムアウト、cgroups制限（MemoryMax=128M, CPUQuota=50%, TasksMax=32）、グローバル実行キュー |
| `file_read` / `file_write` | `workdir/<user_id>` | |
| `file_delete` | `workdir/<user_id>` | `.trash/`へ退避、物理削除しない |
| `web_search` / `web_fetch` | グローバル | レート制限・SSRFガード・キャッシュ・ターン内呼び出し上限あり |
| `memory_read` / `memory_write` | ユーザーごとMarkdown | 再注入時はデータとして区切る |
| `cron_register` | ユーザーごと | 宛先固定、最短間隔強制 |
| `discord_fetch` | 呼び出し者の実際のDiscord権限でスコープ | get_messages, get_user, get_channel_info, get_guild_info, search_messages |
| `github_fetch` | 固定ドメイン `api.github.com` | `.env`に任意でPAT（read-only scope推奨） |
| `modrinth_fetch` | 固定ドメイン `api.modrinth.com` | 認証不要 |
| `pi_status` | グローバル | CPU温度（`vcgencmd measure_temp`）、メモリ、uptime。固定コマンドのみ実行、ユーザー入力をシェルに渡さない |

## 採用しなかった案（あえて入れていない。再検討する場合は理由を確認すること）

- OpenAI互換APIバックエンド
- SQLite（JSON + Markdownで統一）
- サンドボックスのネットワークにおけるドメイン許可リスト方式
- LLM API呼び出しの事前RPM/RPDトラッキング（429リアクティブのみ）
- ユーザー単位/サーバー単位のスパム・コストクールダウン
- 「危険操作の実行前にモデル経由で人間に確認を取る」フロー（構造的制約で代替済み）
- Discordコマンド経由のAPIキー登録・管理（`.env`管理に統一）

## コーディング規約

- イディオマティックなGo。`gofmt`/`goimports`を通す
- グローバルな可変状態は`internal/store`の外に置かない（他のパッケージは明示的な依存注入を受け取る）
- 新しいツールを追加する時は、必ず中央のツールディスパッチテーブルに登録する形にする（場当たり的な配線をしない）
- テストはtable-driven推奨。特にセキュリティ関連コード（パス検証、権限チェック、SSRFガード）は**拒否されるべきケースのテストを必ず書く**（成功パスだけのテストでは不十分）

## Discord 出力フォーマット規則

Discord のメッセージボックスは Markdown のサブセットしかサポートしていない。モデル（Gemma/Gemini）が Markdown 全領域を使おうとすると、表示が崩れる。

### 使えるもの（✅）

| 構文 | 例 | 動作 |
|------|-----|------|
| 太字 | `**text**` | **text** |
| 斜体 | `*text*` | *text` |
| 下線 | `__text__` | __text__ |
| 取り消し線 | `~~text~~` | ~~text~~ |
| スポイラー | `\|\|text\|\|` | \|\|text\|\| |
| インラインコード | `` `code` `` | `code` |
| コードブロック | ` ```lang\ncode\n``` ` | 色付き表示 |
| 引用 | `> text` | インデント表示 |
| リスト | `- item` or `1. item` | 箇条書き |
| リンク | `[text](url)` | クリック可能 |
| メンション | `<@user_id>` | 通知付きメンション |

### 使えないもの（❌ — モデルが使おうと阻止する）

| 構文 | 代替手段 |
|------|---------|
| **テーブル** (`\| col \| col \|`) | コードブロック内に整形して貼る |
| **ヘッダー** (`# heading`) | 太字 `**heading**` で代替 |
| **ネストリスト** | 親子関係をインデント（スペース）で表現 |
| **画像埋め込み** (`![alt](url)`) | URL をテキストで貼る |
| **HTML タグ** | 使えない（Markdown のみ） |

### テーブルの代替パターン

```
// ❌ ダメ（Discord で崩れる）
| 名前 | 値 |
|------|-----|
| CPU  | 45°C |

// ✅ OK（コードブロックで整形）
```
名前: CPU
値:   45°C
```

// ✅ OK（箇条書き）
- **CPU**: 45°C
- **メモリ**: 128MB
```

### 実装上の注意

- `bot.go` の `splitMessage` がコードブロック分割を処理する場合、` ``` ` の対が崩れないよう注意
- ツールの戻り値をユーザーに見せる場合、JSON はコードブロックに包む
- 長いリストは分割送信する（Discord の 2000 文字制限）
- モデルの出力をそのまま返すのではなく、Discord に適した形式に変換する関数を `bot.go` に追加することを推奨
