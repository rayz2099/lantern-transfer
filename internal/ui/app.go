package ui

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/rayz2099/lantern-transfer/internal/transfer"
)

const defaultPort = 53317

// App 让传输状态只在 MyGo 主线程上修改，网络回调通过 Window.Update 汇入。
type App struct {
	win              *mygo.Window
	service          *transfer.Service
	peers            []transfer.Peer
	selected         string
	paths            []string
	manual           string
	link             string
	linkInput        string
	download         string
	progress         transfer.Progress
	busy             bool
	status           string
	tab              int
	history          []HistoryItem
	clearAlert       bool
	needsDownloadDir bool
}

// New 建立 UI 状态；网络服务在窗口可接收回调后启动。
func New() *App {
	return &App{status: "正在启动局域网服务"}
}

// Open 初始化用户目录与网络监听，失败信息留在主界面以便排查。
func (a *App) Open() {
	home, err := os.UserHomeDir()
	if err != nil {
		a.needsDownloadDir = true
		a.status = "无法确定下载目录，请在设置中选择: " + err.Error()
	} else {
		a.download = filepath.Join(home, "Downloads")
		if err := os.MkdirAll(a.download, 0755); err != nil {
			a.needsDownloadDir = true
			a.status = "无法创建下载目录，请在设置中选择: " + err.Error()
		}
	}
	if dir, loadErr := loadDownloadDir(); loadErr != nil {
		a.needsDownloadDir = true
		a.status = "已保存的下载目录不可用，请在设置中重新选择: " + loadErr.Error()
	} else if dir != "" {
		a.download = dir
	}
	a.history, err = loadHistory()
	if err != nil {
		if !a.needsDownloadDir {
			a.status = "读取历史失败: " + err.Error()
		}
	} else {
		if err := saveHistory(a.history); err != nil {
			if !a.needsDownloadDir {
				a.status = "清理过期历史失败: " + err.Error()
			}
		}
	}
	a.win = mygo.NewWindow(mygo.WindowOptions{
		Title: "Lantern Transfer", Width: 900, Height: 650,
		MinWidth: 690, MinHeight: 520, StateKey: "main", Content: ui.View(a.view),
	})
	host, _ := os.Hostname()
	a.service = transfer.New(transfer.Config{
		Alias: host, DownloadDir: a.download, Port: defaultPort,
		OnIncoming: a.confirmIncoming, OnProgress: a.onProgress, OnPeers: a.onPeers, OnComplete: a.onComplete,
	})
	if a.needsDownloadDir {
		return
	}
	if err := a.service.Start(); err != nil {
		a.status = "局域网服务启动失败: " + err.Error()
		return
	}
	a.status = "已就绪 · 同一局域网内的设备会自动出现"
}

func (a *App) onPeers(peers []transfer.Peer) {
	a.win.Update(func() { a.peers = peers })
}

func (a *App) onProgress(p transfer.Progress) {
	a.win.Update(func() {
		a.progress = p
	})
}

func (a *App) onComplete(p transfer.Progress, err error) {
	if p.Direction != "receive" {
		return
	}
	a.win.Update(func() {
		result := "成功"
		if err != nil {
			result = "失败"
			a.status = "接收失败: " + err.Error()
		}
		a.addHistory(HistoryItem{Time: time.Now(), Direction: "接收", Peer: p.Peer, Name: p.File, Size: p.Total, Result: result})
	})
}

// confirmIncoming 在网络协程等待用户选择；系统对话框避免 UI 状态跨线程竞争。
func (a *App) confirmIncoming(ctx context.Context, in transfer.Incoming) bool {
	name := in.Peer.Hostname
	if name == "" {
		name = in.Peer.Alias
	}
	if name == "" {
		name = in.Peer.Address
	}
	label := fmt.Sprintf("%s 要发送 %d 个文件", name, len(in.Files))
	res, err := mygo.Dialog.Message(mygo.MessageOptions{
		Parent: a.win, Message: label, Detail: fileSummary(in.Files),
		Buttons: []string{"拒绝", "接收"}, DefaultButton: 1, CancelButton: 0,
	})
	if err != nil || ctx.Err() != nil {
		return false
	}
	return res.Button == 1
}

func fileSummary(files []transfer.FileMeta) string {
	var names []string
	for i, f := range files {
		if i == 4 {
			names = append(names, fmt.Sprintf("另有 %d 个文件", len(files)-4))
			break
		}
		names = append(names, f.Name)
	}
	return strings.Join(names, "\n")
}

func (a *App) addHistory(item HistoryItem) {
	a.history = append([]HistoryItem{item}, a.history...)
	if len(a.history) > 2000 {
		a.history = a.history[:2000]
	}
	if err := saveHistory(a.history); err != nil {
		a.status = "保存历史失败: " + err.Error()
	}
}

