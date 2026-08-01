// Server 负责依赖装配、路由分流和不暴露业务数据的健康检查。
package httpapi

import (
	"log/slog"
	"net/http"
	"strings"
	"time"

	"my-travel/server/internal/app"
	"my-travel/server/internal/repository"
	"my-travel/server/internal/service"
)

type Server struct {
	config  app.Config
	repo    *repository.Repository
	service service.Service
	weather *weatherProxy
	amap    *amapServiceProxy
	places  *placeLookupProxy
}

func New(config app.Config, repo *repository.Repository) *Server {
	return &Server{
		config:  config,
		repo:    repo,
		service: service.Service{Repo: repo, Config: config},
		weather: newWeatherProxy(config),
		amap:    newAmapServiceProxy(config),
		places:  newPlaceLookupProxy(config),
	}
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	startedAt := time.Now()
	writer := &responseStatusWriter{ResponseWriter: w}
	writer.Header().Set("X-Request-Id", requestID(r.Header.Get("X-Request-Id")))
	writer.Header().Set("Cache-Control", "no-store")
	writer.Header().Set("X-Content-Type-Options", "nosniff")
	writer.Header().Set("X-Frame-Options", "DENY")
	writer.Header().Set("Referrer-Policy", "same-origin")
	defer func() {
		status := writer.status
		if status == 0 {
			status = http.StatusOK
		}
		if r.URL.Path == "/healthz" && status < http.StatusBadRequest {
			return
		}
		slog.Info(
			"http request",
			"request_id", writer.Header().Get("X-Request-Id"),
			"method", r.Method,
			"path", r.URL.Path,
			"status", status,
			"bytes", writer.bytes,
			"duration_ms", time.Since(startedAt).Milliseconds(),
		)
	}()

	switch {
	case r.URL.Path == "/healthz":
		s.health(writer, r)
	case strings.HasPrefix(r.URL.Path, "/_AMapService/"):
		s.amapServiceRoute(writer, r)
	case strings.HasPrefix(r.URL.Path, "/auth/"), r.URL.Path == "/session",
		r.URL.Path == "/profile",
		r.URL.Path == "/trips", strings.HasPrefix(r.URL.Path, "/trips/"),
		strings.HasPrefix(r.URL.Path, "/trip-imports/"),
		r.URL.Path == "/library", strings.HasPrefix(r.URL.Path, "/library/"):
		s.api(writer, r)
	default:
		problem(writer, http.StatusNotFound, "not-found", "没有找到该接口。", nil)
	}
}

func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	if _, err := s.repo.ListTrips(r.Context()); err != nil {
		slog.Error(
			"health check could not reach MongoDB",
			"request_id", w.Header().Get("X-Request-Id"),
			"error", err,
		)
		problem(w, http.StatusServiceUnavailable, "mongo-unavailable", "数据库暂不可用。", nil)
		return
	}
	fingerprint, err := s.repo.DatabaseContractFingerprint(r.Context())
	if err != nil || fingerprint != repository.CurrentDatabaseContractFingerprint() {
		slog.Error(
			"health check found a database contract mismatch",
			"request_id", w.Header().Get("X-Request-Id"),
			"installed_fingerprint", fingerprint,
			"expected_fingerprint", repository.CurrentDatabaseContractFingerprint(),
			"error", err,
		)
		problem(w, http.StatusServiceUnavailable, "mongo-contract-mismatch", "数据库结构与当前程序不一致。", nil)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

type responseStatusWriter struct {
	http.ResponseWriter
	status int
	bytes  int
}

func (w *responseStatusWriter) WriteHeader(status int) {
	if w.status != 0 {
		return
	}
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}

func (w *responseStatusWriter) Write(payload []byte) (int, error) {
	if w.status == 0 {
		w.WriteHeader(http.StatusOK)
	}
	written, err := w.ResponseWriter.Write(payload)
	w.bytes += written
	return written, err
}

func (w *responseStatusWriter) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}
