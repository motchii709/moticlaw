#!/usr/bin/env bash
# moticlaw セットアップスクリプト
# firstrun.sh の末尾から呼び出されることを想定:
#   su - moti -c '/home/moti/moticlaw/deploy/setup.sh'
#
# 前提条件:
#   - ユーザー moti が存在する
#   - 以下のファイルが /home/moti/moticlaw/ 以下にデプロイ済み:
#       bin/moticlaw  (クロスコンパイル済みバイナリ)
#       .env          (DISCORD_BOT_TOKEN, GEMINI_API_KEY 設定済み)
#       deploy/setup.sh (このスクリプト自身)
#
# このスクリプトが行うこと:
#   1. データディレクトリ構造の作成
#   2. バイナリの /usr/local/bin へのインストール
#   3. .env の確認
#   4. systemd ユーザーサービスのセットアップ
#   5. Lingering の有効化
#   6. パーミッションの設定
#   7. サービスの起動

set -euo pipefail

# --------------------------------------------------
# ユーティリティ関数
# --------------------------------------------------

GREEN='\033[0;32m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
NC='\033[0m' # No Color

info()  { echo -e "${GREEN}[INFO]${NC}  $*"; }
warn()  { echo -e "${YELLOW}[WARN]${NC}  $*"; }
error() { echo -e "${RED}[ERROR]${NC} $*" >&2; }

# エラーハンドラ：どのコマンドで失敗したかを表示
trap 'error "セットアップが中断されました（最終終了コード: $?）"' ERR

# --------------------------------------------------
# パス設定（すべて絶対パス）
# --------------------------------------------------

TARGET_DIR="${HOME}/moticlaw"
BINARY_SRC="${TARGET_DIR}/bin/moticlaw"
BINARY_DST="/usr/local/bin/moticlaw"
SYSTEMD_USER_DIR="${HOME}/.config/systemd/user"
SERVICE_FILE="${SYSTEMD_USER_DIR}/moticlaw.service"

export DEBIAN_FRONTEND=noninteractive

# --------------------------------------------------
# フェーズ 0: 前提条件チェック
# --------------------------------------------------

info "=== フェーズ 0: 前提条件チェック ==="

if [[ "${EUID}" -eq 0 ]]; then
    error "このスクリプトは root ではなく moti ユーザーで実行してください。"
    exit 1
fi

if [[ "$(id -un)" != "moti" ]]; then
    warn "ユーザー 'moti' 以外で実行しています（ユーザー: $(id -un)）。"
    warn "想定外の動作をする可能性があります。moti ユーザーで実行することを推奨します。"
fi

if [[ ! -f "${BINARY_SRC}" ]]; then
    error "moticlaw バイナリが見つかりません: ${BINARY_SRC}"
    error "事前にクロスコンパイルして ${TARGET_DIR}/bin/ に配置してください:"
    error "  GOOS=linux GOARCH=arm64 go build -o bin/moticlaw ./cmd/moticlaw"
    exit 1
fi

info "ターゲットディレクトリ: ${TARGET_DIR}"
info "バイナリ: ${BINARY_SRC}"
info ""

# --------------------------------------------------
# フェーズ 1: ディレクトリ構造の作成
# --------------------------------------------------

info "=== フェーズ 1: ディレクトリ構造の作成 ==="

mkdir -p "${TARGET_DIR}/data/config"
mkdir -p "${TARGET_DIR}/data/workdir/moti"
mkdir -p "${TARGET_DIR}/data/memory"
mkdir -p "${TARGET_DIR}/data/logs"

info "ディレクトリ構造を作成しました: ${TARGET_DIR}/data/{config,workdir/moti,memory,logs}"
info ""

# --------------------------------------------------
# フェーズ 2: moticlaw バイナリのインストール
# --------------------------------------------------

info "=== フェーズ 2: moticlaw バイナリのインストール ==="

info "バイナリを ${BINARY_DST} にコピー中（sudo が必要です）..."
sudo cp "${BINARY_SRC}" "${BINARY_DST}"
sudo chmod 755 "${BINARY_DST}"
info "バイナリをインストールしました: ${BINARY_DST}"
info ""

