package transfer

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type prepareUpload struct {
	Info  Peer                `json:"info"`
	Files map[string]FileMeta `json:"files"`
}

type prepareResult struct {
	SessionID string            `json:"sessionId"`
	Files     map[string]string `json:"files"`
}

type downloadResult struct {
	Info      Peer                `json:"info"`
	SessionID string              `json:"sessionId"`
	Files     map[string]FileMeta `json:"files"`
}

// Start 同时启动 HTTP 和 UDP；任一监听失败都向用户明确报告。
func (s *Service) Start() error {
	s.mu.Lock()
	if s.started {
		s.mu.Unlock()
		return errors.New("服务已启动")
	}
	s.mu.Unlock()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/localsend/v2/info", s.handleInfo)
	mux.HandleFunc("POST /api/localsend/v2/register", s.handleRegister)
	mux.HandleFunc("POST /api/localsend/v2/prepare-upload", s.handlePrepareUpload)
	mux.HandleFunc("POST /api/localsend/v2/upload", s.handleUpload)
	mux.HandleFunc("POST /api/localsend/v2/cancel", s.handleCancel)
	mux.HandleFunc("POST /api/localsend/v2/prepare-download", s.handlePrepareDownload)
	mux.HandleFunc("GET /api/localsend/v2/download", s.handleDownload)
	listener, err := net.Listen("tcp", ":"+strconv.Itoa(s.config.Port))
	if err != nil {
		return err
	}
	s.listener = listener
	s.server = &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	if err = s.discovery.Start(); err != nil {
		listener.Close()
		return err
	}
	s.mu.Lock()
	s.started = true
	s.mu.Unlock()
	tlsListener := tls.NewListener(listener, &tls.Config{Certificates: []tls.Certificate{s.certificate}, MinVersion: tls.VersionTLS13})
	go func() { _ = s.server.Serve(tlsListener) }()
	return nil
}

func (s *Service) Close() error {
	s.mu.Lock()
	if !s.started {
		s.mu.Unlock()
		return nil
	}
	s.started = false
	s.mu.Unlock()
	udpErr := s.discovery.Close()
	httpErr := s.server.Close()
	return errors.Join(udpErr, httpErr)
}

func (s *Service) handleInfo(w http.ResponseWriter, _ *http.Request) { writeJSON(w, s.self) }

func (s *Service) handleRegister(w http.ResponseWriter, r *http.Request) {
	var peer Peer
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10)).Decode(&peer) != nil {
		http.Error(w, "invalid peer", 400)
		return
	}
	peer.Address = remoteIP(r)
	s.discovery.Add(peer)
	writeJSON(w, s.self)
}

func (s *Service) handlePrepareUpload(w http.ResponseWriter, r *http.Request) {
	var req prepareUpload
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req) != nil || len(req.Files) == 0 || len(req.Files) > 1000 {
		http.Error(w, "invalid files", 400)
		return
	}
	files := make([]FileMeta, 0, len(req.Files))
	for id, f := range req.Files {
		if id == "" || f.ID != id || f.Size < 0 || safeName(f.Name) == "" {
			http.Error(w, "invalid file", 400)
			return
		}
		files = append(files, f)
	}
	peer := req.Info
	peer.Address = remoteIP(r)
	approval, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
	defer cancel()
	if s.config.OnIncoming == nil || !s.config.OnIncoming(approval, Incoming{Peer: peer, Files: files}) {
		http.Error(w, "rejected", 403)
		return
	}
	sess := &session{peerIP: peer.Address, files: req.Files, tokens: make(map[string]string), expires: time.Now().Add(time.Hour)}
	for id := range req.Files {
		sess.tokens[id] = randomID()
	}
	sid := randomID()
	s.mu.Lock()
	s.sessions[sid] = sess
	s.mu.Unlock()
	writeJSON(w, prepareResult{SessionID: sid, Files: sess.tokens})
}

func (s *Service) handleUpload(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	sid, id, token := q.Get("sessionId"), q.Get("fileId"), q.Get("token")
	s.mu.RLock()
	sess := s.sessions[sid]
	dir := s.config.DownloadDir
	s.mu.RUnlock()
	if sess == nil || time.Now().After(sess.expires) || sess.peerIP != remoteIP(r) || sess.tokens[id] == "" || sess.tokens[id] != token {
		http.Error(w, "invalid session", 403)
		return
	}
	meta := sess.files[id]
	name := safeName(meta.Name)
	if name == "" {
		http.Error(w, "invalid file name", 400)
		return
	}
	full, err := destination(dir, name)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	tmp, err := os.CreateTemp(filepath.Dir(full), ".lantern-*")
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	defer os.Remove(tmp.Name())
	defer tmp.Close()
	hash := sha256.New()
	limit := io.LimitReader(r.Body, meta.Size+1)
	count, err := io.Copy(io.MultiWriter(tmp, hash), limit)
	if err != nil || count != meta.Size {
		http.Error(w, "size mismatch", 422)
		return
	}
	if meta.SHA256 != "" && !strings.EqualFold(meta.SHA256, hex.EncodeToString(hash.Sum(nil))) {
		http.Error(w, "checksum mismatch", 422)
		return
	}
	if err = tmp.Close(); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	final, err := reservePath(full)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	if err = os.Rename(tmp.Name(), final); err != nil {
		os.Remove(final)
		http.Error(w, err.Error(), 500)
		return
	}
	p := Progress{Direction: "receive", Peer: sess.peerIP, File: name, Done: count, Total: meta.Size}
	s.progress(p)
	s.complete(p, nil)
	w.WriteHeader(http.StatusOK)
}

