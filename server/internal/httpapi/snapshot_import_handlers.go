// 快照导入处理器提供 owner 专用的两阶段预检与确认应用入口。
package httpapi

import (
	"encoding/json"
	"net/http"
)

func (s *Server) snapshotImportRoute(
	w http.ResponseWriter,
	r *http.Request,
	tripID, role, actorMemberID string,
	tail []string,
) {
	if role != "owner" {
		problem(w, http.StatusForbidden, "owner-required", "只有旅行负责人可以导入快照。", nil)
		return
	}
	if len(tail) != 1 || r.Method != http.MethodPost {
		problem(w, http.StatusNotFound, "not-found", "没有找到该快照导入接口。", nil)
		return
	}
	baseRevision, ok := ifMatch(w, r)
	if !ok {
		return
	}

	switch tail[0] {
	case "preview":
		var input struct {
			Snapshot json.RawMessage `json:"snapshot"`
		}
		if !decode(w, r, &input) {
			return
		}
		if len(input.Snapshot) == 0 {
			problem(w, http.StatusBadRequest, "snapshot-required", "请提供完整快照。", nil)
			return
		}
		preview, err := s.service.PreviewSnapshotImport(r.Context(), tripID, baseRevision, input.Snapshot)
		if err != nil {
			respondRepo(w, err)
			return
		}
		writeJSON(w, http.StatusOK, preview)
	case "apply":
		key, ok := s.idempotencyKey(w, r)
		if !ok {
			return
		}
		var input struct {
			Snapshot        json.RawMessage `json:"snapshot"`
			ConfirmChecksum string          `json:"confirmChecksum"`
			AllowDelete     bool            `json:"allowDelete"`
		}
		if !decode(w, r, &input) {
			return
		}
		hash, err := requestHash(struct {
			BaseRevision int64 `json:"baseRevision"`
			Input        any   `json:"input"`
		}{BaseRevision: baseRevision, Input: input})
		if err != nil {
			internal(w, err)
			return
		}
		before, after, applied, replayed, err := s.service.ApplySnapshotImport(
			r.Context(),
			tripID,
			baseRevision,
			input.Snapshot,
			input.ConfirmChecksum,
			input.AllowDelete,
			actorMemberID,
			key,
			hash,
		)
		if err != nil {
			respondRepo(w, err)
			return
		}
		if replayed {
			w.Header().Set("Idempotency-Replayed", "true")
		}
		response := map[string]any{"applied": applied, "replayed": replayed}
		if applied {
			response["message"] = "快照变更已应用"
			response["before"] = snapshotSummary(before)
			response["after"] = snapshotSummary(after)
		} else {
			response["message"] = "已同步，无需写入"
		}
		writeJSON(w, http.StatusOK, response)
	default:
		problem(w, http.StatusNotFound, "not-found", "没有找到该快照导入接口。", nil)
	}
}