func (a *App) chosenPeer() (transfer.Peer, error) {
	if a.manual != "" {
		address := strings.TrimSpace(a.manual)
		port := defaultPort
		if host, rawPort, err := net.SplitHostPort(address); err == nil {
			address = host
			port, err = strconv.Atoi(rawPort)
			if err != nil || port < 1 || port > 65535 {
				return transfer.Peer{}, fmt.Errorf("端口无效")
			}
		}
		if net.ParseIP(address) == nil {
			return transfer.Peer{}, fmt.Errorf("请输入有效的局域网 IP")
		}
		return transfer.Peer{Address: address, Port: port, Hostname: address}, nil
	}
	for _, peer := range a.peers {
		if peer.Address == a.selected {
			return peer, nil
		}
	}
	return transfer.Peer{}, fmt.Errorf("请选择接收设备或输入地址")
}

func (a *App) send() {
	if a.busy || len(a.paths) == 0 || a.service == nil {
		return
	}
	peer, err := a.chosenPeer()
	if err != nil {
		a.status = err.Error()
		return
	}
	paths := append([]string(nil), a.paths...)
	manual := strings.TrimSpace(a.manual) != ""
	a.busy = true
	a.status = "正在连接接收设备"
	go func() {
		var err error
		if manual {
			peer, err = a.service.Probe(context.Background(), peer.Address, peer.Port)
		}
		if err == nil {
			err = a.service.Send(context.Background(), peer, paths)
		}
		a.win.Update(func() {
			a.busy = false
			result := "成功"
			if err != nil {
				result = "失败"
				a.status = "发送失败: " + err.Error()
				a.addHistory(HistoryItem{Time: time.Now(), Direction: "发送", Peer: peer.Hostname, Name: fmt.Sprintf("%d 个项目", len(paths)), Result: result})
			} else {
				a.status = "发送完成"
				for _, path := range paths {
					var size int64
					if info, statErr := os.Stat(path); statErr == nil && !info.IsDir() {
						size = info.Size()
					}
					a.addHistory(HistoryItem{Time: time.Now(), Direction: "发送", Peer: peer.Hostname, Name: filepath.Base(path), Size: size, Result: result})
				}
			}
		})
	}()
}

func (a *App) pickFiles(directory bool) {
	go func() {
		paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{
			Parent: a.win, Title: "选择要发送的内容", Multiple: !directory, Directory: directory,
		})
		a.win.Update(func() {
			if err != nil {
				a.status = "选择文件失败: " + err.Error()
				return
			}
			a.queue(paths)
		})
	}()
}

func (a *App) queue(paths []string) {
	for _, path := range paths {
		if path == "" {
			continue
		}
		found := false
		for _, old := range a.paths {
			if old == path {
				found = true
				break
			}
		}
		if !found {
			a.paths = append(a.paths, path)
		}
	}
}

func (a *App) createLink() {
	if a.service == nil || len(a.paths) == 0 || a.busy {
		a.status = "请先选择文件"
		return
	}
	paths := append([]string(nil), a.paths...)
	a.busy = true
	a.status = "正在准备分享链接"
	go func() {
		link, err := a.service.CreateLink(paths)
		a.win.Update(func() {
			a.busy = false
			if err != nil {
				a.status = "创建链接失败: " + err.Error()
				return
			}
			a.link = link
			mygo.Clipboard.WriteText(link)
			a.status = "链接已复制，10 分钟内可在同一局域网使用"
		})
	}()
}

func (a *App) downloadLink() {
	if a.service == nil || a.busy {
		return
	}
	link := strings.TrimSpace(a.linkInput)
	if link == "" {
		link = strings.TrimSpace(mygo.Clipboard.ReadText())
		a.linkInput = link
	}
	if link == "" {
		a.status = "剪贴板没有分享链接"
		return
	}
	a.busy = true
	a.status = "正在从分享链接下载"
	go func() {
		err := a.service.DownloadLink(context.Background(), link, a.download)
		a.win.Update(func() {
			a.busy = false
			result := "成功"
			if err != nil {
				result = "失败"
				a.status = "下载失败: " + err.Error()
			} else {
				a.status = "下载完成"
			}
			a.addHistory(HistoryItem{Time: time.Now(), Direction: "下载", Peer: linkHost(link), Name: "分享链接", Result: result})
		})
	}()
}

func linkHost(link string) string {
	if parts := strings.SplitN(strings.TrimPrefix(strings.TrimPrefix(link, "https://"), "http://"), "/", 2); len(parts) > 0 {
		return parts[0]
	}
	return link
}

func (a *App) chooseDownloadDir() {
	go func() {
		paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{Parent: a.win, Title: "选择下载目录", Directory: true})
		a.win.Update(func() {
			if err != nil {
				a.status = "选择目录失败: " + err.Error()
				return
			}
			if len(paths) > 0 {
				if err := a.service.SetDownloadDir(paths[0]); err != nil {
					a.status = "更改下载目录失败: " + err.Error()
					return
				}
				if err := saveDownloadDir(paths[0]); err != nil {
					if !a.needsDownloadDir {
						_ = a.service.SetDownloadDir(a.download)
					}
					a.status = "保存下载目录失败: " + err.Error()
					return
				}
				a.download = paths[0]
				if a.needsDownloadDir {
					if err := a.service.Start(); err != nil {
						a.status = "局域网服务启动失败: " + err.Error()
						return
					}
					a.needsDownloadDir = false
				}
				a.status = "下载目录已更新"
			}
		})
	}()
}
