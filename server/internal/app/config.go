// Config 汇总 Go 服务的运行时环境变量，并在进程启动时校验基础边界。
package app

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	MongoURI                 string
	MongoDatabase            string
	SnapshotDir              string
	SnapshotReconcileEvery   time.Duration
	SnapshotReconcileTimeout time.Duration
	SnapshotReconcileBatch   int64
	APIAddress               string
	WebAddress               string
	CookieName               string
	CookieSecure             bool
	SessionTTL               time.Duration
	BootstrapToken           string
	ProvisionToken           string
	AppBasePath              string
	AmapWebKey               string
	AmapJSAPIKey             string
	AmapJSSecurity           string
	WeatherCacheTTL          time.Duration
}

func LoadConfig() (Config, error) {
	secure, err := strconv.ParseBool(value("COOKIE_SECURE", "true"))
	if err != nil {
		return Config{}, fmt.Errorf("COOKIE_SECURE must be true or false")
	}
	reconcileEvery, err := positiveDuration(
		"SNAPSHOT_RECONCILE_INTERVAL",
		value("SNAPSHOT_RECONCILE_INTERVAL", "1m"),
	)
	if err != nil {
		return Config{}, err
	}
	reconcileTimeout, err := positiveDuration(
		"SNAPSHOT_RECONCILE_TIMEOUT",
		value("SNAPSHOT_RECONCILE_TIMEOUT", "10s"),
	)
	if err != nil {
		return Config{}, err
	}
	reconcileBatch, err := strconv.ParseInt(
		value("SNAPSHOT_RECONCILE_BATCH", "100"),
		10,
		64,
	)
	if err != nil || reconcileBatch < 1 || reconcileBatch > 1000 {
		return Config{}, fmt.Errorf(
			"SNAPSHOT_RECONCILE_BATCH must be between 1 and 1000",
		)
	}
	config := Config{
		MongoURI:                 value("MONGODB_URI", "mongodb://localhost:27017/my_travel?replicaSet=rs0"),
		MongoDatabase:            value("MONGODB_DATABASE", "my_travel"),
		SnapshotDir:              value("SNAPSHOT_DIR", "snapshots"),
		SnapshotReconcileEvery:   reconcileEvery,
		SnapshotReconcileTimeout: reconcileTimeout,
		SnapshotReconcileBatch:   reconcileBatch,
		APIAddress:               value("API_ADDRESS", "127.0.0.1:8502"),
		WebAddress:               value("WEB_ADDRESS", "127.0.0.1:8501"),
		CookieName:               value("SESSION_COOKIE_NAME", "travel_session"),
		CookieSecure:             secure,
		SessionTTL:               30 * 24 * time.Hour,
		BootstrapToken:           strings.TrimSpace(os.Getenv("BOOTSTRAP_TOKEN")),
		ProvisionToken:           strings.TrimSpace(os.Getenv("ADMIN_PROVISION_TOKEN")),
		AppBasePath:              strings.Trim(strings.TrimSpace(value("APP_BASE_PATH", "travel")), "/"),
		AmapWebKey:               strings.TrimSpace(os.Getenv("AMAP_WEBSERVICE_KEY")),
		AmapJSAPIKey:             strings.TrimSpace(os.Getenv("AMAP_JSAPI_KEY")),
		AmapJSSecurity:           strings.TrimSpace(os.Getenv("AMAP_JSAPI_SECURITY_CODE")),
		WeatherCacheTTL:          30 * time.Minute,
	}
	if config.AppBasePath == "" {
		return Config{}, fmt.Errorf("APP_BASE_PATH cannot be empty")
	}
	if !filepath.IsAbs(config.SnapshotDir) {
		config.SnapshotDir = filepath.Clean(config.SnapshotDir)
	}
	return config, nil
}

func positiveDuration(key, raw string) (time.Duration, error) {
	duration, err := time.ParseDuration(raw)
	if err != nil || duration <= 0 {
		return 0, fmt.Errorf("%s must be a positive duration", key)
	}
	return duration, nil
}

func value(key, fallback string) string {
	if candidate := strings.TrimSpace(os.Getenv(key)); candidate != "" {
		return candidate
	}
	return fallback
}
