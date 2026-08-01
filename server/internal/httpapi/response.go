// 本文件统一 JSON、Problem Details、请求 ID、幂等键和并发修订前置条件解析。
package httpapi

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"
	"unicode"

	"my-travel/server/internal/model"
	"my-travel/server/internal/repository"
)

func decode(w http.ResponseWriter, r *http.Request, target any) bool {
	defer r.Body.Close()
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		problem(w, http.StatusBadRequest, "invalid-json", "请求正文不是有效的 JSON。", nil)
		return false
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		problem(w, http.StatusBadRequest, "invalid-json", "请求正文只能包含一个 JSON 对象。", nil)
		return false
	}
	return true
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		slog.Error(
			"JSON response encoding failed",
			"request_id", w.Header().Get("X-Request-Id"),
			"error", err,
		)
	}
}

func problem(w http.ResponseWriter, status int, code, detail string, fields map[string]string) {
	w.Header().Set("Content-Type", "application/problem+json; charset=utf-8")
	w.WriteHeader(status)
	payload := map[string]any{
		"type":      "https://my-travel.local/problems/" + code,
		"title":     problemTitle(status),
		"status":    status,
		"detail":    detail,
		"code":      code,
		"requestId": w.Header().Get("X-Request-Id"),
	}
	if len(fields) > 0 {
		payload["fieldErrors"] = fields
	}
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		slog.Error(
			"problem response encoding failed",
			"request_id", w.Header().Get("X-Request-Id"),
			"error", err,
		)
	}
}

func problemTitle(status int) string {
	switch status {
	case http.StatusBadRequest:
		return "请求有误"
	case http.StatusUnauthorized:
		return "需要登录"
	case http.StatusForbidden:
		return "没有权限"
	case http.StatusNotFound:
		return "未找到"
	case http.StatusConflict:
		return "操作有冲突"
	case http.StatusPreconditionFailed:
		return "数据已变化"
	case http.StatusPreconditionRequired:
		return "缺少并发基准"
	case http.StatusMethodNotAllowed:
		return "不支持该操作"
	case http.StatusServiceUnavailable, http.StatusBadGateway:
		return "服务暂不可用"
	default:
		return "请求未完成"
	}
}

func internal(w http.ResponseWriter, err error) {
	slog.Error(
		"request failed internally",
		"request_id", w.Header().Get("X-Request-Id"),
		"error", err,
	)
	problem(w, http.StatusInternalServerError, "internal-error", "请求暂时无法完成，请稍后重试。", nil)
}

func respondRepo(w http.ResponseWriter, err error) {
	if errors.Is(err, repository.ErrRepositoryUnavailable) {
		internal(w, err)
		return
	}
	if errors.Is(err, repository.ErrNotFound) {
		problem(
			w,
			http.StatusNotFound,
			"not-found",
			"要操作的资料不存在，可能已被删除；请刷新页面后重试。",
			nil,
		)
		return
	}
	if errors.Is(err, repository.ErrForbidden) {
		problem(w, http.StatusForbidden, "permission-changed", "当前账号或旅行权限已变化，请刷新后重试。", nil)
		return
	}
	if errors.Is(err, repository.ErrConflict) {
		problem(w, http.StatusPreconditionFailed, "version-conflict", "资料已被其他操作更新，请刷新后重试。", nil)
		return
	}
	if errors.Is(err, repository.ErrReferenced) {
		detail := strings.TrimSuffix(
			strings.TrimSpace(err.Error()),
			": "+repository.ErrReferenced.Error(),
		)
		problem(w, http.StatusConflict, "resource-referenced", detail, nil)
		return
	}
	if errors.Is(err, repository.ErrIdempotencyConflict) {
		problem(w, http.StatusConflict, "idempotency-conflict", "本次操作标识已用于另一项请求，请重新提交。", nil)
		return
	}
	slog.Warn(
		"request rejected by repository",
		"request_id", w.Header().Get("X-Request-Id"),
		"error", err,
	)
	problem(w, http.StatusBadRequest, "invalid-request", visibleError(err), nil)
}

