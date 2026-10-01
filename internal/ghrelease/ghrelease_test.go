package ghrelease

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseChecksums(t *testing.T) {
	// The shape sha256sum and goreleaser produce, including the "*" binary
	// marker and a path component.
	body := strings.Join([]string{
		"e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855  apptide_1.0.0_Windows_x86_64.exe",
		"5891b5b522d5df086d0ff0b110fbd9d21bb4fc7163af34d08286a2e846f6be03 *apptide_1.0.0_Windows_arm64.exe",
		"0000000000000000000000000000000000000000000000000000000000000000  dist/other.zip",
		"",
	}, "\n")

	tests := map[string]string{
		"apptide_1.0.0_Windows_x86_64.exe": "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
		"apptide_1.0.0_Windows_arm64.exe":  "5891b5b522d5df086d0ff0b110fbd9d21bb4fc7163af34d08286a2e846f6be03",
		"other.zip":                        "0000000000000000000000000000000000000000000000000000000000000000",
	}

	for name, want := range tests {
		got, ok := ParseChecksums(body, name)
		if !ok {
			t.Errorf("no checksum found for %q", name)
			continue
		}
		if got != want {
			t.Errorf("ParseChecksums(%q) = %q, want %q", name, got, want)
		}
	}

	if _, ok := ParseChecksums(body, "missing.exe"); ok {
		t.Error("reported a checksum for a file that is not listed")
	}
}

func TestVerifySHA256(t *testing.T) {
	path := filepath.Join(t.TempDir(), "payload.bin")
	if err := os.WriteFile(path, []byte("hello"), 0o600); err != nil {
		t.Fatal(err)
	}
	// echo -n hello | sha256sum
	const want = "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824"

	if err := VerifySHA256(path, want); err != nil {
		t.Errorf("matching digest rejected: %v", err)
	}
	if err := VerifySHA256(path, "sha256:"+want); err != nil {
		t.Errorf("prefixed digest rejected: %v", err)
	}
	if err := VerifySHA256(path, strings.ToUpper(want)); err != nil {
		t.Errorf("uppercase digest rejected: %v", err)
	}
	if err := VerifySHA256(path, "deadbeef"); err == nil {
		t.Error("a wrong digest was accepted")
	}
	// An empty expectation means "nothing to check against", not "fails".
	if err := VerifySHA256(path, ""); err != nil {
		t.Errorf("empty digest should be a no-op, got %v", err)
	}
}

func TestChecksumAsset(t *testing.T) {
	assets := []Asset{
		{Name: "apptide_Windows_x86_64.exe"},
		{Name: "apptide_checksums.txt"},
	}
	got, ok := ChecksumAsset(assets)
	if !ok || got.Name != "apptide_checksums.txt" {
		t.Errorf("ChecksumAsset = %v, %v", got, ok)
	}

	if _, ok := ChecksumAsset([]Asset{{Name: "app.exe"}}); ok {
		t.Error("reported a checksum asset where there is none")
	}
}

// A truncated download must be rejected rather than installed: the release
// states the size, so a short body is detectable.
func TestDownloadRejectsShortBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("short"))
	}))
	defer srv.Close()

	dest := filepath.Join(t.TempDir(), "out.bin")
	c := NewClient("")
	err := c.Download(context.Background(), Asset{Name: "out.bin", URL: srv.URL, Size: 999}, dest)

	if err == nil {
		t.Fatal("a truncated download was accepted")
	}
	if !strings.Contains(err.Error(), "expected 999") {
		t.Errorf("error does not name the size mismatch: %v", err)
	}
	if _, statErr := os.Stat(dest); statErr == nil {
		t.Error("the partial file was left behind")
	}
}

func TestDownloadAcceptsMatchingSize(t *testing.T) {
	const body = "complete payload"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()

	dest := filepath.Join(t.TempDir(), "out.bin")
	c := NewClient("")
	if err := c.Download(context.Background(), Asset{Name: "out.bin", URL: srv.URL, Size: int64(len(body))}, dest); err != nil {
		t.Fatal(err)
	}

	got, err := os.ReadFile(dest)
	if err != nil || string(got) != body {
		t.Errorf("file = %q, %v", got, err)
	}
}

// A 403 with requests remaining is an access problem, not a rate limit, and
// telling the user to set GITHUB_TOKEN when they already did sends them in
// circles.
func TestForbiddenDistinguishesRateLimitFromAccess(t *testing.T) {
	tests := []struct {
		name      string
		remaining string
		token     string
		wantText  string
	}{
		{"rate limited", "0", "", "rate-limited"},
		{"denied with a token", "42", "tok", "token's scope"},
		{"denied without a token", "42", "", "private"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("X-RateLimit-Remaining", tt.remaining)
				w.WriteHeader(http.StatusForbidden)
			}))
			defer srv.Close()

			c := NewClient(tt.token)
			_, err := c.get(context.Background(), srv.URL, "thing")
			if err == nil {
				t.Fatal("expected an error")
			}
			if !strings.Contains(err.Error(), tt.wantText) {
				t.Errorf("error %q does not mention %q", err, tt.wantText)
			}
		})
	}
}

func TestNotFoundIsDistinct(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	c := NewClient("")
	_, err := c.get(context.Background(), srv.URL, "v1.2.3")

	var notFound *NotFoundError
	if !asNotFoundErr(err, &notFound) {
		t.Fatalf("got %T (%v), want *NotFoundError", err, err)
	}
}

func asNotFoundErr(err error, target **NotFoundError) bool {
	nf, ok := err.(*NotFoundError)
	if ok {
		*target = nf
	}
	return ok
}
