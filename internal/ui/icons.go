package ui

import framework "github.com/egoist/mygo/ui"

// 图标取自 Lucide，保留统一的描边比例以适配原生工具栏。
func lucide(shapes string) *framework.SVG {
	return framework.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">` + shapes + `</svg>`))
}

var (
	clockIcon    = lucide(`<circle cx="12" cy="12" r="10"/><path d="M12 6v6h4"/>`)
	settingsIcon = lucide(`<path d="M14 17H5"/><path d="M19 7h-9"/><circle cx="17" cy="17" r="3"/><circle cx="7" cy="7" r="3"/>`)
	moreIcon     = lucide(`<circle cx="12" cy="12" r="1"/><circle cx="19" cy="12" r="1"/><circle cx="5" cy="12" r="1"/>`)
	backIcon     = lucide(`<path d="m12 19-7-7 7-7"/><path d="M19 12H5"/>`)
	monitorIcon  = lucide(`<rect width="20" height="14" x="2" y="3" rx="2"/><line x1="8" x2="16" y1="21" y2="21"/><line x1="12" x2="12" y1="17" y2="21"/>`)
	closeIcon    = lucide(`<path d="M18 6 6 18"/><path d="m6 6 12 12"/>`)
)
