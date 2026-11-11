package ui

import (
	"fmt"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/plugins/updater"
	"github.com/egoist/mygo/ui"
)

func (a *App) historyPage(c *ui.Context) {
	ui.Column(c).Grow(1).MinHeight(0).Padding(26, 30).Children(func() {
		ui.Row(c).AlignItems(ui.Center).Children(func() {
			ui.Text(c, fmt.Sprintf("最近 90 天 · %d 条", len(a.history))).FontSize(13).TextColor(muted).Grow(1)
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
		ui.Scroll(c).Grow(1).PaddingY(16).Gap(2).Children(func() {
			if len(a.history) == 0 {
				ui.Column(c).Fill().Center().Children(func() { ui.Text(c, "还没有传输记录").TextColor(muted) })
				return
			}
			for i, item := range a.history {
				ui.Row(c.Key(i)).Height(57).Padding(8, 4).AlignItems(ui.Center).Gap(12).Children(func() {
					ui.Column(c).Grow(1).MinWidth(0).Gap(2).Children(func() {
						ui.Text(c, item.Name).FontSize(13).Bold().TextColor(ink).SingleLine()
						ui.Text(c, item.Direction+" · "+item.Peer).FontSize(11).TextColor(muted).SingleLine()
					})
					ui.Text(c, bytes(item.Size)).FontSize(11).TextColor(muted).Width(78)
					ui.Text(c, item.Time.Local().Format("01-02 15:04")).FontSize(11).TextColor(muted).Width(84)
					color := blue
					if item.Result != "成功" {
						color = ui.Hex("#B34A3A")
					}
					ui.Text(c, item.Result).FontSize(11).TextColor(color).Width(42)
				})
				ui.Box(c).Height(1).Background(line)
			}
		})
	})
}

func (a *App) settingsPage(c *ui.Context) {
	ui.Column(c).Grow(1).Padding(30).Gap(22).Children(func() {
		ui.Row(c).AlignItems(ui.Center).Gap(16).Children(func() {
			ui.Column(c).Grow(1).MinWidth(0).Gap(4).Children(func() {
				ui.Text(c, "下载目录").FontSize(14).Bold().TextColor(ink)
				ui.Text(c, a.download).FontSize(12).TextColor(muted).SingleLine()
			})
			if ui.Button(c, "更改").Clicked() {
				a.chooseDownloadDir()
			}
		})
		ui.Box(c).Height(1).Background(line)
		ui.Row(c).AlignItems(ui.Center).Gap(16).Children(func() {
			ui.Column(c).Grow(1).Gap(4).Children(func() {
				ui.Text(c, "应用更新").FontSize(14).Bold().TextColor(ink)
				ui.Text(c, "从 GitHub Releases 获取签名更新").FontSize(12).TextColor(muted)
			})
			if ui.Button(c, "检查更新").Clicked() {
				updater.CheckForUpdates()
			}
		})
		ui.Box(c).Height(1).Background(line)
		ui.Text(c, "HTTP 传输未加密，仅供可信局域网使用。").FontSize(12).TextColor(muted)
	})
}

func (a *App) manualPage(c *ui.Context) {
	ui.Column(c).Grow(1).MinHeight(0).Children(func() {
		ui.Column(c).Grow(1).Padding(30).Center().Children(func() {
			ui.Column(c).Width(420).Gap(16).Children(func() {
				ui.Text(c, "设备地址").FontSize(16).Bold().TextColor(ink)
				ui.TextInput(c, &a.manual).Placeholder("局域网 IP，可加 :端口").Label("设备地址")
				if len(a.paths) > 0 {
					ui.Text(c, fmt.Sprintf("已选 %d 个项目", len(a.paths))).FontSize(12).TextColor(muted)
				}
				label := "发送"
				if len(a.paths) == 0 {
					label = "选择文件并发送"
				}
				if ui.PrimaryButton(c, label).Disabled(a.busy).Clicked() {
					if a.manual == "" {
						a.status = "请输入接收设备的局域网 IP"
					} else if len(a.paths) == 0 {
						a.pickFiles(false, true)
					} else {
						a.send()
					}
				}
			})
		})
		if a.busy || a.progress.File != "" {
			a.transferBar(c)
		}
	})
}

func (a *App) linkPage(c *ui.Context) {
	ui.Column(c).Grow(1).MinHeight(0).Children(func() {
		ui.Column(c).Grow(1).Padding(30).Center().Children(func() {
			ui.Column(c).Width(500).Gap(16).Children(func() {
				ui.Text(c, "分享链接").FontSize(16).Bold().TextColor(ink)
				ui.TextInput(c, &a.linkInput).Placeholder("Lantern 分享链接").Label("分享链接")
				ui.Row(c).Gap(10).Children(func() {
					if ui.Button(c, "从剪贴板粘贴").Clicked() {
						a.linkInput = mygo.Clipboard.ReadText()
					}
					if ui.PrimaryButton(c, "下载").Disabled(a.busy).Clicked() {
						a.downloadLink()
					}
				})
			})
		})
		if a.busy || a.progress.File != "" {
			a.transferBar(c)
		}
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
