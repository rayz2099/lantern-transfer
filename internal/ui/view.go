package ui

import (
	"fmt"
	"path/filepath"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/plugins/updater"
	"github.com/egoist/mygo/ui"
)

var (
	ink   = ui.Hex("#27343A")
	muted = ui.Hex("#66757A")
	teal  = ui.Hex("#147D78")
	line  = ui.Hex("#DDE5E4")
	soft  = ui.Hex("#F4F8F7")
)

// view 用固定浅色主题，保证系统深色模式下仍符合首版视觉要求。
func (a *App) view(c *ui.Context) {
	theme := ui.LightTheme()
	theme.Accent = ui.Hex("#C45140")
	theme.AccentHover = ui.Hex("#B74635")
	theme.AccentPressed = ui.Hex("#A53C2D")
	theme.Selection = ui.Hex("#F5D8D2")
	theme.Focus = ui.Hex("#C45140")
	c.SetTheme(theme)
	ui.Column(c).Fill().Background(ui.Hex("#FFFFFF")).Children(func() {
		a.header(c)
		ui.Column(c).Grow(1).MinHeight(0).Padding(26, 30, 18).Children(func() {
			ui.Tabs(c, &a.tab, "传输", "历史", "设置")
			switch a.tab {
			case 0:
				a.transferPage(c)
			case 1:
				a.historyPage(c)
			case 2:
				a.settingsPage(c)
			}
		})
		a.footer(c)
	})
}

func (a *App) header(c *ui.Context) {
	ui.Row(c).Height(76).Padding(18, 30).AlignItems(ui.Center).Background(soft).Children(func() {
		ui.Column(c).Grow(1).Gap(2).Children(func() {
			ui.Text(c, "LANTERN TRANSFER").FontSize(17).Bold().TextColor(ink)
			ui.Text(c, "局域网文件传输").FontSize(12).TextColor(muted)
		})
		ui.Text(c, "●  局域网直连").FontSize(12).Bold().TextColor(teal)
	})
	ui.Box(c).Height(1).Background(line)
}

func (a *App) footer(c *ui.Context) {
	ui.Box(c).Height(1).Background(line)
	ui.Row(c).Height(43).Padding(10, 30).AlignItems(ui.Center).Children(func() {
		ui.Text(c, a.status).FontSize(12).TextColor(muted).Grow(1).SingleLine()
		version := mygo.App.Version()
		if version != "" {
			ui.Text(c, "v"+version).FontSize(11).TextColor(muted)
		}
	})
}

func (a *App) transferPage(c *ui.Context) {
	ui.Row(c).Grow(1).MinHeight(0).Gap(28).PaddingY(20).Children(func() {
		ui.Column(c).Grow(1).MinWidth(0).Gap(16).Children(func() {
			a.devices(c)
			a.manualAddress(c)
			a.downloadSection(c)
		})
		ui.Box(c).Width(1).Background(line)
		ui.Column(c).Grow(1).MinWidth(0).Gap(14).Children(func() {
			a.fileQueue(c)
			a.sendActions(c)
			a.progressView(c)
		})
	})
}

func sectionTitle(c *ui.Context, title, subtitle string) {
	ui.Text(c, title).FontSize(16).Bold().TextColor(ink)
	ui.Text(c, subtitle).FontSize(12).TextColor(muted)
}

func (a *App) devices(c *ui.Context) {
	ui.Row(c).AlignItems(ui.Center).Children(func() {
		ui.Column(c).Grow(1).Gap(3).Children(func() {
			sectionTitle(c, "附近设备", "选择同一局域网内的接收设备")
		})
		ui.Text(c, fmt.Sprintf("%d 台在线", len(a.peers))).FontSize(11).TextColor(teal)
	})
	ui.Scroll(c).Height(190).PaddingY(8).Gap(6).Children(func() {
		if len(a.peers) == 0 {
			ui.Column(c).Fill().Center().Gap(5).Children(func() {
				ui.Text(c, "正在寻找设备").FontSize(14).TextColor(ink)
				ui.Text(c, "也可以在下方输入对方的局域网地址").FontSize(12).TextColor(muted)
			})
			return
		}
		for _, peer := range a.peers {
			name := peer.Hostname
			if name == "" {
				name = peer.Alias
			}
			if name == "" {
				name = peer.Address
			}
			row := ui.Row(c.Key(peer.Address)).Height(58).Padding(9, 12).Radius(6).
				Border(1, line).AlignItems(ui.Center).Cursor(ui.CursorPointer)
			if a.selected == peer.Address {
				row.Background(soft).BorderColor(teal)
			}
			if row.Clicked() {
				a.selected = peer.Address
				a.manual = ""
			}
			row.Children(func() {
				ui.Box(c).Size(10, 10).Radius(5).Background(teal)
				ui.Column(c).Grow(1).PaddingX(10).Gap(2).Children(func() {
					ui.Text(c, name).FontSize(13).Bold().TextColor(ink).SingleLine()
					ui.Text(c, peer.Address).FontSize(11).TextColor(muted).SingleLine()
				})
				if a.selected == peer.Address {
					ui.Text(c, "已选择").FontSize(11).TextColor(teal)
				}
			})
		}
	})
}

