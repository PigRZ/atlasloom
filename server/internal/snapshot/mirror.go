// Mirror 在 MongoDB 提交后生成文件镜像；失败只标记状态，不回滚数据真相。
package snapshot

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func Mirror(root, tripID, snapshotID string, payload []byte) error {
	target, err := Path(root, tripID, snapshotID)
	if err != nil {
		return err
	}
	directory := filepath.Dir(target)
	if err := os.MkdirAll(directory, 0700); err != nil {
		return err
	}
	temporary := target + ".tmp"
	file, err := os.OpenFile(temporary, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		return err
	}
	if _, err := file.Write(payload); err != nil {
		file.Close()
		os.Remove(temporary)
		return err
	}
	if err := file.Sync(); err != nil {
		file.Close()
		os.Remove(temporary)
		return err
	}
	if err := file.Close(); err != nil {
		os.Remove(temporary)
		return err
	}
	if err := os.Rename(temporary, target); err != nil {
		os.Remove(temporary)
		return err
	}
	return nil
}

func Path(root, tripID, snapshotID string) (string, error) {
	if !safeComponent(tripID) || !safeComponent(snapshotID) {
		return "", fmt.Errorf("trip and snapshot ids must be safe path components")
	}
	rootPath, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	target := filepath.Join(rootPath, tripID, snapshotID+".json")
	relative, err := filepath.Rel(rootPath, target)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("snapshot path escapes configured root")
	}
	return target, nil
}

func safeComponent(value string) bool {
	return value != "" && value != "." && value != ".." &&
		!filepath.IsAbs(value) && filepath.Base(value) == value &&
		!strings.ContainsAny(value, `/\`)
}
