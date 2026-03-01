# Moticlaw 🫀

OpenClawの「Heartbeat」哲学を完全に再現した、自律型Discord AIエージェントです。
「人間が話しかけたとき」だけでなく、30分ごとに自分で起きて思考し、サーバーの状態を確認して行動します。

## 🌟 特徴

- **Heartbeat機能**: 30分ごとに自ら思考ループを実行し、未読メッセージの確認や自己改善（スキルの生成）を行います。
- **フルシェル権限**: AIが自分自身でシェルコマンド（Git, Pip, ファイル操作など）を実行し、文字通り自律的にシステムを維持・更新します。
- **マルチプロバイダー・ルーティング**: 以下の16種類のプロバイダーをサポート。障害発生時は自動で正常なモデルへ切り替えます。
  - OpenAI, Anthropic, Groq, Gemini
  - NVIDIA NIM, Cerebras, SambaNova, OpenRouter
  - DeepInfra, Mistral (Codestral), Hyperbolic, Scaleway
  - SiliconFlow, Together AI, Hugging Face, Replicate
- **ローカルメモリシステム**: 会話履歴をSQLiteでローカルに保存。プロバイダーを切り替えても会話の文脈（コンテキスト）が維持されます。
- **対話型設定**: 最低限のセットアップ後は、Discord上でAIとチャットしながら詳細な設定（APIキーの追加や同期間隔の変更など）が可能です。

## 🚀 クイックスタート

### 1. リポジトリのクローン
```bash
git clone https://github.com/motchii709/moticlaw.git
cd moticlaw
```

### 2. セットアップ（Onboarding）
まずは以下のコマンドを実行して、最小限の認証情報を設定してください。
```bash
moticlaw onboard
```
※ `moticlaw.bat` が実行されます。Discordトークンと、最低1つのAIプロバイダー（OpenAIなど）のキーが必要です。

### 3. ライブラリのインストール
```bash
pip install -r requirements.txt
```

### 4. 起動
```bash
python main.py
```

## 🛠️ 使いかた

### Discordコマンド
- `/status`: モデルの健康状態とランキングを表示
- `/health`: モデルの死活監視を手動実行
- `/heartbeat_now`: Heartbeatサイクルを今すぐ強制実行
- `/register`: 新しいAPIキーを登録
- `/edit_heartbeat`: AIの「巡回マニュアル（HEARTBEAT.md）」を表示・編集

### AIによる自動設定
Botを起動し、Discord上でメンションして話しかけてください。
「管理チャネルをここに設定して」「Heartbeatの間隔を15分にして」といった依頼にAIが対応します。

## 📁 フォルダ構成
- `main.py`: アプリケーションのエントリーポイント
- `heartbeat_core.py`: 認知ループ（Heartbeat）の核心部
- `model_router.py`: モデルの選択とフォールバック
- `extensions/`: AIが生成した新しいスキルが保存される場所
- `SOUL.md`: AIの性格・アイデンティティ定義
- `HEARTBEAT.md`: 巡回時の行動指示書

## ⚠️ 注意事項
このリポジトリはパブリックです。`.env` や `moticlaw.db` などの機密情報は絶対にコミットしないでください（`.gitignore` で保護されています）。
