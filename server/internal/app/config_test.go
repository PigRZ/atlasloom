// 配置测试保证高德公开 Key 与服务端安全密钥被分别读取且正确裁剪。
package app

import (
	"testing"
	"time"
)

func TestLoadConfigReadsAmapJSAPIWithoutExposingDefaults(t *testing.T) {
	t.Setenv("AMAP_JSAPI_KEY", " public-web-key ")
	t.Setenv("AMAP_JSAPI_SECURITY_CODE", " server-security-code ")

	config, err := LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if config.AmapJSAPIKey != "public-web-key" {
		t.Fatalf("AmapJSAPIKey=%q", config.AmapJSAPIKey)
	}
	if config.AmapJSSecurity != "server-security-code" {
		t.Fatalf("AmapJSSecurity=%q", config.AmapJSSecurity)
	}
}

func TestLoadConfigUsesOneCurrentDatabaseAndSnapshotDirectory(t *testing.T) {
	t.Setenv("MONGODB_URI", "")
	t.Setenv("MONGODB_DATABASE", "")
	t.Setenv("SNAPSHOT_DIR", "")
	config, err := LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if config.MongoDatabase != "my_travel" {
		t.Fatalf("MongoDatabase=%q", config.MongoDatabase)
	}
	if config.SnapshotDir != "snapshots" {
		t.Fatalf("SnapshotDir=%q", config.SnapshotDir)
	}
	if config.SnapshotReconcileEvery != time.Minute ||
		config.SnapshotReconcileTimeout != 10*time.Second ||
		config.SnapshotReconcileBatch != 100 {
		t.Fatalf("unexpected snapshot reconcile config: %#v", config)
	}
}

func TestLoadConfigValidatesSnapshotReconcileSettings(t *testing.T) {
	t.Setenv("SNAPSHOT_RECONCILE_INTERVAL", "0s")
	if _, err := LoadConfig(); err == nil {
		t.Fatal("zero reconcile interval was accepted")
	}
	t.Setenv("SNAPSHOT_RECONCILE_INTERVAL", "2m")
	t.Setenv("SNAPSHOT_RECONCILE_TIMEOUT", "bad")
	if _, err := LoadConfig(); err == nil {
		t.Fatal("invalid reconcile timeout was accepted")
	}
	t.Setenv("SNAPSHOT_RECONCILE_TIMEOUT", "5s")
	t.Setenv("SNAPSHOT_RECONCILE_BATCH", "1001")
	if _, err := LoadConfig(); err == nil {
		t.Fatal("oversized reconcile batch was accepted")
	}
}
