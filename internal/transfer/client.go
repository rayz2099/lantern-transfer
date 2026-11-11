package transfer

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

func (s *Service) Send(ctx context.Context, peer Peer, paths []string) error {
	files, local, err := collectFiles(ctx, paths)
	if err != nil {
		return err
	}
	if peer.Protocol != "http" {
		return errors.New("设备未启用 HTTP 局域网模式")
	}
	ip := net.ParseIP(peer.Address)
	if ip == nil || (!ip.IsPrivate() && !ip.IsLoopback()) {
		return errors.New("设备地址不是局域网 IP")
	}
	if peer.Port <= 0 {
		peer.Port = 53317
	}
	base := peer.Protocol + "://" + net.JoinHostPort(peer.Address, strconv.Itoa(peer.Port))
	data, _ := json.Marshal(prepareUpload{Info: s.self, Files: files})
	client := &http.Client{CheckRedirect: rejectRedirect}
	approval, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	req, err := http.NewRequestWithContext(approval, http.MethodPost, base+"/api/localsend/v2/prepare-upload", strings.NewReader(string(data)))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := client.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return fmt.Errorf("接收方未接受: HTTP %d", res.StatusCode)
	}
	var prepared prepareResult
	if err = json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(&prepared); err != nil {
		return err
	}
	if prepared.SessionID == "" {
		return errors.New("接收方未返回会话 ID")
	}
	defer func() {
		stop, done := context.WithTimeout(context.Background(), 5*time.Second)
		defer done()
		cancelReq, _ := http.NewRequestWithContext(stop, http.MethodPost, base+"/api/localsend/v2/cancel?sessionId="+url.QueryEscape(prepared.SessionID), nil)
		if cancelReq != nil {
			response, e := client.Do(cancelReq)
			if e == nil {
				response.Body.Close()
			}
		}
	}()
	ids := sortedIDs(files)
	for _, id := range ids {
		token := prepared.Files[id]
		if token == "" {
			return fmt.Errorf("接收方未接受文件 %s", files[id].Name)
		}
		f, err := os.Open(local[id])
		if err != nil {
			return err
		}
		meta := files[id]
		reader := &progressReader{reader: f, notify: func(done int64) {
			s.progress(Progress{Direction: "send", Peer: peer.Alias, File: meta.Name, Done: done, Total: meta.Size})
		}}
		query := url.Values{"sessionId": {prepared.SessionID}, "fileId": {id}, "token": {token}}
		endpoint := base + "/api/localsend/v2/upload?" + query.Encode()
		req, err = http.NewRequestWithContext(ctx, http.MethodPost, endpoint, reader)
		if err != nil {
			f.Close()
			return err
		}
		req.ContentLength = meta.Size
		req.Header.Set("Content-Type", "application/octet-stream")
		response, err := client.Do(req)
		f.Close()
		if err != nil {
			return err
		}
		response.Body.Close()
		if response.StatusCode != 200 {
			return fmt.Errorf("传输 %s 失败: HTTP %d", meta.Name, response.StatusCode)
		}
	}
	return nil
}

// CreateLink 只公开所选文件且十分钟失效，下载仍须发送方逐次确认。
func (s *Service) CreateLink(paths []string) (string, error) {
	files, local, err := collectFiles(context.Background(), paths)
	if err != nil {
		return "", err
	}
	ip, err := localIPv4()
	if err != nil {
		return "", err
	}
	token := randomID()
	s.mu.Lock()
	s.share = &session{files: files, paths: local, tokens: map[string]string{"share": token}, expires: time.Now().Add(10 * time.Minute)}
	s.mu.Unlock()
	query := url.Values{"pin": {token}}
	return "http://" + net.JoinHostPort(ip, strconv.Itoa(s.config.Port)) + "/?" + query.Encode(), nil
}

