package ui

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"

	"github.com/egoist/mygo"
)

type settings struct {
	DownloadDir string `json:"downloadDir"`
}

func settingsPath() (string, error) {
	dir, err := mygo.App.Path(mygo.PathUserData)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "settings.json"), nil
}

// loadDownloadDir 拒绝失效的已保存目录，防止接收文件落到意料之外的位置。
func loadDownloadDir() (string, error) {
	path, err := settingsPath()
	if err != nil {
		return "", err
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	var cfg settings
	if err := json.Unmarshal(data, &cfg); err != nil {
		return "", err
	}
	if cfg.DownloadDir == "" {
		return "", errors.New("目录为空")
	}
	info, err := os.Stat(cfg.DownloadDir)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", errors.New("保存位置不是目录")
	}
	return cfg.DownloadDir, nil
}

// saveDownloadDir 原子写入设置，确保下一次启动使用同一接收目录。
func saveDownloadDir(dir string) error {
	path, err := settingsPath()
	if err != nil {
		return err
	}
	data, err := json.Marshal(settings{DownloadDir: dir})
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
