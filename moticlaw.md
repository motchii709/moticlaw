# moticlaw — 設計書

## 1. これは何か

**moticlaw**は、OpenClaw（Discord等から操作できるパーソナルAIエージェント）にインスパイアされた、ゼロから自作する個人専用のAIエージェントBot。Raspberry Pi Zero 2W（aarch64, RAM 512MB）という非力なシングルボードコンピュータ上で24時間稼働させ、Discordから完結して操作できることを目指す。

開発者本人が日常的に使うツールとして、Minecraftモッディング・Godotゲーム開発・Flutterアプリ開発などの個人プロジェクトの支援（GitHub状況確認、Modrinth検索、雑務の自動化）と、汎用的なAIチャット相手の両方を兼ねる。

将来的には同じBotインスタンスを複数人で共有する可能性も見据え、最初からユーザー単位でデータと権限を分離した設計にしてある。

## 2. 設計目標と非目標

### 目標
- Pi Zero 2Wの限られたリソース（RAM 512MB）でも安定して動く軽量性
- 初回セットアップ以降は**Discordコマンドだけで運用が完結**する（Piに二度と触らなくていい）
- モデルがジェイルブレイクされることを前提にした、構造的に安全なツール実行
- 将来の複数人共有に耐える、ユーザー単位のデータ分離

### 非目標（明確に採用しなかった案）
- OpenAI互換APIバックエンドのサポート
- SQLiteなどのDBエンジンの利用
- サンドボックス内ネットワークのドメイン許可リスト方式
- LLM APIのRPM/RPDを事前に自前でカウントする仕組み
- ユーザー単位・サーバー単位のスパム/コストクールダウン
- 「危険な操作の実行前にモデル経由で人間に確認を取る」ような、モデルの自制に期待するフロー
- Discordコマンド経由でのAPIキー登録・管理

## 3. 技術スタック

| 項目 | 選定 | 理由 |
|---|---|---|
| 言語 | Go | クロスコンパイルが容易（開発機で`GOOS=linux GOARCH=arm64 go build`一発）。静的バイナリ1つで配布できる。Rustより開発速度が速く、goroutineで非同期処理（Discordイベント・cron・LLM呼び出し）が書きやすい |
| Discordライブラリ | discordgo | 成熟したGo製Discordクライアント。ロール・権限の合成計算（`State.UserChannelPermissions`等）も提供される |
| LLM | Google Gemini API（Gemma 4: 26B A4B MoE / 31B Dense、Gemini Lite系） | Google AI Studio経由で利用。ネイティブのfunction calling対応 |
| サンドボックス | bubblewrap (bwrap) | Flatpakも採用している軽量な非特権サンドボックス。Dockerより圧倒的に軽くPi Zero 2Wでも実用的 |
| リソース制限 | systemd-run + cgroups v2 | CPU/メモリ/プロセス数の上限を強制 |
| 永続化 | JSON（設定）+ Markdown（履歴・ログ） | DBエンジン無しで、人間にも読める軽量な永続化 |

## 4. ハードウェア/デプロイ

- OS: Raspberry Pi OS Lite **64-bit**（Zero 2Wはaarch64対応、`uname -m`で`aarch64`を確認すること）
- RAM 512MBに対してswapfile 2GBを追加し、`vm.swappiness=10`に設定
- 不要サービス（cups, bluetooth, avahi-daemon等）を停止
- ヘッドレス運用のため`gpu_mem=16`でGPUメモリ割り当てを削減
- ビルドはPi上で行わない。開発機でクロスコンパイルしたバイナリをscp/rsyncで転送
- `systemd --user`サービスとして登録し、`loginctl enable-linger`でヘッドレス起動後も自動起動
- ストレージはSDカードよりUSB SSD推奨（書き込み頻度が高くSDは劣化しやすい。Zero 2Wはmicro USB OTGなのでハブ経由になる）

**初回セットアップでPiに触る範囲はここまで**：OS構築 → バイナリ配置 → systemd登録 → `.env`にDiscord bot tokenとGemini APIキー（任意でGitHub PAT）を記入。これ以降の運用は全部Discordコマンドで完結する。

## 5. 設計の核心思想：モデルの判断を信頼しない

moticlawの安全性設計は「モデルはいつか必ずジェイルブレイクされる」という前提に立っている。Web検索結果・画像入力・外部APIレスポンスなど、攻撃者が制御できるあらゆる経路から、モデルに悪意ある指示を注入される可能性がある。

