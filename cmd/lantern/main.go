package main

import (
	"log"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/plugins/updater/native"
	"github.com/rayz2099/lantern-transfer/internal/ui"
)

// main 注册原生更新界面，让桌面包无需系统 WebView 依赖即可更新。
func main() {
	mygo.Use(native.Plugin)
	app := ui.New()
	mygo.App.WhenReady(app.Open)
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}