func (s *Service) DownloadLink(ctx context.Context, link string, dir string) error {
	parsed, err := url.Parse(link)
	if err != nil || parsed.Scheme != "http" || parsed.User != nil || parsed.Hostname() == "" || parsed.Query().Get("pin") == "" {
		return errors.New("无效的局域网分享链接")
	}
	ip := net.ParseIP(parsed.Hostname())
	if ip == nil || (!ip.IsPrivate() && !ip.IsLoopback()) {
		return errors.New("链接必须指向局域网 IP")
	}
	if dir == "" {
		s.mu.RLock()
		dir = s.config.DownloadDir
		s.mu.RUnlock()
	}
	info, err := os.Stat(dir)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return errors.New("保存位置不是目录")
	}
	base := parsed.Scheme + "://" + parsed.Host
	query := url.Values{"pin": {parsed.Query().Get("pin")}}
	approval, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	req, err := http.NewRequestWithContext(approval, http.MethodPost, base+"/api/localsend/v2/prepare-download?"+query.Encode(), nil)
	if err != nil {
		return err
	}
	client := &http.Client{CheckRedirect: rejectRedirect}
	res, err := client.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return fmt.Errorf("发送方未接受下载: HTTP %d", res.StatusCode)
	}
	var prepared downloadResult
	if err = json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(&prepared); err != nil {
		return err
	}
	if prepared.SessionID == "" {
		return errors.New("发送方未返回会话 ID")
	}
	for _, id := range sortedIDs(prepared.Files) {
		meta := prepared.Files[id]
		name := safeName(meta.Name)
		if name == "" || meta.Size < 0 {
			return errors.New("分享文件名称或大小无效")
		}
		full, err := destination(dir, name)
		if err != nil {
			return err
		}
		q := url.Values{"sessionId": {prepared.SessionID}, "fileId": {id}}
		req, err = http.NewRequestWithContext(ctx, http.MethodGet, base+"/api/localsend/v2/download?"+q.Encode(), nil)
		if err != nil {
			return err
		}
		response, err := client.Do(req)
		if err != nil {
			return err
		}
		if response.StatusCode != 200 {
			response.Body.Close()
			return fmt.Errorf("下载 %s 失败: HTTP %d", name, response.StatusCode)
		}
		tmp, err := os.CreateTemp(filepath.Dir(full), ".lantern-*")
		if err != nil {
			response.Body.Close()
			return err
		}
		hash := sha256.New()
		reader := &progressReader{reader: io.LimitReader(response.Body, meta.Size+1), notify: func(done int64) {
			s.progress(Progress{Direction: "download", Peer: parsed.Hostname(), File: name, Done: done, Total: meta.Size})
		}}
		count, copyErr := io.Copy(io.MultiWriter(tmp, hash), reader)
		closeErr := tmp.Close()
		response.Body.Close()
		if copyErr != nil || closeErr != nil || count != meta.Size {
			os.Remove(tmp.Name())
			return fmt.Errorf("文件 %s 大小不匹配", name)
		}
		if meta.SHA256 != "" && !strings.EqualFold(meta.SHA256, hex.EncodeToString(hash.Sum(nil))) {
			os.Remove(tmp.Name())
			return fmt.Errorf("文件 %s 校验失败", name)
		}
		final, err := reservePath(full)
		if err != nil {
			os.Remove(tmp.Name())
			return err
		}
		if err = os.Rename(tmp.Name(), final); err != nil {
			os.Remove(final)
			os.Remove(tmp.Name())
			return err
		}
		s.complete(Progress{Direction: "download", Peer: parsed.Hostname(), File: name, Done: count, Total: meta.Size}, nil)
	}
	return nil
}

// rejectRedirect 防止局域网链接把文件请求重定向到外部地址。
func rejectRedirect(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }

type progressReader struct {
	reader io.Reader
	notify func(int64)
	done   int64
}

func (p *progressReader) Read(buf []byte) (int, error) {
	n, err := p.reader.Read(buf)
	p.done += int64(n)
	if n > 0 {
		p.notify(p.done)
	}
	return n, err
}

type contextReader struct {
	ctx context.Context
	r   io.Reader
}

func (r contextReader) Read(buf []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.r.Read(buf)
}

// collectFiles 在校验阶段响应取消，避免大文件准备过程锁住下一次操作。
func collectFiles(ctx context.Context, paths []string) (map[string]FileMeta, map[string]string, error) {
	files := make(map[string]FileMeta)
	local := make(map[string]string)
	for _, root := range paths {
		if err := ctx.Err(); err != nil {
			return nil, nil, err
		}
		root = filepath.Clean(root)
		if _, err := os.Stat(root); err != nil {
			return nil, nil, err
		}
		base := filepath.Base(root)
		err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if err := ctx.Err(); err != nil {
				return err
			}
			if entry.IsDir() {
				return nil
			}
			if !entry.Type().IsRegular() {
				return nil
			}
			rel, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}
			name := base
			if path != root {
				name = filepath.ToSlash(filepath.Join(base, rel))
			}
			info, err := entry.Info()
			if err != nil {
				return err
			}
			f, err := os.Open(path)
			if err != nil {
				return err
			}
			hash := sha256.New()
			_, err = io.Copy(hash, contextReader{ctx: ctx, r: f})
			f.Close()
			if err != nil {
				return err
			}
			id := randomID()
			files[id] = FileMeta{ID: id, Name: name, Size: info.Size(), FileType: mime.TypeByExtension(filepath.Ext(name)), SHA256: hex.EncodeToString(hash.Sum(nil))}
			local[id] = path
			return nil
		})
		if err != nil {
			return nil, nil, err
		}
	}
	if len(files) == 0 {
		return nil, nil, errors.New("没有可传输的文件")
	}
	return files, local, nil
}

func sortedIDs(files map[string]FileMeta) []string {
	ids := make([]string, 0, len(files))
	for id := range files {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return files[ids[i]].Name < files[ids[j]].Name })
	return ids
}

func localIPv4() (string, error) {
	interfaces, err := net.Interfaces()
	if err != nil {
		return "", err
	}
	for _, nic := range interfaces {
		if nic.Flags&net.FlagUp == 0 || nic.Flags&net.FlagLoopback != 0 {
			continue
		}
		addresses, err := nic.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addresses {
			ip, _, err := net.ParseCIDR(addr.String())
			if err == nil && ip.To4() != nil && ip.IsPrivate() {
				return ip.String(), nil
			}
		}
	}
	return "", errors.New("未找到局域网 IPv4 地址")
}
