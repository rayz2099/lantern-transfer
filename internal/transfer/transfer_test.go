package transfer

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func testMux(s *Service) *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/localsend/v2/info", s.handleInfo)
	mux.HandleFunc("POST /api/localsend/v2/prepare-upload", s.handlePrepareUpload)
	mux.HandleFunc("POST /api/localsend/v2/upload", s.handleUpload)
	mux.HandleFunc("POST /api/localsend/v2/cancel", s.handleCancel)
	mux.HandleFunc("POST /api/localsend/v2/prepare-download", s.handlePrepareDownload)
	mux.HandleFunc("GET /api/localsend/v2/download", s.handleDownload)
	return mux
}

func testServer(s *Service) *httptest.Server { return httptest.NewServer(testMux(s)) }

func testTLSServer(s *Service) *httptest.Server {
	server := httptest.NewUnstartedServer(testMux(s))
	server.TLS = &tls.Config{Certificates: []tls.Certificate{s.certificate}}
	server.StartTLS()
	return server
}

func peerFromURL(t *testing.T, raw string) Peer {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	host, portText, err := net.SplitHostPort(u.Host)
	if err != nil {
		t.Fatal(err)
	}
	port, err := net.LookupPort("tcp", portText)
	if err != nil {
		t.Fatal(err)
	}
	return Peer{Alias: "test", Address: host, Port: port, Protocol: "http"}
}

func TestSendPreservesFolderAndExistingFile(t *testing.T) {
	source := t.TempDir()
	root := filepath.Join(source, "folder")
	if err := os.Mkdir(root, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("new content"), 0644); err != nil {
		t.Fatal(err)
	}
	dest := t.TempDir()
	if err := os.Mkdir(filepath.Join(dest, "folder"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dest, "folder", "a.txt"), []byte("old content"), 0644); err != nil {
		t.Fatal(err)
	}
	receiver := New(Config{DownloadDir: dest, OnIncoming: func(context.Context, Incoming) bool { return true }})
	server := testServer(receiver)
	defer server.Close()
	sender := New(Config{})
	if err := sender.Send(context.Background(), peerFromURL(t, server.URL), []string{root}); err != nil {
		t.Fatal(err)
	}
	old, err := os.ReadFile(filepath.Join(dest, "folder", "a.txt"))
	if err != nil || string(old) != "old content" {
		t.Fatalf("existing file changed: %q, %v", old, err)
	}
	newFile, err := os.ReadFile(filepath.Join(dest, "folder", "a (1).txt"))
	if err != nil || string(newFile) != "new content" {
		t.Fatalf("transferred file invalid: %q, %v", newFile, err)
	}
}

func TestPrepareRejectsTraversal(t *testing.T) {
	service := New(Config{DownloadDir: t.TempDir(), OnIncoming: func(context.Context, Incoming) bool { return true }})
	server := testServer(service)
	defer server.Close()
	body, _ := json.Marshal(prepareUpload{Files: map[string]FileMeta{"x": {ID: "x", Name: "../outside.txt", Size: 1}}})
	res, err := http.Post(server.URL+"/api/localsend/v2/prepare-upload", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != 400 {
		t.Fatalf("status=%d", res.StatusCode)
	}
	for _, name := range []string{"../outside", "/absolute", "C:/drive", "..\\outside"} {
		if safeName(name) != "" {
			t.Fatalf("accepted %q", name)
		}
	}
}

func TestDestinationRejectsSymlink(t *testing.T) {
	dir := t.TempDir()
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(dir, "folder")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	if _, err := destination(dir, "folder/file.txt"); err == nil {
		t.Fatal("destination followed symlink")
	}
}

func TestUploadRejectsChecksumMismatch(t *testing.T) {
	dir := t.TempDir()
	service := New(Config{DownloadDir: dir, OnIncoming: func(context.Context, Incoming) bool { return true }})
	server := testServer(service)
	defer server.Close()
	meta := FileMeta{ID: "x", Name: "file.txt", Size: 3, SHA256: strings.Repeat("0", 64)}
	body, _ := json.Marshal(prepareUpload{Files: map[string]FileMeta{"x": meta}})
	res, err := http.Post(server.URL+"/api/localsend/v2/prepare-upload", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	var prepared prepareResult
	if err = json.NewDecoder(res.Body).Decode(&prepared); err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	q := url.Values{"sessionId": {prepared.SessionID}, "fileId": {"x"}, "token": {prepared.Files["x"]}}
	res, err = http.Post(server.URL+"/api/localsend/v2/upload?"+q.Encode(), "application/octet-stream", strings.NewReader("bad"))
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != 422 {
		t.Fatalf("status=%d", res.StatusCode)
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 0 {
		t.Fatalf("unexpected output: %v, %v", entries, err)
	}
}

func TestDownloadLinkRequiresLocalIPAndValidToken(t *testing.T) {
	client := New(Config{DownloadDir: t.TempDir()})
	if err := client.DownloadLink(context.Background(), "http://8.8.8.8/?pin=test", ""); err == nil {
		t.Fatal("public IP accepted")
	}
	content := []byte("shared payload")
	file := filepath.Join(t.TempDir(), "shared.txt")
	if err := os.WriteFile(file, content, 0644); err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256(content)
	serverService := New(Config{OnIncoming: func(context.Context, Incoming) bool { return true }})
	serverService.share = &session{files: map[string]FileMeta{"x": {ID: "x", Name: "shared.txt", Size: int64(len(content)), SHA256: hex.EncodeToString(hash[:])}}, paths: map[string]string{"x": file}, tokens: map[string]string{"share": "secret"}, expires: time.Now().Add(time.Minute)}
	server := testTLSServer(serverService)
	defer server.Close()
	link := server.URL + "/?pin=wrong&fp=" + serverService.self.Fingerprint
	if err := client.DownloadLink(context.Background(), link, ""); err == nil {
		t.Fatal("invalid token accepted")
	}
	link = server.URL + "/?pin=secret&fp=" + serverService.self.Fingerprint
	if err := client.DownloadLink(context.Background(), link, ""); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(client.config.DownloadDir, "shared.txt"))
	if err != nil || !bytes.Equal(got, content) {
		t.Fatalf("download invalid: %q, %v", got, err)
	}
}

func TestHTTPSProbeAndSendPinsCertificate(t *testing.T) {
	source := filepath.Join(t.TempDir(), "a.txt")
	if err := os.WriteFile(source, []byte("secure"), 0644); err != nil {
		t.Fatal(err)
	}
	dest := t.TempDir()
	receiver := New(Config{DownloadDir: dest, OnIncoming: func(context.Context, Incoming) bool { return true }})
	server := testTLSServer(receiver)
	defer server.Close()
	endpoint := peerFromURL(t, server.URL)
	sender := New(Config{})
	peer, err := sender.Probe(context.Background(), endpoint.Address, endpoint.Port)
	if err != nil {
		t.Fatal(err)
	}
	if peer.Fingerprint != receiver.self.Fingerprint {
		t.Fatal("probe fingerprint mismatch")
	}
	peer.Fingerprint = strings.Repeat("0", 64)
	if err = sender.Send(context.Background(), peer, []string{source}); err == nil {
		t.Fatal("wrong fingerprint accepted")
	}
	peer.Fingerprint = receiver.self.Fingerprint
	if err = sender.Send(context.Background(), peer, []string{source}); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(dest, "a.txt"))
	if err != nil || string(got) != "secure" {
		t.Fatalf("received %q: %v", got, err)
	}
}