このため：

- システムプロンプトでの「危険な操作は拒否して」のような指示は**意味を持たないものとして扱う**（突破される前提で設計する）
- 安全性は常に**コードレベルで構造的に強制する**。モデルの出力内容に関係なく、実行できる範囲そのものを物理的に絞る
- 権限判定は常にDiscordネイティブの権限システムやコード内バリデーションで行い、モデルの自己判断に依存する箇所を作らない

新機能を追加する際は「これをジェイルブレイクされたモデルに好き勝手呼ばれたら何が起きるか」を必ず最初に検討する。

## 6. ディレクトリ構成

```
moticlaw/
├── cmd/moticlaw/main.go
├── internal/
│   ├── discord/         # discordgoイベントハンドラ、スラッシュコマンド
│   ├── llm/             # Gemini/Gemmaクライアント、429検知とフォールバック
│   ├── tools/           # function calling用ツール実装
│   ├── sandbox/         # bubblewrapラッパー、cgroups v2セットアップ
│   ├── store/           # JSON設定 + Markdown永続化（atomic write）
│   └── security/        # Discord権限チェック、パス検証、SSRFガード、レート制限
├── data/
│   ├── config/
│   │   ├── channels.json
│   │   ├── users/<user_id>/model.json
│   │   ├── users/<user_id>/skills.json
│   │   └── cron.json
│   ├── workdir/<user_id>/
│   │   └── .trash/
│   ├── memory/<user_id>.md
│   └── logs/exec-YYYYMMDD.md
├── .env
├── go.mod / go.sum
└── AGENTS.md / moticlaw.md
```

## 7. 応答トリガー

- `/channel add`（管理者権限必須）で登録されたチャンネル → 全メッセージに応答
- それ以外のチャンネル → `@メンション`時のみ応答
- 他のbot/webhookからのメッセージは無条件で無視（`message.Author.Bot`チェック）。bot同士が反応し合って無限ループ→API爆撃、という事故を防ぐため

## 8. モデルとフォールバック

- 利用モデル: `gemma-4-26b-a4b-it`, `gemma-4-31b-it`, Gemini Lite系（いずれもGemini API経由）
- `/model set`で**ユーザーごとに**プライマリモデルを選択できる（bot全体共通の設定ではない）
- RPM/RPDの事前カウントは行わない。理由：Gemini APIには事前にクォータ残量だけを問い合わせる専用エンドポイントが存在せず、実際にリクエストを送った際のレスポンスヘッダかAI Studioのダッシュボードでしか確認できないため、事前カウントの仕組みを自作する価値が薄い
- HTTP 429を検知した時の挙動：
  1. レスポンスヘッダ（`Retry-After`, `x-ratelimit-*`）があれば読んで待機時間を決定。無ければ指数バックオフ（1秒→2秒→4秒…）
  2. 解消しない場合、設定済みのフォールバック順（例: 31B → 26B → Gemini Lite）で次のモデルに自動切替し、同じリクエストを再投げ
  3. 全モデルが429の場合、エラーをラップせず**生のまま**Discordに返す（ユーザーは技術者なので過剰な親切設計は不要。長大な場合はコードブロックで整形する程度の処理は行う）
- ユーザー単位・サーバー単位のスパム/コストクールダウンは導入しない。429リアクティブのみで一貫させる

## 9. 永続化

- 設定系（チャンネル登録、モデル選択、cronジョブ、Skill/MCP登録）→ **JSON**
- 履歴・永続メモリ・実行ログ → **Markdown**
- 全書き込みは一時ファイル書き込み→rename によるatomic write + mutexでの排他制御（DBエンジンなしでも壊れない構成）
- **メモリポイズニング対策**：永続メモリをモデルのコンテキストに再注入する際は、「これはユーザーの過去ログ＝データ」であることを明示的に区切り、指示として解釈されない構造にする。攻撃的な入力でメモリに変な内容を書き込ませて、それが将来「事実」として読み込まれ続けるループを防ぐ

## 10. シークレット管理

- Discord bot token, Gemini APIキー, 任意のGitHub PATは全て`.env`にPi上で直接記述する
- Discordコマンド経由でのキー登録・参照は実装しない（一度Modal入力方式・暗号化保存方式を検討したが、シンプルさを優先して`.env`管理に統一）
- モデルのコンテキストに生のキー値を渡すことは絶対にしない。HTTPヘッダへの埋め込みはGoコード側のみで行い、モデルはキーの存在自体を知らない

