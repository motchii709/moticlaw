package security

import (
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// SSRFGuard はSSRF攻撃を防ぐガード
type SSRFGuard struct{}

// NewSSRFGuard は新しいSSRFガードを作成する
func NewSSRFGuard() *SSRFGuard {
	return &SSRFGuard{}
}

// IsSafeHost はホストが安全か確認する
func (g *SSRFGuard) IsSafeHost(host string) bool {
	// ホスト名解決
	ips, err := net.LookupIP(host)
	if err != nil {
		return false
	}

	// 解決されたIPがプライベート/ループバック/リンクローカル範囲でないか確認
	for _, ip := range ips {
		if isPrivateIP(ip) {
			return false
		}
	}

	return true
}

// isPrivateIP はIPアドレスがプライベート/ループバック/リンクローカル範囲か確認する
func isPrivateIP(ip net.IP) bool {
	// ループバック
	if ip.IsLoopback() {
		return true
	}

	// リンクローカル
	if ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
		return true
	}

	// プライベートアドレス
	if ip.IsPrivate() {
		return true
	}

	// 明示的な範囲チェック
	if ip4 := ip.To4(); ip4 != nil {
		// 10.0.0.0/8
		if ip4[0] == 10 {
			return true
		}
		// 172.16.0.0/12
		if ip4[0] == 172 && ip4[1] >= 16 && ip4[1] <= 31 {
			return true
		}
		// 192.168.0.0/16
		if ip4[0] == 192 && ip4[1] == 168 {
			return true
		}
		// 169.254.0.0/16 (リンクローカル)
		if ip4[0] == 169 && ip4[1] == 254 {
			return true
		}
	}

	return false
}

// RateLimiter はトークンバケットアルゴリズムによるグローバルなレート制限
type RateLimiter struct {
	tokens   chan struct{}
	closeCh  chan struct{}
	stopOnce sync.Once
}

// NewRateLimiter は新しいレート制限を作成する
func NewRateLimiter(maxPerMinute int) *RateLimiter {
	rl := &RateLimiter{
		tokens:  make(chan struct{}, maxPerMinute),
		closeCh: make(chan struct{}),
	}

	// トークンを初期化
	for i := 0; i < maxPerMinute; i++ {
		rl.tokens <- struct{}{}
	}

	// 定期的にトークンを補充するgoroutine
	// maxPerMinute <= 0 の場合は補充しない（常に拒否）
	if maxPerMinute > 0 {
		interval := time.Minute / time.Duration(maxPerMinute)
		go rl.refillLoop(interval)
	}

	return rl
}

// refillLoop は指定された間隔でトークンを補充する
func (rl *RateLimiter) refillLoop(interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			select {
			case rl.tokens <- struct{}{}:
			default:
				// トークンが満杯の場合はスキップ
			}
		case <-rl.closeCh:
			return
		}
	}
}

// Allow はリクエストを許可するか確認する
func (rl *RateLimiter) Allow() bool {
	select {
	case <-rl.tokens:
		return true
	default:
		return false
	}
}

// Stop はレート制限を停止し、リソースを解放する
func (rl *RateLimiter) Stop() {
	rl.stopOnce.Do(func() {
		close(rl.closeCh)
	})
}

// PathValidator はパスの安全性を検証する
type PathValidator struct {
	allowedBase string
}

// NewPathValidator は新しいパスバリデータを作成する
func NewPathValidator(allowedBase string) *PathValidator {
	return &PathValidator{
		allowedBase: allowedBase,
	}
}

// IsValidPath はパスが許可されたベースパス内にあるか確認する
func (v *PathValidator) IsValidPath(path string) bool {
	if strings.Contains(path, "..") {
		return false
	}
	if strings.HasPrefix(path, "/") {
		return false
	}

	absAllowed, err := filepath.Abs(v.allowedBase)
	if err != nil {
		return false
	}

	cleanPath := filepath.Clean(path)

	fullPath := filepath.Join(absAllowed, cleanPath)

	resolved, err := filepath.EvalSymlinks(fullPath)
	if err != nil {
		resolved, _ = filepath.Abs(fullPath)
	}

	return strings.HasPrefix(resolved, absAllowed+string(os.PathSeparator)) || resolved == absAllowed
}
