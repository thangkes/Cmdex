package update

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNewer(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{"0.1.0", "v0.2.0", true},
		{"0.1.0", "0.1.0", false},
		{"0.2.0", "0.1.9", false},
		{"0.9.0", "0.10.0", true},
		{"dev", "1.0.0", false},
		{"0.1.0", "v0.1.1-beta", true},
		{"0.2.0-beta.1", "0.2.0", true}, // the final release is above its betas
		{"0.2.0", "0.2.0-beta.9", false},
		{"0.2.0-beta.1", "0.2.0-beta.2", true},
		{"0.2.0-beta.2", "0.2.0-beta.10", true},
		{"0.2.0-alpha", "0.2.0-beta", true},
		{"0.2.0-beta", "0.2.0-beta.1", true},
		{"0.1.0", "windows-server-v0.2.0-beta.1", true},
		{"0.2.0-beta.1", "windows-server-v0.2.0", true},
	}
	for _, c := range cases {
		if got := Newer(c.a, c.b); got != c.want {
			t.Errorf("Newer(%q,%q)=%v", c.a, c.b, got)
		}
	}
}

// rewrite sends api.github.com and download requests to a test server.
type rewrite struct{ base string }

func (r rewrite) RoundTrip(req *http.Request) (*http.Response, error) {
	u := *req.URL
	u.Scheme, u.Host = "http", strings.TrimPrefix(r.base, "http://")
	req2 := req.Clone(req.Context())
	req2.URL = &u
	return http.DefaultTransport.RoundTrip(req2)
}

// testKey makes a signing key, trusted (instead of the real keys) for the
// test, and returns the base64 signature of b by it.
func testKey(t *testing.T) func(b []byte) string {
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	old := TrustedKeys
	TrustedKeys = []string{base64.StdEncoding.EncodeToString(pub)}
	t.Cleanup(func() { TrustedKeys = old })
	return func(b []byte) string { return base64.StdEncoding.EncodeToString(ed25519.Sign(priv, b)) }
}

