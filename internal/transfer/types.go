package transfer

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"math/big"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
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
	config      Config
	self        Peer
	discovery   *discovery.Manager
	server      *http.Server
	certificate tls.Certificate
	listener    net.Listener
	mu          sync.RWMutex
	sessions    map[string]*session
	share       *session
	started     bool
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
	certificate, fingerprint := newCertificate(host)
	self := Peer{Alias: cfg.Alias, Version: "2.0", DeviceModel: host, DeviceType: "desktop", Fingerprint: fingerprint, Port: cfg.Port, Protocol: "https", Download: true}
	s := &Service{config: cfg, self: self, certificate: certificate, sessions: make(map[string]*session)}
	s.discovery = discovery.New(self, cfg.OnPeers)
	return s
}

// newCertificate 为每次运行生成证书，指纹随发现公告发布供直连校验。
func newCertificate(host string) (tls.Certificate, string) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		panic(err)
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		panic(err)
	}
	template := &x509.Certificate{SerialNumber: serial, Subject: pkix.Name{CommonName: host}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().AddDate(1, 0, 0), KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}, DNSNames: []string{host}}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		panic(err)
	}
	sum := sha256.Sum256(der)
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}, hex.EncodeToString(sum[:])
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
	if net.ParseIP(address) == nil {
		return Peer{}, errors.New("请输入有效的 IP 地址")
	}
	if port <= 0 {
		port = discovery.DefaultPort
	}
	client, fingerprint, err := clientForPeer(Peer{Protocol: "https"})
	if err != nil {
		return Peer{}, err
	}
	probe, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	url := "https://" + net.JoinHostPort(address, strconv.Itoa(port)) + "/api/localsend/v2/info"
	req, err := http.NewRequestWithContext(probe, http.MethodGet, url, nil)
	if err != nil {
		return Peer{}, err
	}
	res, err := client.Do(req)
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
	if !strings.EqualFold(peer.Fingerprint, *fingerprint) {
		return Peer{}, errors.New("设备身份与 HTTPS 证书不一致")
	}
	peer.Address = address
	peer.Port = port
	peer.Protocol = "https"
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
