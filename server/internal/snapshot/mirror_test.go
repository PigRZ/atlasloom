// 这些测试固定镜像原子替换、权限和失败时保留 Mongo 提交的语义。
package snapshot

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMirrorUsesPrivateFilePermissions(t *testing.T) {
	dir := t.TempDir()
	if err := Mirror(dir, "trip-a", "snapshot-a", []byte(`{"revision":1}`)); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "trip-a", "snapshot-a.json")
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("permission=%#o", info.Mode().Perm())
	}
}

func TestMirrorRejectsPathTraversal(t *testing.T) {
	dir := t.TempDir()
	for _, tripID := range []string{"../escape", "/tmp/escape", `..\escape`} {
		if err := Mirror(dir, tripID, "snapshot-a", []byte(`{}`)); err == nil {
			t.Fatalf("trip id %q escaped snapshot root", tripID)
		}
	}
	if err := Mirror(dir, "trip-a", "../snapshot", []byte(`{}`)); err == nil {
		t.Fatal("snapshot id escaped snapshot root")
	}
}
