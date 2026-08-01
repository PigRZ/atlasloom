// 快照处理器提供列表、下载、预检与仅 owner 可执行的确认恢复。
package httpapi

import (
	"net/http"

	"my-travel/server/internal/repository"
)

func (s *Server) snapshotRoute(w http.ResponseWriter, r *http.Request, tripID, role, actorMemberID string, tail []string) {
	if role != "owner" {
		problem(w, http.StatusForbidden, "owner-required", "只有旅行负责人可以查看或恢复快照。", nil)
		return
	}
	if r.Method == http.MethodGet && len(tail) == 0 {
		snapshots, err := s.repo.ListSnapshots(r.Context(), tripID)
		if err != nil {
			internal(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"snapshots": snapshots})
		return
	}
	if len(tail) == 2 && tail[1] == "restore-preview" && r.Method == http.MethodGet {
		preview, err := s.repo.PreviewRestore(r.Context(), tripID, tail[0])
		if err != nil {
			respondRepo(w, err)
			return
		}
		writeJSON(w, http.StatusOK, preview)
		return
	}
	if len(tail) == 2 && tail[1] == "restore" && r.Method == http.MethodPost {
		var input struct {
			ConfirmSnapshotID string `json:"confirmSnapshotId"`
			AllowDelete       *bool  `json:"allowDelete"`
		}
		if !decode(w, r, &input) {
			return
		}
		if input.ConfirmSnapshotID != tail[0] {
			problem(w, http.StatusBadRequest, "restore-confirmation-required", "确认的快照必须与待恢复快照一致。", nil)
			return
		}
		if input.AllowDelete == nil {
			problem(w, http.StatusBadRequest, "delete-confirmation-required", "请根据恢复预览明确是否允许删除资料或成员访问权。", nil)
			return
		}
		baseRevision, ok := ifMatch(w, r)
		if !ok {
			return
		}
		before, after, restored, err := s.service.RestoreSnapshot(
			r.Context(), tripID, tail[0], baseRevision, *input.AllowDelete, actorMemberID,
		)
		if err != nil {
			respondRepo(w, err)
			return
		}
		response := map[string]any{"restored": restored}
		if restored {
			response["message"] = "快照已恢复"
			response["before"] = snapshotSummary(before)
			response["after"] = snapshotSummary(after)
		} else {
			response["message"] = repository.SnapshotNoChangesMessage
		}
		writeJSON(w, http.StatusOK, response)
		return
	}
	if len(tail) == 1 && r.Method == http.MethodGet {
		snapshot, err := s.repo.GetSnapshot(r.Context(), tripID, tail[0])
		if err != nil {
			respondRepo(w, err)
			return
		}
		if err := repository.VerifyStoredSnapshot(snapshot); err != nil {
			internal(w, err)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Content-Disposition", "attachment; filename=\""+snapshot.ID+".json\"")
		_, _ = w.Write(snapshot.Payload)
		return
	}
	problem(w, http.StatusNotFound, "not-found", "没有找到该快照接口。", nil)
}