func TestCheckAndDownload(t *testing.T) {
	sign := testKey(t)
	payload := []byte("new setup bytes")
	sum := sha256.Sum256(payload)
	sums := fmt.Sprintf("%s  Cmdex-Windows-Setup-0.2.0.exe\n%s  Cmdex-Linux-amd64-0.2.0.tar.gz\n", hex.EncodeToString(sum[:]), strings.Repeat("0", 64))
	sig := sign([]byte(sums))
	var srvURL string
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/"+Repo+"/releases", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `[
 {"tag_name":"v0.3.0","draft":true,"assets":[]},
 {"tag_name":"v0.2.0","html_url":"page","body":"notes","assets":[
   {"name":"Cmdex-Windows-Setup-0.2.0.exe","browser_download_url":"%[1]s/setup","size":15},
   {"name":"Cmdex-Linux-amd64-0.2.0.tar.gz","browser_download_url":"%[1]s/tgz","size":1},
   {"name":"SHA256SUMS.txt","browser_download_url":"%[1]s/sums","size":1},
   {"name":"SHA256SUMS.txt.sig","browser_download_url":"%[1]s/sig","size":1}]},
 {"tag_name":"v0.1.5","assets":[{"name":"Cmdex-Windows-Setup-0.1.5.exe","browser_download_url":"x"}]}
]`, srvURL)
	})
	mux.HandleFunc("/setup", func(w http.ResponseWriter, r *http.Request) { w.Write(payload) })
	mux.HandleFunc("/sums", func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, sums) })
	mux.HandleFunc("/sig", func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, sig) })
	srv := httptest.NewServer(mux)
	defer srv.Close()
	srvURL = srv.URL
	hc := &http.Client{Transport: rewrite{srv.URL}}
	ctx := context.Background()
	isSetup := func(n string) bool {
		return strings.HasPrefix(n, "Cmdex-Windows-Setup-") && strings.HasSuffix(n, ".exe")
	}

	r, err := Check(ctx, hc, "0.1.0", isSetup)
	if err != nil || r == nil || r.Version != "0.2.0" || r.AssetName != "Cmdex-Windows-Setup-0.2.0.exe" {
		t.Fatalf("Check: %+v %v", r, err)
	}
	if r2, _ := Check(ctx, hc, "0.2.0", isSetup); r2 != nil {
		t.Fatalf("up to date but got %+v", r2)
	}
	if r2, _ := Check(ctx, hc, "dev", isSetup); r2 != nil {
		t.Fatal("dev build should not update")
	}

	dest := filepath.Join(t.TempDir(), "setup.exe")
	if err := Download(ctx, hc, r, dest); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(dest); string(b) != string(payload) {
		t.Fatal("wrong content")
	}

	// The Linux archive's published sum does not match what the server returns -> rejected, no file left.
	tgz, _ := Check(ctx, hc, "0.1.0", func(n string) bool { return strings.HasSuffix(n, ".tar.gz") })
	tgz.AssetURL = srv.URL + "/setup"
	bad := filepath.Join(t.TempDir(), "x.tar.gz")
	if err := Download(ctx, hc, tgz, bad); err == nil {
		t.Fatal("mismatching checksum accepted")
	}
	if _, err := os.Stat(bad); !os.IsNotExist(err) {
		t.Fatal("bad download left on disk")
	}

	// checksums changed after signing (or signed by another key): refused
	sums += "0000000000000000000000000000000000000000000000000000000000000000  extra\n"
	if err := Download(ctx, hc, r, filepath.Join(t.TempDir(), "s.exe")); !errors.Is(err, ErrUnsigned) {
		t.Fatalf("unsigned checksums accepted: %v", err)
	}
	sums = strings.SplitAfterN(sums, "extra\n", 2)[0]
	TrustedKeys = []string{base64.StdEncoding.EncodeToString(make([]byte, 32))}
	if err := Download(ctx, hc, r, filepath.Join(t.TempDir(), "s.exe")); !errors.Is(err, ErrUnsigned) {
		t.Fatalf("signature by an unknown key accepted: %v", err)
	}
}

// A release without a signature is not offered at all.
func TestUnsignedReleaseNotOffered(t *testing.T) {
	testKey(t)
	var srvURL string
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/"+Repo+"/releases", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `[{"tag_name":"v0.9.0","assets":[
   {"name":"App-0.9.0.exe","browser_download_url":"%[1]s/f"},{"name":"SHA256SUMS.txt","browser_download_url":"%[1]s/s"}]}]`, srvURL)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	srvURL = srv.URL
	if r, _ := Check(context.Background(), &http.Client{Transport: rewrite{srv.URL}}, "0.1.0", func(string) bool { return true }); r != nil {
		t.Fatalf("unsigned release offered: %+v", r)
	}
}

// Stable builds are not offered betas; beta builds get newer betas and the
// final release.
func TestCheckBetas(t *testing.T) {
	var srvURL string
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/"+Repo+"/releases", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `[
 {"tag_name":"v0.3.0-beta.1","prerelease":true,"assets":[
   {"name":"App-0.3.0-beta.1.exe","browser_download_url":"%[1]s/b"},{"name":"SHA256SUMS.txt","browser_download_url":"%[1]s/s"},{"name":"SHA256SUMS.txt.sig","browser_download_url":"%[1]s/g"}]},
 {"tag_name":"v0.2.0","assets":[
   {"name":"App-0.2.0.exe","browser_download_url":"%[1]s/f"},{"name":"SHA256SUMS.txt","browser_download_url":"%[1]s/s"},{"name":"SHA256SUMS.txt.sig","browser_download_url":"%[1]s/g"}]}
]`, srvURL)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	srvURL = srv.URL
	hc := &http.Client{Transport: rewrite{srv.URL}}
	any := func(string) bool { return true }
	ctx := context.Background()

	if r, _ := Check(ctx, hc, "0.1.0", any); r == nil || r.Version != "0.2.0" {
		t.Fatalf("stable build: want 0.2.0, got %+v", r)
	}
	if r, _ := Check(ctx, hc, "0.2.0", any); r != nil {
		t.Fatalf("stable build offered a beta: %+v", r)
	}
	if r, _ := Check(ctx, hc, "0.2.0-beta.3", any); r == nil || r.Version != "0.3.0-beta.1" {
		t.Fatalf("beta build: want 0.3.0-beta.1, got %+v", r)
	}
}
