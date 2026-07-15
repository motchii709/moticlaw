#!/bin/bash
# firstrun.sh — Headless first-boot setup for Raspberry Pi OS Trixie
# Place this in /boot/ or /root/ and it will self-destruct after execution.
set -e -o pipefail

MARKER="/etc/firstrun.done"
LOG_FILE="/var/log/firstrun.log"
TARGET_USER="moti"
TARGET_PASS="moti"
TARGET_HOSTNAME="moticlaw"
WIFI_SSID="ST-2g"
WIFI_PASS="gingnang931"

# Redirect all subsequent output to log file (append).
exec >> "$LOG_FILE" 2>&1

echo "=== firstrun.sh started: $(date) ==="
echo "Logging to $LOG_FILE"

# ── Idempotency guard ──────────────────────────────────────────────
if [ -f "$MARKER" ]; then
    echo "Marker $MARKER exists — first run already completed. Exiting."
    exit 0
fi

# ── 1. Create user ─────────────────────────────────────────────────
if id "$TARGET_USER" &>/dev/null; then
    echo "User $TARGET_USER already exists, skipping user creation."
else
    echo "Creating user $TARGET_USER with sudo+adm groups…"
    # Generate SHA-512 password hash inline — never stored as plaintext in /etc/shadow.
    PASS_HASH="$(echo "$TARGET_PASS" | openssl passwd -6 -stdin)"
    useradd \
        --create-home \
        --shell /bin/bash \
        --groups sudo,adm \
        --password "$PASS_HASH" \
        "$TARGET_USER"
    echo "User $TARGET_USER created."
fi

# ── 2. Set hostname ────────────────────────────────────────────────
CUR_HOSTNAME="$(hostname)"
if [ "$CUR_HOSTNAME" != "$TARGET_HOSTNAME" ]; then
    echo "Setting hostname to $TARGET_HOSTNAME (was $CUR_HOSTNAME)…"

    # /etc/hostname (persists across reboots)
    echo "$TARGET_HOSTNAME" > /etc/hostname

    # /etc/hosts — replace or append 127.0.1.1 entry
    if grep -q "^127\.0\.1\.1" /etc/hosts; then
        sed -i "s/^127\.0\.1\.1\s\+.*/127.0.1.1 $TARGET_HOSTNAME/" /etc/hosts
    else
        echo "127.0.1.1 $TARGET_HOSTNAME" >> /etc/hosts
    fi

    # Apply immediately (best-effort; if it fails the next boot picks it up)
    if hostname "$TARGET_HOSTNAME" 2>/dev/null; then
        echo "Running hostname changed to $TARGET_HOSTNAME."
    else
        echo "Warning: could not set running hostname (will take effect on next boot)."
    fi
else
    echo "Hostname is already $TARGET_HOSTNAME, skipping."
fi

# ── 3. Connect WiFi ────────────────────────────────────────────────
# Check whether a wireless connection is already active.
if nmcli -t -f TYPE,DEVICE connection show --active 2>/dev/null | grep -q "^802-11-wireless"; then
    echo "WiFi is already connected, skipping."
else
    echo "Connecting to WiFi SSID \"$WIFI_SSID\"…"

    # Wait up to 30 s for NetworkManager to be ready.
    for i in $(seq 1 30); do
        if pgrep -x NetworkManager &>/dev/null; then
            echo "NetworkManager ready after ${i}s."
            break
        fi
        if [ "$i" -eq 30 ]; then
            echo "Warning: NetworkManager not detected after 30 s — proceeding anyway."
        fi
        sleep 1
    done

    # Attempt connection (allow failure — non-fatal on headless first-boot).
    if nmcli dev wifi connect "$WIFI_SSID" password "$WIFI_PASS" --timeout 30; then
        echo "WiFi connected to $WIFI_SSID."
    else
        echo "Warning: WiFi connection failed (non-fatal — check $LOG_FILE)."
    fi
fi

# ── 4. Enable SSH ──────────────────────────────────────────────────
echo "Enabling SSH…"

# Raspberry Pi first-boot marker (checked by raspi-config / sshswitch).
if touch /boot/ssh 2>/dev/null; then
    echo "Created /boot/ssh marker."
else
    echo "Could not create /boot/ssh (non-fatal — systemd service used instead)."
fi

# Explicit sshd config to allow password authentication.
mkdir -p /etc/ssh/sshd_config.d
cat > /etc/ssh/sshd_config.d/01_enable_ssh.conf << 'EOF'
PasswordAuthentication yes
EOF
echo "Written /etc/ssh/sshd_config.d/01_enable_ssh.conf."

# Enable and start the ssh service.
if systemctl is-enabled ssh &>/dev/null; then
    systemctl enable ssh 2>/dev/null || true
fi
if systemctl is-active ssh &>/dev/null; then
    systemctl restart ssh 2>/dev/null || true
else
    systemctl start ssh 2>/dev/null || true
fi
echo "SSH service enabled."

# ── 5. Passwordless sudo ───────────────────────────────────────────
echo "Granting passwordless sudo to $TARGET_USER…"
echo "$TARGET_USER ALL=(ALL) NOPASSWD:ALL" > /etc/sudoers.d/"$TARGET_USER"
chmod 440 /etc/sudoers.d/"$TARGET_USER"
echo "Sudo configured."

# ── 6. Mark complete ───────────────────────────────────────────────
touch "$MARKER"
echo "=== firstrun.sh completed successfully: $(date) ==="

# ── 7. Run moticlaw setup ──────────────────────────────────────────
SETUP_SCRIPT="/home/${TARGET_USER}/moticlaw/deploy/setup.sh"
if [[ -f "$SETUP_SCRIPT" ]]; then
    echo "Running moticlaw setup as user ${TARGET_USER}..."
    su - "$TARGET_USER" -c "bash $SETUP_SCRIPT" || echo "Warning: setup.sh failed (check logs above)"
    echo "moticlaw setup finished."
else
    echo "Warning: $SETUP_SCRIPT not found — skipping moticlaw setup."
fi

# ── 8. Self-delete ─────────────────────────────────────────────────
echo "Self-deleting $0…"
rm -- "$0"