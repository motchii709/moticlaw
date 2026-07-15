package security

import (
	"net"
	"testing"
)

func TestIsPrivateIP(t *testing.T) {
	tests := []struct {
		name     string
		ip       string
		expected bool
	}{
		// ループバック
		{"loopback v4", "127.0.0.1", true},
		{"loopback v4 other", "127.0.0.2", true},
		{"loopback v6", "::1", true},

		// 10/8
		{"10/8 first", "10.0.0.1", true},
		{"10/8 random", "10.255.255.255", true},
		{"10/8 middle", "10.123.45.67", true},

		// 172.16/12
		{"172.16/12 min", "172.16.0.1", true},
		{"172.16/12 max", "172.31.255.255", true},
		{"172.16/12 middle", "172.20.0.1", true},

		// 192.168/16
		{"192.168/16 first", "192.168.1.1", true},
		{"192.168/16 range", "192.168.255.255", true},
		{"192.168/16 middle", "192.168.100.200", true},

		// リンクローカル (169.254/16)
		{"link-local", "169.254.1.1", true},
		{"link-local edge", "169.254.255.255", true},

		// 公開IP
		{"public DNS", "8.8.8.8", false},
		{"public Cloudflare", "1.1.1.1", false},
		{"public google", "216.58.200.14", false},

		// 境界ケース：172.15はプライベート範囲外（/12の下限未満）
		{"172.15 is not private", "172.15.0.1", false},
		// 172.32はプライベート範囲外（/12の上限超過）
		{"172.32 is not private", "172.32.0.1", false},

		// その他プライベート範囲外
		{"192.169 is not private", "192.169.1.1", false},
		{"11.0.0.1 is not private", "11.0.0.1", false},

		// ブロードキャスト/その他
		{"broadcast", "255.255.255.255", false},
		{"zero", "0.0.0.0", false},

		// IPv6 private (ULA)
		{"IPv6 local unicast", "fc00::1", true},
		{"IPv6 unique local", "fd00::1", true},

		// IPv6 link-local
		{"IPv6 link-local", "fe80::1", true},

		// IPv6 public
		{"IPv6 public Google DNS", "2001:4860:4860::8888", false},
		{"IPv6 public Cloudflare", "2606:4700:4700::1111", false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ip := net.ParseIP(tc.ip)
			if ip == nil {
				t.Fatalf("failed to parse IP: %s", tc.ip)
			}
			got := isPrivateIP(ip)
			if got != tc.expected {
				t.Errorf("isPrivateIP(%q) = %v; want %v", tc.ip, got, tc.expected)
			}
		})
	}
}

func TestPathValidatorIsValidPath(t *testing.T) {
	pv := NewPathValidator("/home/user/workdir")

	tests := []struct {
		name     string
		path     string
		expected bool
	}{

		// パストラバーサル攻撃：拒否されるべき
		{"parent dir", "../etc/passwd", false},
		{"double parent", "../../etc/passwd", false},
		{"nested traversal", "foo/../../bar", false},
		{"deep traversal", "a/b/c/../../../../etc/passwd", false},

		// 絶対パス：拒否されるべき
		{"absolute root", "/etc/passwd", false},
		{"absolute no traversal", "/safe/path", false},
		{"absolute with dot", "/./file", false},

		// .. を含むがパストラバーサルでないケース（依然として拒否）
		{"dots in filename", "some..file", false},
		{"leading dots", "..hidden", false},
		{"trailing dots", "file..", false},

		// 安全なパス：許可されるべき
		{"simple relative", "safe/path", true},
		{"just a file", "file.txt", true},
		{"deep relative", "a/b/c/d/e/f.txt", true},
		{"current dir prefix", "./file.txt", true},
		{"single dir", "subdir", true},
		{"hidden file", ".hidden", true},
		{"number in name", "file123.txt", true},
		{"with hyphen", "my-file.txt", true},
		{"with underscore", "my_file.txt", true},
		{"subdirectory with dot", "dir/file.txt", true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := pv.IsValidPath(tc.path)
			if got != tc.expected {
				t.Errorf("IsValidPath(%q) = %v; want %v", tc.path, got, tc.expected)
			}
		})
	}
}

func TestSSRFGuardIsSafeHost_PrivateIP(t *testing.T) {
	// SSRFGuardがプライベートIPを解決するホスト名を拒否することを検証
	// 注意：これらは外部DNSに依存するため、解決が期待通りに動作する場合にのみテストする
	// ローカルテスト向けのため、スキップはせず実行する

	g := NewSSRFGuard()

	tests := []struct {
		name     string
		host     string
		expected bool
	}{
		{"localhost", "localhost", false},
		{"loopback IP", "127.0.0.1", false},
		{"private 10.x", "10.0.0.1", false},
		{"private 192.168", "192.168.1.1", false},
		{"public host", "google.com", true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := g.IsSafeHost(tc.host)
			if got != tc.expected {
				t.Errorf("IsSafeHost(%q) = %v; want %v", tc.host, got, tc.expected)
			}
		})
	}
}

func TestNewPathValidator(t *testing.T) {
	pv := NewPathValidator("/home/user")
	if pv == nil {
		t.Fatal("NewPathValidator returned nil")
	}
	// allowedBase が正しく設定されているか（間接的に IsValidPath の振る舞いで確認）
	if pv.allowedBase != "/home/user" {
		t.Errorf("allowedBase = %q; want %q", pv.allowedBase, "/home/user")
	}
}

func TestNewSSRFGuard(t *testing.T) {
	g := NewSSRFGuard()
	if g == nil {
		t.Fatal("NewSSRFGuard returned nil")
	}
}

func TestRateLimiter_Allow(t *testing.T) {
	rl := NewRateLimiter(5)
	if rl == nil {
		t.Fatal("NewRateLimiter returned nil")
	}

	// 最初の5回は許可される
	for i := 0; i < 5; i++ {
		if !rl.Allow() {
			t.Errorf("Allow() returned false on iteration %d; want true", i)
		}
	}

	// 6回目以降は拒否される（トークンが補充されるまで）
	if rl.Allow() {
		t.Error("Allow() returned true after consuming all tokens; want false")
	}
}

func TestRateLimiter_Allow_Zero(t *testing.T) {
	// 容量0のレートリミッター
	rl := NewRateLimiter(0)
	if rl == nil {
		t.Fatal("NewRateLimiter returned nil")
	}

	// 常に拒否
	if rl.Allow() {
		t.Error("Allow() returned true for zero-capacity limiter; want false")
	}
}
