package discovery

import (
	"encoding/json"
	"net"
	"sort"
	"strconv"
	"sync"
	"time"
)

const DefaultPort = 53317

var group = net.IPv4(224, 0, 0, 167)

// Peer 保留网络地址和可读设备名，避免同名设备被误认为同一台机器。
type Peer struct {
	Alias       string    `json:"alias"`
	Hostname    string    `json:"-"`
	Address     string    `json:"-"`
	Port        int       `json:"port"`
	Version     string    `json:"version"`
	DeviceModel string    `json:"deviceModel,omitempty"`
	DeviceType  string    `json:"deviceType,omitempty"`
	Fingerprint string    `json:"fingerprint"`
	Protocol    string    `json:"protocol"`
	Download    bool      `json:"download"`
	Announce    bool      `json:"announce,omitempty"`
	LastSeen    time.Time `json:"-"`
}

// Manager 只维护发现状态，传输请求由上层决定是否接受。
type Manager struct {
	self     Peer
	onChange func([]Peer)
	mu       sync.RWMutex
	peers    map[string]Peer
	conn     *net.UDPConn
	stop     chan struct{}
	wg       sync.WaitGroup
}

func New(self Peer, onChange func([]Peer)) *Manager {
	return &Manager{self: self, onChange: onChange, peers: make(map[string]Peer), stop: make(chan struct{})}
}

// Start 使用 LocalSend 的默认组播地址，使同网段客户端能直接发现彼此。
func (m *Manager) Start() error {
	addr := &net.UDPAddr{IP: group, Port: DefaultPort}
	conn, err := net.ListenMulticastUDP("udp4", nil, addr)
	if err != nil {
		return err
	}
	if err = conn.SetReadBuffer(64 << 10); err != nil {
		conn.Close()
		return err
	}
	m.conn = conn
	m.wg.Add(2)
	go m.readLoop()
	go m.announceLoop()
	return nil
}

func (m *Manager) Close() error {
	close(m.stop)
	err := m.conn.Close()
	m.wg.Wait()
	return err
}

func (m *Manager) Peers() []Peer {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]Peer, 0, len(m.peers))
	for _, peer := range m.peers {
		if time.Since(peer.LastSeen) < 35*time.Second {
			out = append(out, peer)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Alias < out[j].Alias })
	return out
}

func (m *Manager) Add(peer Peer) {
	if peer.Fingerprint == m.self.Fingerprint || peer.Address == "" {
		return
	}
	if peer.Port == 0 {
		peer.Port = DefaultPort
	}
	peer.LastSeen = time.Now()
	peer.Hostname = peer.DeviceModel
	if peer.Hostname == "" {
		peer.Hostname = peer.Address
	}
	key := peer.Fingerprint
	if key == "" {
		key = net.JoinHostPort(peer.Address, stringPort(peer.Port))
	}
	m.mu.Lock()
	old, exists := m.peers[key]
	m.peers[key] = peer
	m.mu.Unlock()
	if m.onChange != nil && (!exists || old.Alias != peer.Alias || old.Address != peer.Address) {
		m.onChange(m.Peers())
	}
}

func stringPort(port int) string { return strconv.Itoa(port) }

func (m *Manager) readLoop() {
	defer m.wg.Done()
	buf := make([]byte, 4096)
	for {
		n, src, err := m.conn.ReadFromUDP(buf)
		if err != nil {
			return
		}
		var peer Peer
		if json.Unmarshal(buf[:n], &peer) != nil || peer.Version == "" {
			continue
		}
		peer.Address = src.IP.String()
		m.Add(peer)
		if peer.Announce {
			m.reply(src)
		}
	}
}

func (m *Manager) announceLoop() {
	defer m.wg.Done()
	m.announce(true)
	tick := time.NewTicker(10 * time.Second)
	defer tick.Stop()
	for {
		select {
		case <-m.stop:
			return
		case <-tick.C:
			m.prune()
			m.announce(true)
		}
	}
}

func (m *Manager) prune() {
	m.mu.Lock()
	changed := false
	for key, peer := range m.peers {
		if time.Since(peer.LastSeen) >= 35*time.Second {
			delete(m.peers, key)
			changed = true
		}
	}
	m.mu.Unlock()
	if changed && m.onChange != nil {
		m.onChange(m.Peers())
	}
}

func (m *Manager) announce(announce bool) {
	peer := m.self
	peer.Announce = announce
	data, _ := json.Marshal(peer)
	conn, err := net.DialUDP("udp4", nil, &net.UDPAddr{IP: group, Port: DefaultPort})
	if err != nil {
		return
	}
	defer conn.Close()
	_, _ = conn.Write(data)
}

func (m *Manager) reply(addr *net.UDPAddr) {
	peer := m.self
	peer.Announce = false
	data, _ := json.Marshal(peer)
	_, _ = m.conn.WriteToUDP(data, addr)
}
