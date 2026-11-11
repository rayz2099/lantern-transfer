package ui

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/rayz2099/lantern-transfer/internal/transfer"
)

const (
	pageNearby = iota
	pageHistory
	pageSettings
	pageManual
	pageLink
)

var (
	ink      = ui.Hex("#18232F")
	muted    = ui.Hex("#667583")
	blue     = ui.Hex("#1677E8")
	line     = ui.Hex("#E1E7EE")
	canvas   = ui.Hex("#F7F9FC")
	blueWash = ui.Hex("#EAF3FF")
	white    = ui.Hex("#FFFFFF")
)

// view 保留 AirDrop 的单任务层级：设备是主操作，文件与高级入口围绕它排列。
func (a *App) view(c *ui.Context) {
	theme := ui.LightTheme()
	theme.Accent = blue
	theme.AccentHover = ui.Hex("#0D68D5")
	theme.AccentPressed = ui.Hex("#095CBF")
	theme.Selection = blueWash
	theme.Focus = blue
	c.SetTheme(theme)
	ui.Column(c).Fill().Background(canvas).Children(func() {
		a.toolbar(c)
		switch a.tab {
		case pageHistory:
			a.historyPage(c)
		case pageSettings:
			a.settingsPage(c)
		case pageManual:
			a.manualPage(c)
		case pageLink:
			a.linkPage(c)
		default:
			a.nearbyPage(c)
		}
		a.statusBar(c)
	})
}

func iconButton(c *ui.Context, label string, icon *ui.SVG) ui.Element {
	button := ui.ButtonBase(c).Size(38, 38).Center().Radius(8).
		Label(label).Tooltip(label).Cursor(ui.CursorPointer)
	if button.Hovered() {
		button.Background(blueWash)
	}
	if button.Pressed() {
		button.Background(ui.Hex("#D9EAFE"))
	}
	button.Children(func() { ui.Icon(c, icon).Size(19, 19).TextColor(ink) })
	return button
}

func (a *App) toolbar(c *ui.Context) {
	ui.Row(c).Height(64).Padding(10, 24).AlignItems(ui.Center).
		Background(white).BorderWidth(0, 0, 1, 0).BorderColor(line).Children(func() {
		if a.tab != pageNearby {
			if iconButton(c, "返回附近设备", backIcon).Clicked() {
				a.tab = pageNearby
			}
		}
		title := "Lantern"
		if a.tab != pageNearby {
			title = map[int]string{
				pageHistory: "传输历史", pageSettings: "设置", pageManual: "通过 IP 连接", pageLink: "从链接下载",
			}[a.tab]
		}
		ui.Text(c, title).FontSize(19).Bold().TextColor(ink).Grow(1)
		if a.tab != pageNearby {
			return
		}
		if iconButton(c, "传输历史", clockIcon).Clicked() {
			a.tab = pageHistory
		}
		if iconButton(c, "设置", settingsIcon).Clicked() {
			a.tab = pageSettings
		}
		iconButton(c, "更多操作", moreIcon).Menu(func(m *ui.Menu) {
			if m.Item("通过 IP 连接").Chosen() {
				a.tab = pageManual
			}
			if m.Item("从链接下载").Chosen() {
				a.tab = pageLink
			}
			if len(a.paths) > 0 {
				m.Separator()
				if m.Item("复制分享链接").Disabled(a.busy).Chosen() {
					a.createLink()
				}
			}
		})
	})
}

func (a *App) statusBar(c *ui.Context) {
	ui.Row(c).Height(38).Padding(8, 24).AlignItems(ui.Center).
		Background(white).BorderWidth(1, 0, 0, 0).BorderColor(line).Children(func() {
		ui.Box(c).Size(7, 7).Radius(4).Background(blue)
		ui.Text(c, a.status).FontSize(11).TextColor(muted).Grow(1).PaddingX(8).SingleLine()
		if version := mygo.App.Version(); version != "" {
			ui.Text(c, "v"+version).FontSize(11).TextColor(muted)
		}
	})
}

func (a *App) nearbyPage(c *ui.Context) {
	root := ui.Column(c).Grow(1).MinHeight(0)
	if root.FileDragOver() {
		root.Background(blueWash)
	}
	if paths := root.DroppedFiles(); len(paths) > 0 {
		a.queue(paths)
	}
	root.Children(func() {
		ui.Column(c).Grow(1).MinHeight(0).Padding(30, 28, 0).Children(func() {
			heading := "附近设备"
			if len(a.paths) > 0 {
				heading = "发送给"
			}
			ui.Text(c, heading).FontSize(24).Bold().TextColor(ink)
			ui.Text(c, fmt.Sprintf("%d 台设备可用", len(a.peers))).FontSize(13).TextColor(muted).PaddingY(5)
			a.deviceGrid(c)
		})
		if a.busy || a.progress.File != "" {
			a.transferBar(c)
		}
		a.fileDock(c)
	})
}