func (s *Service) handleCancel(w http.ResponseWriter, r *http.Request) {
	sid := r.URL.Query().Get("sessionId")
	s.mu.Lock()
	delete(s.sessions, sid)
	s.mu.Unlock()
	w.WriteHeader(http.StatusOK)
}

func (s *Service) handlePrepareDownload(w http.ResponseWriter, r *http.Request) {
	if r.URL.Query().Get("pin") == "" {
		http.Error(w, "missing token", 401)
		return
	}
	s.mu.RLock()
	share := s.share
	s.mu.RUnlock()
	if share == nil || time.Now().After(share.expires) || r.URL.Query().Get("pin") != share.tokens["share"] {
		http.Error(w, "expired link", 403)
		return
	}
	files := make([]FileMeta, 0, len(share.files))
	for _, f := range share.files {
		files = append(files, f)
	}
	peer := Peer{Alias: "下载链接", Address: remoteIP(r)}
	approval, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
	defer cancel()
	if s.config.OnIncoming == nil || !s.config.OnIncoming(approval, Incoming{Peer: peer, Files: files}) {
		http.Error(w, "rejected", 403)
		return
	}
	sid := randomID()
	sess := &session{peerIP: peer.Address, files: share.files, paths: share.paths, expires: time.Now().Add(10 * time.Minute)}
	s.mu.Lock()
	s.sessions[sid] = sess
	s.mu.Unlock()
	writeJSON(w, downloadResult{Info: s.self, SessionID: sid, Files: sess.files})
}

func (s *Service) handleDownload(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	s.mu.RLock()
	sess := s.sessions[q.Get("sessionId")]
	s.mu.RUnlock()
	if sess == nil || sess.paths == nil || sess.peerIP != remoteIP(r) || time.Now().After(sess.expires) {
		http.Error(w, "invalid session", 403)
		return
	}
	id := q.Get("fileId")
	path := sess.paths[id]
	if path == "" {
		http.Error(w, "unknown file", 404)
		return
	}
	f, err := os.Open(path)
	if err != nil {
		http.Error(w, "file unavailable", 404)
		return
	}
	defer f.Close()
	meta := sess.files[id]
	w.Header().Set("Content-Length", strconv.FormatInt(meta.Size, 10))
	w.Header().Set("Content-Type", "application/octet-stream")
	_, _ = io.Copy(w, f)
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func remoteIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// safeName 阻断绝对路径和上级目录，接收目录只能由本机设置决定。
func safeName(name string) string {
	name = strings.ReplaceAll(name, "\\", "/")
	if strings.HasPrefix(name, "/") || strings.Contains(name, ":") {
		return ""
	}
	clean := filepath.ToSlash(filepath.Clean(filepath.FromSlash(name)))
	if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") {
		return ""
	}
	return clean
}

func reservePath(path string) (string, error) {
	ext := filepath.Ext(path)
	stem := strings.TrimSuffix(path, ext)
	for i := 0; i < 10000; i++ {
		candidate := path
		if i > 0 {
			candidate = fmt.Sprintf("%s (%d)%s", stem, i, ext)
		}
		f, err := os.OpenFile(candidate, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0644)
		if err == nil {
			f.Close()
			return candidate, nil
		}
		if !os.IsExist(err) {
			return "", err
		}
	}
	return "", errors.New("too many files with same name")
}

// destination 拒绝路径中的符号链接，避免远端文件名借本地链接越出保存目录。
func destination(dir, name string) (string, error) {
	clean := safeName(name)
	if clean == "" {
		return "", errors.New("invalid file name")
	}
	parts := strings.Split(filepath.ToSlash(filepath.Dir(clean)), "/")
	current := dir
	for _, part := range parts {
		if part == "." || part == "" {
			continue
		}
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if os.IsNotExist(err) {
			if err = os.Mkdir(current, 0755); err != nil {
				return "", err
			}
			continue
		}
		if err != nil {
			return "", err
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return "", errors.New("保存路径包含符号链接或非目录")
		}
	}
	return filepath.Join(dir, filepath.FromSlash(clean)), nil
}
