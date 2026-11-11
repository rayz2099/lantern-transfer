package ui

import (
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/rayz2099/lantern-transfer/internal/transfer"
)

func TestNearbyNavigation(t *testing.T) {
	app := New()
	app.status = "已就绪"
	tt := ui.NewTester(app.view, 900, 650)
	if !tt.HasText("附近设备") || tt.HasText("设备地址") {
		t.Fatalf("主界面内容不符合预期: %q", tt.Texts())
	}
	if err := tt.Click("传输历史"); err != nil {
		t.Fatal(err)
	}
	if app.tab != pageHistory || !tt.HasText("还没有传输记录") {
		t.Fatalf("历史入口未打开: %q", tt.Texts())
	}
	if err := tt.Click("返回附近设备"); err != nil {
		t.Fatal(err)
	}
	if app.tab != pageNearby {
		t.Fatal("返回入口未恢复附近设备")
	}
}

func TestDeviceIsPrimaryAction(t *testing.T) {
	app := New()
	app.paths = []string{"/tmp/sample.txt"}
	app.peers = []transfer.Peer{{Alias: "Office PC", Hostname: "office-pc", Address: "192.168.1.12"}}
	tt := ui.NewTester(app.view, 690, 520)
	if err := tt.Click("office-pc"); err != nil {
		t.Fatal(err)
	}
	if app.selected != "192.168.1.12" {
		t.Fatal("点击设备没有选中接收方")
	}
}

func TestEmptyStateManualConnectionAtMinimumSize(t *testing.T) {
	app := New()
	tt := ui.NewTester(app.view, 690, 520)
	if err := tt.Click("通过 IP 连接"); err != nil {
		t.Fatal(err)
	}
	if app.tab != pageManual || !tt.HasText("设备地址") {
		t.Fatalf("手动连接入口不可用: %q", tt.Texts())
	}
}

func TestTransferCanBeCancelled(t *testing.T) {
	app := New()
	app.busy = true
	called := false
	app.cancelTransfer = func() { called = true }
	tt := ui.NewTester(app.view, 690, 520)
	if err := tt.Click("取消传输"); err != nil {
		t.Fatal(err)
	}
	if !called || app.status != "正在取消传输" {
		t.Fatal("取消操作未触发")
	}
}

func TestMoreMenuOpensLinkDownload(t *testing.T) {
	app := New()
	tt := ui.NewTester(app.view, 690, 520)
	if err := tt.Click("更多操作"); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("从链接下载"); err != nil {
		t.Fatal(err)
	}
	if app.tab != pageLink || !tt.HasText("分享链接") {
		t.Fatalf("链接下载入口未打开: %q", tt.Texts())
	}
}
