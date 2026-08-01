// 旅行备份接口提供负责人或管理员导出，以及新旅行导入的预览、确认两阶段入口。
package httpapi

import (
	"encoding/json"
	"net/http"

	"my-travel/server/internal/service"
)

func (s *Server) exportTripBackup(
	w http.ResponseWriter,
	r *http.Request,
	tripID, role string,
	admin bool,
) {
	if role != "owner" && !admin {
		problem(w, http.StatusForbidden, "owner-required", "只有旅行负责人或管理员可以导出完整备份。", nil)
		return
	}
	backup, err := s.repo.ExportTripBackup(r.Context(), tripID)
	if err != nil {
		respondRepo(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set(
		"Content-Disposition",
		`attachment; filename="`+tripID+`-backup.json"`,
	)
	_ = json.NewEncoder(w).Encode(backup)
}

func (s *Server) tripBackupImportRoute(
	w http.ResponseWriter,
	r *http.Request,
	parts []string,
) {
	if r.Method != http.MethodPost || len(parts) != 1 {
		problem(w, http.StatusNotFound, "not-found", "没有找到该旅行导入接口。", nil)
		return
	}
	user, _, ok := s.requireSession(w, r, true)
	if !ok {
		return
	}
	switch parts[0] {
	case "preview":
		var input struct {
			Backup json.RawMessage `json:"backup"`
		}
		if !decode(w, r, &input) {
			return
		}
		if len(input.Backup) == 0 {
			problem(w, http.StatusBadRequest, "backup-required", "请选择旅行备份文件。", nil)
			return
		}
		preview, err := s.service.PreviewTripBackupImport(
			r.Context(),
			input.Backup,
		)
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
			Backup          json.RawMessage                       `json:"backup"`
			ConfirmChecksum string                                `json:"confirmChecksum"`
			MemberMappings  []service.TripBackupDependencyMapping `json:"memberMappings"`
			TagMappings     []service.TripBackupDependencyMapping `json:"tagMappings"`
		}
		if !decode(w, r, &input) {
			return
		}
		if len(input.Backup) == 0 {
			problem(w, http.StatusBadRequest, "backup-required", "请选择旅行备份文件。", nil)
			return
		}
		hash, err := requestHash(input)
		if err != nil {
			internal(w, err)
			return
		}
		result, err := s.service.ApplyTripBackupImport(
			r.Context(),
			input.Backup,
			input.ConfirmChecksum,
			input.MemberMappings,
			input.TagMappings,
			user,
			key,
			hash,
		)
		if err != nil {
			respondRepo(w, err)
			return
		}
		if result.Replayed {
			w.Header().Set("Idempotency-Replayed", "true")
		}
		response := map[string]any{
			"trip":     result.Trip,
			"snapshot": snapshotSummary(result.Snapshot),
			"replayed": result.Replayed,
		}
		if result.LibrarySnapshot != nil {
			response["librarySnapshot"] = librarySnapshotSummary(
				*result.LibrarySnapshot,
			)
		}
		writeJSON(w, http.StatusCreated, response)
	default:
		problem(w, http.StatusNotFound, "not-found", "没有找到该旅行导入接口。", nil)
	}
}