## 11. サンドボックス（shell_exec / file系ツール）

- `shell_exec`は例外なくbubblewrap経由で実行する。バイパス経路を作らない
- bwrapの最低限フラグ：
  - `--unshare-all`（net, pid, ipc, uts全分離）
  - `--cap-drop ALL`（全capability剥奪）
  - `--die-with-parent`（親プロセス死亡時に確実終了、ゾンビ化防止）
  - `--new-session`
- ファイルシステム：`/usr`, `/lib`等のシステム領域はread-only bind。書き込み可能なのは`data/workdir/<user_id>/`のみ
- ネットワーク：サンドボックス内は完全遮断。ドメイン許可リストは「運用コストに対して得られる安全性向上が薄い」と判断し採用しなかった
- リソース制限：systemd-run経由のcgroups v2でCPU quota・MemoryMax・プロセス数上限を設定
- **削除の扱い**：`file_delete`は物理削除せず、`data/workdir/<user_id>/.trash/<timestamp>_<filename>`へのmoveのみ。人間が`/trashclear`を実行した時だけ完全削除される。`/trash`で現在のゴミ箱の中身を確認できる

## 12. 外部ネットワークアクセス系ツール（サンドボックスとは別の脅威カテゴリ）

`web_search`/`web_fetch`はGo本体が直接HTTPクライアントで叩くネイティブ実装（ネットワークが必要なため、bubblewrapを経由しない）。

これらは「APIの無料枠を溶かす」だけでなく**外部に実害（自宅回線が意図せずDDoSの踏み台になる等）を及ぼしうる**ため、ハードキャップが必須：

| 対策 | 内容 |
|---|---|
| グローバルレート制限 | fetch/search合計で「1分間にN回まで」をコード側で強制（モデルの自制に期待しない） |
| 同一URL重複防止 | 直近60秒以内に同じURLをfetch済みなら再送せずキャッシュを返す |
| ターン内呼び出し上限 | 1回の応答生成サイクルでの呼び出し回数を制限（暴走ループ対策） |
| cron最小間隔の強制 | `cron_register`で短すぎる間隔（例: 5分未満）は登録自体を拒否 |
| SSRFガード | 解決先IPがプライベート/ループバック/リンクローカル範囲（10.x, 172.16-31.x, 192.168.x, 127.x, 169.254.x）なら拒否 |
| タイムアウト/サイズ上限 | 1回のfetchの所要時間・レスポンスサイズに上限 |

`github_fetch`（`api.github.com`固定）と`modrinth_fetch`（`api.modrinth.com`固定）は接続先ドメインがコード側で固定された専用クライアントなので、上記SSRF/ドメイン制限の対象外（任意URLを受け取らない）。これらのAPI自体のレート制限到達は429任せでよい（外部に実害を与えるタイプの問題ではないため）。

## 13. discord_fetchツールの権限チェック

`discord_fetch`はBot自身がアクセスできる範囲（サーバー内の全情報）に届きうるため、最大の攻撃面になる。全サブアクション（`get_messages`, `get_user`, `get_channel_info`, `get_guild_info`, `search_messages`）は共通関数`canUserAccessChannel(userID, channelID) bool`を必ず通過すること：

1. **ギルドメンバーシップ確認**：リクエスト送信者が対象チャンネルのギルドに**現在も**所属しているか確認（脱退済みなら拒否）
2. **チャンネル権限確認**：discordgoの`State.UserChannelPermissions`等で、本人に`ViewChannel`権限があるか確認
3. **Bot自身の権限が天井**：Botがそもそも見れないチャンネルは、本人の権限に関係なく無条件拒否
4. **DMの特別扱い**：DMチャンネルは「そのDMの当事者（本人とBotのみ）」以外を対象にできないようハードコードでブロック
5. **キャッシュは短いTTLのみ**：ロール・チャンネル権限は変化するため、長期キャッシュは禁止。呼び出しごとに再評価する
6. **スレッド**：親チャンネルの権限チェックに加え、スレッド自体のメンバーシップも確認（プライベートスレッドは個別注意）

## 14. Cron

- `cron_register`はモデルが呼べるツールとして実装する
- 通知先は**登録時のチャンネル/ユーザーに固定**。モデルが任意の宛先を指定する余地は構造的に無い
- 最短実行間隔をコード側で強制（例: 5分未満は拒否）

