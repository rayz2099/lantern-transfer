package ui

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"github.com/egoist/mygo"
)

// HistoryItem 只保留本机传输元数据，避免历史记录成为文件内容的副本。
type HistoryItem struct {
	Time      time.Time `json:"time"`
	Direction string    `json:"direction"`
	Peer      string    `json:"peer"`
	Name      string    `json:"name"`
	Size      int64     `json:"size"`
	Result    string    `json:"result"`
}

func historyPath() (string, error) {
	dir, err := mygo.App.Path(mygo.PathUserData)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "history.json"), nil
}

// loadHistory 丢弃过期条目，使 90 天保留策略在每次启动时生效。
func loadHistory() ([]HistoryItem, error) {
	path, err := historyPath()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var items []HistoryItem
	if err := json.Unmarshal(data, &items); err != nil {
		return nil, err
	}
	cutoff := time.Now().AddDate(0, 0, -90)
	kept := items[:0]
	for _, item := range items {
		if !item.Time.Before(cutoff) {
			kept = append(kept, item)
		}
	}
	return kept, nil
}

// saveHistory 原子替换记录，避免异常退出留下截断的 JSON。
func saveHistory(items []HistoryItem) error {
	path, err := historyPath()
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(items, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