# --------------------------------------------------
# フェーズ 3: .env の確認
# --------------------------------------------------

info "=== フェーズ 3: .env の確認 ==="

if [[ -f "${TARGET_DIR}/.env" ]]; then
    chmod 600 "${TARGET_DIR}/.env"
    info ".env を確認しました: ${TARGET_DIR}/.env（パーミッション: 600）"
else
    error "${TARGET_DIR}/.env が見つかりません。"
    error "DISCORD_BOT_TOKEN と GEMINI_API_KEY を設定した .env ファイルを"
    error "${TARGET_DIR}/.env に配置してください。"
    error "設定が完了するまでボットは起動しません。"
fi
info ""

# --------------------------------------------------
# フェーズ 4: systemd ユーザーサービスのセットアップ
# --------------------------------------------------

info "=== フェーズ 4: systemd ユーザーサービスのセットアップ ==="

mkdir -p "${SYSTEMD_USER_DIR}"

if [[ ! -f "${SERVICE_FILE}" ]]; then
    cat > "${SERVICE_FILE}" << SERVICE_EOF
[Unit]
Description=moticlaw - Personal AI Agent
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
ExecStart=/usr/local/bin/moticlaw
WorkingDirectory=${TARGET_DIR}
Restart=on-failure
RestartSec=5
StandardOutput=journal
StandardError=journal
EnvironmentFile=${TARGET_DIR}/.env
# OOM 対策（Raspberry Pi Zero 2W 用）
MemoryHigh=400M
MemoryMax=450M
CPUAccounting=yes
MemoryAccounting=yes

[Install]
WantedBy=default.target
SERVICE_EOF
    chmod 644 "${SERVICE_FILE}"
    info "systemd ユーザーサービスを作成しました: ${SERVICE_FILE}"
else
    info "systemd ユーザーサービスは既に存在します: ${SERVICE_FILE}"
fi
info ""

# --------------------------------------------------
# フェーズ 5: Lingering の有効化
# --------------------------------------------------

info "=== フェーズ 5: Lingering の有効化 ==="

info "moti ユーザーの lingering を有効化中（sudo が必要です）..."
sudo loginctl enable-linger moti
info "linger を有効化しました（ログインなしでもユーザーサービスが動作します）"
info ""

# --------------------------------------------------
# フェーズ 6: パーミッションの設定
# --------------------------------------------------

info "=== フェーズ 6: パーミッションの設定 ==="

sudo chown -R moti:moti "${TARGET_DIR}"
info "${TARGET_DIR} 以下の所有者を moti:moti に設定しました"
info ""

# --------------------------------------------------
# フェーズ 7: サービスの起動
# --------------------------------------------------

info "=== フェーズ 7: サービスの起動 ==="

systemctl --user daemon-reload

if systemctl --user is-enabled moticlaw &>/dev/null; then
    info "moticlaw サービスは既に有効化されています"
else
    systemctl --user enable moticlaw
    info "moticlaw サービスを有効化しました（自動起動）"
fi

if systemctl --user is-active moticlaw &>/dev/null; then
    info "moticlaw サービスは既に稼働中です"
    systemctl --user restart moticlaw
    info "moticlaw サービスを再起動しました"
else
    systemctl --user start moticlaw
    info "moticlaw サービスを起動しました"
fi
info ""

# --------------------------------------------------
# 完了
# --------------------------------------------------

info "=== セットアップ完了 ==="
info ""
info "サービス状態の確認:"
info "  systemctl --user status moticlaw"
info ""
info "ログの確認:"
info "  journalctl --user -u moticlaw -f"
info ""
info "バイナリ: ${BINARY_DST}"
info "作業ディレクトリ: ${TARGET_DIR}"
info "データディレクトリ: ${TARGET_DIR}/data"
info ""
info "注意: .env に正しいトークンが設定されていることを確認してください。"
info "設定が完了していない場合は ${TARGET_DIR}/.env を編集してください。"