func (a *App) manualAddress(c *ui.Context) {
	ui.Column(c).Gap(7).Children(func() {
		ui.Text(c, "手动连接").FontSize(13).Bold().TextColor(ink)
		ui.TextInput(c, &a.manual).Placeholder("局域网 IP，可加 :端口").Label("设备地址")
	})
}

func (a *App) downloadSection(c *ui.Context) {
	ui.Column(c).Gap(8).PaddingY(12).Children(func() {
		ui.Box(c).Height(1).Background(line)
		ui.Text(c, "从分享链接下载").FontSize(13).Bold().TextColor(ink)
		ui.TextInput(c, &a.linkInput).Placeholder("粘贴局域网分享链接").Label("分享链接")
		ui.Row(c).Gap(8).Children(func() {
			if ui.Button(c, "从剪贴板读取").Clicked() {
				a.linkInput = mygo.Clipboard.ReadText()
			}
			if ui.PrimaryButton(c, "下载").Disabled(a.busy).Clicked() {
				a.downloadLink()
			}
		})
	})
}

func (a *App) fileQueue(c *ui.Context) {
	sectionTitle(c, "待发送", "拖入文件，或从磁盘选择文件和文件夹")
	zone := ui.Column(c).Grow(1).MinHeight(165).Radius(6).Border(1, line).Padding(11).
		Background(soft).Gap(6)
	if zone.FileDragOver() {
		zone.Border(2, teal)
	}
	if paths := zone.DroppedFiles(); len(paths) > 0 {
		a.queue(paths)
	}
	zone.Children(func() {
		if len(a.paths) == 0 {
			ui.Column(c).Fill().Center().Gap(6).Children(func() {
				ui.Text(c, "拖放文件到这里").FontSize(14).TextColor(ink)
				ui.Text(c, "文件内容只在设备间传输").FontSize(12).TextColor(muted)
			})
			return
		}
		ui.Scroll(c).Grow(1).Gap(5).Children(func() {
			remove := -1
			for i, path := range a.paths {
				ui.Row(c.Key(path)).Height(38).Padding(6, 8).AlignItems(ui.Center).Gap(8).Children(func() {
					ui.Text(c, filepath.Base(path)).Grow(1).MinWidth(0).SingleLine().TextColor(ink)
					if ui.Button(c, "×").Label("移除 " + filepath.Base(path)).Tooltip("移除").Clicked() {
						remove = i
					}
				})
			}
			if remove >= 0 {
				a.paths = append(a.paths[:remove], a.paths[remove+1:]...)
			}
		})
	})
	ui.Row(c).Gap(8).Children(func() {
		if ui.Button(c, "添加文件").Clicked() {
			a.pickFiles(false)
		}
		if ui.Button(c, "添加文件夹").Clicked() {
			a.pickFiles(true)
		}
		if len(a.paths) > 0 && ui.Button(c, "清空").Clicked() {
			a.paths = nil
		}
	})
}

func (a *App) sendActions(c *ui.Context) {
	ui.Row(c).Gap(8).Children(func() {
		if ui.PrimaryButton(c, "发送到设备").Disabled(a.busy || len(a.paths) == 0).Clicked() {
			a.send()
		}
		if ui.Button(c, "复制分享链接").Disabled(a.busy || len(a.paths) == 0).Clicked() {
			a.createLink()
		}
	})
	if a.link != "" {
		ui.Text(c, a.link).FontSize(11).TextColor(teal).SingleLine()
	}
}

