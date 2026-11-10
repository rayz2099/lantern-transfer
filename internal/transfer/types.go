package transfer

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"github.com/rayz2099/lantern-transfer/internal/discovery"
)

type Peer = discovery.Peer

type FileMeta struct {
	ID       string `json:"id"`
	Name     string `json:"fileName"`
	Size     int64  `json:"size"`
	FileType string `json:"fileType"`
	SHA256   string `json:"sha256,omitempty"`
}

type Incoming struct {
	Peer  Peer
	Files []FileMeta
}

type Progress struct {
	Direction string
	Peer      string
	File      string
	Done      int64
	Total     int64
}

type Config struct {
	Alias       string
	DownloadDir string
	Port        int
	OnIncoming  func(context.Context, Incoming) bool
	OnProgress  func(Progress)
	OnComplete  func(Progress, error)
	OnPeers     func([]Peer)
}

type session struct {
	peerIP  string
	files   map[string]FileMeta
	tokens  map[string]string
	paths   map[string]string
	expires time.Time
}

// Service 把发现和直连传输放在一个生命周期中，避免 UI 管理多个监听端口。
type Service struct {
	config    Config
	self      Peer
	discovery *discovery.Manager
	server    *http.Server
	listener  net.Listener
	mu        sync.RWMutex
	sessions  map[string]*session
	share     *session
	started   bool
}

func New(cfg Config) *Service {
	if cfg.Port == 0 {
		cfg.Port = discovery.DefaultPort
	}
	if cfg.Alias == "" {
		host, _ := os.Hostname()
		cfg.Alias = host
	}
	if cfg.DownloadDir == "" {
		home, _ := os.UserHomeDir()
		cfg.DownloadDir = filepath.Join(home, "Downloads")
	}
	host, _ := os.Hostname()
	self := Peer{Alias: cfg.Alias, Version: "2.0", DeviceModel: host, DeviceType: "desktop", Fingerprint: randomID(), Port: cfg.Port, Protocol: "http", Download: true}
	s := &Service{config: cfg, self: self, sessions: make(map[string]*session)}
	s.discovery = discovery.New(self, cfg.OnPeers)
	return s
}

func randomID() string {
	var data [16]byte
	if _, err := rand.Read(data[:]); err != nil {
		panic(err)
	}
	return hex.EncodeToString(data[:])
}

func (s *Service) Peers() []Peer { return s.discovery.Peers() }

func (s *Service) Probe(ctx context.Context, address string, port int) (Peer, error) {
	ip := net.ParseIP(address)
	if ip == nil || (!ip.IsPrivate() && !ip.IsLoopback()) {
		return Peer{}, errors.New("请输入有效的局域网 IP 地址")
	}
	if port <= 0 {
		port = discovery.DefaultPort
	}
	probe, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	url := "http://" + net.JoinHostPort(address, strconv.Itoa(port)) + "/api/localsend/v2/info"
	req, err := http.NewRequestWithContext(probe, http.MethodGet, url, nil)
	if err != nil {
		return Peer{}, err
	}
	res, err := (&http.Client{CheckRedirect: rejectRedirect}).Do(req)
	if err != nil {
		return Peer{}, err
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return Peer{}, errors.New("设备未响应 LocalSend 协议")
	}
	var peer Peer
	if err = json.NewDecoder(io.LimitReader(res.Body, 16<<10)).Decode(&peer); err != nil {
		return Peer{}, err
	}
	if peer.Protocol != "http" {
		return Peer{}, errors.New("设备未启用 HTTP 局域网模式")
	}
	peer.Address = address
	peer.Port = port
	s.discovery.Add(peer)
	return peer, nil
}

func (s *Service) SetDownloadDir(dir string) error {
	info, err := os.Stat(dir)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return errors.New("保存位置不是目录")
	}
	s.mu.Lock()
	s.config.DownloadDir = dir
	s.mu.Unlock()
	return nil
}

func (s *Service) progress(p Progress) {
	if s.config.OnProgress != nil {
		s.config.OnProgress(p)
	}
}

func (s *Service) complete(p Progress, err error) {
	if s.config.OnComplete != nil {
		s.config.OnComplete(p, err)
	}
}