## 15. Skill / MCP

- ユーザーごとに登録・管理（`data/config/users/<user_id>/skills.json`）
- リクエストのツールリストを構築する際、**そのリクエストのuser_idが持つSkill/MCPのみ**を読み込む。他人のSkillは見えない・使えない
- これにより「ユーザーAが怪しいMCPサーバーを追加した」場合の被害はユーザーAのworkdir/コンテキスト範囲に閉じる
- 組み込みツール（次節）はSkill/MCPの有無に関係なく全ユーザーが常に使える

## 16. 組み込みツール一覧

| ツール | スコープ | 説明 |
|---|---|---|
| `shell_exec` | サンドボックス, `workdir/<user_id>` | bubblewrap経由、ネットワーク無し |
| `file_read` / `file_write` | `workdir/<user_id>` | ファイル操作 |
| `file_delete` | `workdir/<user_id>` | `.trash/`へ退避のみ、物理削除はしない |
| `web_search` / `web_fetch` | グローバル | レート制限・SSRFガード・キャッシュ・ターン内上限あり |
| `memory_read` / `memory_write` | ユーザーごとMarkdown | 再注入時はデータとして区切る |
| `cron_register` | ユーザーごと | 宛先固定、最短間隔強制 |
| `discord_fetch` | 呼び出し者の実Discord権限でスコープ | get_messages, get_user, get_channel_info, get_guild_info, search_messages |
| `github_fetch` | 固定ドメイン `api.github.com` | `.env`に任意でPAT（read-only推奨） |
| `modrinth_fetch` | 固定ドメイン `api.modrinth.com` | 認証不要 |
| `pi_status` | グローバル | CPU温度（`vcgencmd measure_temp`）、メモリ使用率、uptime。固定コマンドのみ実行、ユーザー入力は渡さない |

vision（画像）入力は専用ツールではなく、マルチモーダル入力としてそのままモデルに渡す形で対応する。

## 17. スラッシュコマンド一覧

| コマンド | 権限 | スコープ | 説明 |
|---|---|---|---|
| `/channel add\|remove` | サーバー管理権限必須 | bot全体 | 常時応答チャンネルの登録/解除 |
| `/model set\|list` | 制限無し | ユーザーごと | 使用するプライマリモデルの切替 |
| `/trash` | 制限無し | ユーザーごと | ゴミ箱の中身確認 |
| `/trashclear` | 制限無し | ユーザーごと | ゴミ箱を完全削除 |
| `/skill install\|list\|remove` | 制限無し | ユーザーごと | Skill/MCPの登録管理 |

権限チェックは常にDiscordネイティブの権限システム（「サーバー管理」権限の有無）で行い、モデルの判断は一切介在しない。一般チャット（メンション/登録チャンネルでの会話）は誰でも無制限に利用できる。

## 18. 複数人共有を見据えた設計

- 全データ（workdir, memory, model設定, skill設定, trash）は**user_id単位で完全分離**
- レート制限・SSRFガード等のグローバル制約は共有リソース（API無料枠、回線帯域）の保護が目的なので全ユーザー共通で適用
- 管理コマンド（`/channel add`等）のみDiscordの「サーバー管理」権限でゲートし、それ以外は誰でも使える設計のまま将来の複数人運用に移行できる

## 19. 検討したが採用しなかった案（経緯メモ）

- **OpenAI互換API対応**：当初追加予定だったが、運用するモデルをGemma4/Gemini Liteの2系統に絞ることでシンプルさを優先し削除
- **SQLite**：CGo無しのpure Go実装で導入可能だったが、「だるそう・重そう」という判断でJSON+Markdownに統一
- **ネットワークのドメイン許可リスト**：運用者が把握しきれない複雑さを生むため、サンドボックス内ネットワークは単純に全遮断する方針に変更
- **危険操作の事前確認フロー**：モデルの判断に依存する仕組みは原理的に信頼できないため、サンドボックス・パス検証等の構造的対策に統一し、確認フロー自体を作らない
- **Discord Modal経由のAPIキー登録＋暗号化保存**：実装コストに対して`.env`管理のシンプルさが勝ると判断し撤回
- **ユーザー単位のスパム/コストクールダウン**：429リアクティブ方式と矛盾しない範囲で省略。無料枠を使い切るリスクは許容