func (a *App) progressView(c *ui.Context) {
	if !a.busy && a.progress.File == "" {
		return
	}
	ui.Column(c).Gap(5).PaddingY(10).Children(func() {
		ui.Box(c).Height(1).Background(line)
		label := a.progress.File
		if label == "" {
			label = "准备传输"
		}
		ui.Text(c, label).FontSize(12).TextColor(ink).SingleLine()
		fraction := -1.0
		if a.progress.Total > 0 {
			fraction = float64(a.progress.Done) / float64(a.progress.Total)
		}
		ui.Progress(c, fraction).Label("传输进度")
		if a.progress.Total > 0 {
			ui.Text(c, fmt.Sprintf("%s / %s", bytes(a.progress.Done), bytes(a.progress.Total))).FontSize(11).TextColor(muted)
		}
	})
}

func (a *App) historyPage(c *ui.Context) {
	ui.Row(c).PaddingY(20).AlignItems(ui.Center).Children(func() {
		ui.Column(c).Grow(1).Gap(3).Children(func() {
			sectionTitle(c, "传输历史", "仅保存在这台设备上，保留最近 90 天")
		})
		if ui.Button(c, "清空历史").Disabled(len(a.history) == 0).Clicked() {
			a.clearAlert = true
		}
	})
	if ui.AlertDialog(c, &a.clearAlert, "清空传输历史？", "此操作只删除本机记录，不影响已传输的文件。", "取消", "清空") == 1 {
		a.history = nil
		if err := saveHistory(nil); err != nil {
			a.status = "清空历史失败: " + err.Error()
		}
	}
	ui.Scroll(c).Grow(1).Gap(2).Children(func() {
		if len(a.history) == 0 {
			ui.Column(c).Fill().Center().Children(func() {
				ui.Text(c, "还没有传输记录").TextColor(muted)
			})
			return
		}
		for i, item := range a.history {
			ui.Row(c.Key(i)).Height(57).Padding(7, 5).AlignItems(ui.Center).Gap(12).Children(func() {
				ui.Column(c).Grow(1).MinWidth(0).Gap(2).Children(func() {
					ui.Text(c, item.Name).FontSize(13).Bold().TextColor(ink).SingleLine()
					ui.Text(c, item.Direction+" · "+item.Peer).FontSize(11).TextColor(muted).SingleLine()
				})
				ui.Text(c, bytes(item.Size)).FontSize(11).TextColor(muted).Width(80)
				ui.Text(c, item.Time.Local().Format("01-02 15:04")).FontSize(11).TextColor(muted).Width(85)
				color := teal
				if item.Result != "成功" {
					color = ui.Hex("#B04C3C")
				}
				ui.Text(c, item.Result).FontSize(11).TextColor(color).Width(42)
			})
			ui.Box(c).Height(1).Background(line)
		}
	})
}

func (a *App) settingsPage(c *ui.Context) {
	ui.Column(c).PaddingY(22).Gap(18).Children(func() {
		sectionTitle(c, "设置", "接收目录与应用更新")
		ui.Box(c).Height(1).Background(line)
		ui.Row(c).AlignItems(ui.Center).Gap(12).Children(func() {
			ui.Column(c).Grow(1).MinWidth(0).Gap(3).Children(func() {
				ui.Text(c, "下载目录").FontSize(13).Bold().TextColor(ink)
				ui.Text(c, a.download).FontSize(11).TextColor(muted).SingleLine()
			})
			if ui.Button(c, "更改").Clicked() {
				a.chooseDownloadDir()
			}
		})
		ui.Box(c).Height(1).Background(line)
		ui.Row(c).AlignItems(ui.Center).Gap(12).Children(func() {
			ui.Column(c).Grow(1).Gap(3).Children(func() {
				ui.Text(c, "应用更新").FontSize(13).Bold().TextColor(ink)
				ui.Text(c, "从 GitHub Releases 获取签名更新").FontSize(11).TextColor(muted)
			})
			if ui.Button(c, "检查更新").Clicked() {
				updater.CheckForUpdates()
			}
		})
		ui.Box(c).Height(1).Background(line)
		ui.Text(c, "HTTP 传输未加密，仅供可信局域网使用。接收前需确认，链接 10 分钟后失效。").FontSize(11).TextColor(muted)
	})
}

func bytes(n int64) string {
	if n < 1024 {
		return fmt.Sprintf("%d B", n)
	}
	units := []string{"KB", "MB", "GB", "TB"}
	v := float64(n)
	for _, unit := range units {
		v /= 1024
		if v < 1024 || unit == "TB" {
			return fmt.Sprintf("%.1f %s", v, unit)
		}
	}
	return ""
}