func (a *App) deviceGrid(c *ui.Context) {
	ui.Scroll(c).Grow(1).MinHeight(0).Children(func() {
		content := ui.Column(c).Center()
		if len(a.peers) == 0 {
			content.MinHeight(220)
		} else {
			content.MinHeight(310)
		}
		content.Children(func() {
			if len(a.peers) == 0 {
				ui.Column(c).Center().Gap(12).Children(func() {
					ui.Box(c).Size(96, 96).Radius(48).Background(blueWash).Center().Children(func() {
						ui.Icon(c, monitorIcon).Size(40, 40).TextColor(blue)
					})
					ui.Text(c, "正在寻找附近设备").FontSize(16).Bold().TextColor(ink)
					if ui.Button(c, "通过 IP 连接").Clicked() {
						a.tab = pageManual
					}
				})
				return
			}
			ui.Row(c).MaxWidth(720).Wrap().Justify(ui.Center).Gap(20).Children(func() {
				for _, peer := range a.peers {
					a.device(c, peer)
				}
			})
		})
	})
}

func deviceName(peer transfer.Peer) string {
	if peer.Hostname != "" {
		return peer.Hostname
	}
	if peer.Alias != "" {
		return peer.Alias
	}
	return peer.Address
}

func (a *App) device(c *ui.Context, peer transfer.Peer) {
	name := deviceName(peer)
	tile := ui.Column(c.Key(peer.Address)).Width(136).Padding(12, 4).Center().Gap(9).
		Radius(8).Label(name).Cursor(ui.CursorPointer)
	if tile.Hovered() || a.selected == peer.Address {
		tile.Background(blueWash)
	}
	if tile.Clicked() {
		a.chooseDevice(peer)
	}
	tile.Children(func() {
		ui.Avatar(c, name, nil).Size(82, 82).Radius(41)
		ui.Text(c, name).FontSize(13).Bold().TextColor(ink).SingleLine().MaxWidth(122)
	})
}

func queueNames(paths []string) string {
	if len(paths) == 0 {
		return ""
	}
	names := make([]string, 0, 3)
	for i, path := range paths {
		if i == 2 {
			break
		}
		names = append(names, filepath.Base(path))
	}
	if len(paths) > 2 {
		names = append(names, fmt.Sprintf("等 %d 个项目", len(paths)))
	}
	return strings.Join(names, " · ")
}

func (a *App) fileDock(c *ui.Context) {
	ui.Row(c).Height(106).Padding(18, 26).Gap(16).AlignItems(ui.Center).
		Background(white).BorderWidth(1, 0, 0, 0).BorderColor(line).Children(func() {
		if len(a.paths) == 0 {
			ui.Text(c, "选择要发送的内容").FontSize(13).TextColor(muted).Grow(1)
		} else {
			ui.Column(c).Grow(1).MinWidth(0).Gap(4).Children(func() {
				ui.Text(c, fmt.Sprintf("已选 %d 个项目", len(a.paths))).FontSize(14).Bold().TextColor(ink)
				ui.Text(c, queueNames(a.paths)).FontSize(11).TextColor(muted).SingleLine()
			})
			if iconButton(c, "清空选择", closeIcon).Disabled(a.busy).Clicked() {
				a.paths = nil
			}
		}
		if ui.Button(c, "文件夹").Disabled(a.busy).Clicked() {
			a.pickFiles(true, false)
		}
		if ui.PrimaryButton(c, "选择文件").Disabled(a.busy).Clicked() {
			a.pickFiles(false, false)
		}
	})
}

func (a *App) transferBar(c *ui.Context) {
	ui.Row(c).Height(70).Padding(10, 26).Gap(14).AlignItems(ui.Center).Background(blueWash).Children(func() {
		ui.Column(c).Grow(1).MinWidth(0).Gap(6).Children(func() {
			name := a.progress.File
			if name == "" {
				name = "准备传输"
			}
			ui.Text(c, name).FontSize(12).Bold().TextColor(ink).SingleLine()
			fraction := -1.0
			if a.progress.Total > 0 {
				fraction = float64(a.progress.Done) / float64(a.progress.Total)
			}
			ui.Progress(c, fraction).Label("传输进度")
		})
		if a.cancelTransfer != nil && iconButton(c, "取消传输", closeIcon).Clicked() {
			a.cancelTransfer()
			a.status = "正在取消传输"
		}
	})
}