func visibleError(err error) string {
	message := strings.TrimSpace(err.Error())
	for _, suffix := range []string{
		repository.ErrSnapshotIdentity.Error(),
		repository.ErrSnapshotValidation.Error(),
	} {
		message = strings.TrimSuffix(message, ": "+suffix)
	}
	for _, character := range message {
		if unicode.Is(unicode.Han, character) {
			return message
		}
	}
	lower := strings.ToLower(message)
	for _, translation := range []struct {
		fragment string
		message  string
	}{
		{"data is required", "请填写完整资料后再提交。"},
		{"record id must be", "资料标识格式不正确，请刷新页面后重试。"},
		{"recordkey must be", "资料的稳定标识格式不正确，请修改后重试。"},
		{"recordkey or position already exists", "当前旅行中已存在相同资料，请刷新后检查。"},
		{"trip id already exists", "旅行标识已被使用，请换一个标识后重试。"},
		{"trip id must be", "旅行标识只能包含小写字母、数字和连字符。"},
		{"trip title is required", "请填写旅行名称。"},
		{"startdate must be", "旅行开始日期格式不正确，请重新选择。"},
		{"enddate must be", "旅行结束日期格式不正确，请重新选择。"},
		{"enddate cannot be before startdate", "旅行结束日期不能早于开始日期。"},
		{"trip date range cannot exceed", "旅行日期范围不能超过 366 天。"},
		{"date must be yyyy-mm-dd", "执行日日期格式不正确，请重新选择。"},
		{"placeentries.placeid is required", "日程中有地点信息不完整，请刷新后重新选择地点。"},
		{"placeentries time must be hh:mm", "到达或离开时间格式不正确，请重新选择。"},
		{"weatheradcode must contain exactly 6 digits", "天气区域代码必须是 6 位数字。"},
		{"latitude and longitude must be provided together", "请同时填写经度和纬度。"},
		{"latitude is out of range", "纬度必须在 -90 到 90 之间。"},
		{"longitude is out of range", "经度必须在 -180 到 180 之间。"},
		{"dayid, fromplaceid and toplaceid are required", "请选择执行日、起点和终点。"},
		{"dayid must reference exactly one day record", "交通段必须关联 1 个有效执行日。"},
		{"segment endpoints must be different places", "交通段的起点和终点不能相同。"},
		{"invalid segment mode", "请选择支持的交通方式。"},
		{"durationminutes cannot be negative", "预计时长不能小于 0 分钟。"},
		{"distancemeters cannot be negative", "预计距离不能小于 0 米。"},
		{"amountminor must be a positive integer", "费用金额必须大于 0。"},
		{"payermemberid is required", "请选择这笔费用的付款人。"},
		{"invalid splitmode", "请选择支持的分摊方式。"},
		{"splitmode and frozen splitmemberids are required", "请选择分摊方式和参与分摊的成员。"},
		{"type is required", "请选择资料类型。"},
		{"invalid packing category", "请选择支持的物品类型。"},
		{"invalid tag category", "请选择支持的标签分类。"},
		{"invalid status", "请选择支持的状态。"},
		{"no editable trip fields", "请至少修改一项旅行信息。"},
		{"ids must not be empty", "请提供完整的排序结果。"},
		{"ids must not contain empty values", "排序结果中包含无效资料，请刷新后重试。"},
		{"ids must not contain duplicates", "排序结果中有重复资料，请刷新后重试。"},
		{"ids must be the complete group ordering", "排序结果与当前资料不一致，请刷新后重试。"},
	} {
		if strings.Contains(lower, translation.fragment) {
			return translation.message
		}
	}
	return "请求内容不符合要求，请检查后重试。"
}

func snapshotSummary(snapshot model.Snapshot) map[string]any {
	return map[string]any{
		"id":              snapshot.ID,
		"revision":        snapshot.Revision,
		"sha256":          snapshot.SHA256,
		"contentChecksum": snapshot.ContentChecksum,
		"mirrorState":     snapshot.MirrorState,
		"createdAt":       snapshot.CreatedAt,
	}
}

func ifMatch(w http.ResponseWriter, r *http.Request) (int64, bool) {
	var version int64
	if _, err := fmt.Sscan(strings.Trim(r.Header.Get("If-Match"), "\""), &version); err != nil || version < 1 {
		problem(w, http.StatusPreconditionRequired, "precondition-required", "请求必须携带当前资料修订号。", nil)
		return 0, false
	}
	return version, true
}

func (s *Server) idempotencyKey(w http.ResponseWriter, r *http.Request) (string, bool) {
	key := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if key == "" {
		problem(w, http.StatusBadRequest, "idempotency-key-required", "新增操作必须携带唯一操作标识。", nil)
		return "", false
	}
	if len(key) < 16 || len(key) > 128 {
		problem(w, http.StatusBadRequest, "invalid-idempotency-key", "唯一操作标识长度必须为 16 到 128 个字符。", nil)
		return "", false
	}
	return key, true
}

func requestHash(value any) (string, error) {
	payload, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:]), nil
}

func requestID(candidate string) string {
	if len(candidate) >= 8 && len(candidate) <= 128 {
		return candidate
	}
	bytes := make([]byte, 12)
	if _, err := rand.Read(bytes); err == nil {
		return hex.EncodeToString(bytes)
	}
	return fmt.Sprintf("request-%d", time.Now().UnixNano())
}
