package apkpack

import "testing"

func TestNormalizeLibPath(t *testing.T) {
	tests := []struct {
		name string
		lib  string
		abi  string
		want string
	}{
		{"basename", "libdemo.so", "arm64-v8a", "lib/arm64-v8a/libdemo.so"},
		{"abi-relative", "arm64-v8a/libdemo.so", "arm64-v8a", "lib/arm64-v8a/libdemo.so"},
		{"apk-relative", "lib/arm64-v8a/libdemo.so", "arm64-v8a", "lib/arm64-v8a/libdemo.so"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := normalizeLibPath(tt.lib, tt.abi); got != tt.want {
				t.Fatalf("normalizeLibPath(%q, %q)=%q want %q", tt.lib, tt.abi, got, tt.want)
			}
		})
	}
}

func TestIsStaleAPKSignatureEntry(t *testing.T) {
	stale := []string{
		"META-INF/MANIFEST.MF",
		"META-INF/CERT.SF",
		"META-INF/CERT.RSA",
		"META-INF/CERT.DSA",
		"META-INF/CERT.EC",
	}
	for _, name := range stale {
		if !isStaleAPKSignatureEntry(name) {
			t.Fatalf("%s should be treated as stale signature metadata", name)
		}
	}
	if isStaleAPKSignatureEntry("META-INF/services/example.Service") {
		t.Fatal("non-signature META-INF service entry should be preserved")
	}
}
