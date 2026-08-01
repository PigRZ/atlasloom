// 基础库接口提供全局成员资料与标签的鉴权 CRUD 和完整导出。
package httpapi

import (
	"net/http"

	"my-travel/server/internal/model"
)

func (s *Server) libraryRoute(w http.ResponseWriter, r *http.Request, parts []string) {
	write := r.Method != http.MethodGet && r.Method != http.MethodHead
	user, _, ok := s.requireSession(w, r, write)
	if !ok {
		return
	}
	if write && !user.Admin {
		problem(w, http.StatusForbidden, "library-admin-required", "只有管理员可以修改基础资料", nil)
		return
	}
	if len(parts) == 0 && r.Method == http.MethodGet {
		state, err := s.repo.GetLibraryState(r.Context())
		if err != nil {
			respondRepo(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"library": state})
		return
	}
	if len(parts) == 1 && parts[0] == "snapshot" && r.Method == http.MethodGet {
		payload, err := s.repo.ExportLibrarySnapshot(r.Context(), "manual-pull")
		if err != nil {
			respondRepo(w, err)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Content-Disposition", `attachment; filename="library.json"`)
		_, _ = w.Write(payload)
		return
	}
	if len(parts) < 1 {
		problem(w, http.StatusNotFound, "not-found", "未找到基础资料接口", nil)
		return
	}
	resource, err := model.ParseLibraryResource(parts[0])
	if err != nil {
		problem(w, http.StatusNotFound, "unknown-library-resource", "未知的基础资料类型", nil)
		return
	}
	if resource == model.LibraryTags &&
		len(parts) == 2 &&
		parts[1] == "presets" &&
		r.Method == http.MethodPost {
		key, ok := s.idempotencyKey(w, r)
		if !ok {
			return
		}
		hash, err := requestHash(struct{}{})
		if err != nil {
			internal(w, err)
			return
		}
		rows, item, replayed, err := s.service.AddTravelTypeTagPresets(
			r.Context(),
			user.ID,
			key,
			hash,
		)
		if err != nil {
			respondRepo(w, err)
			return
		}
		payload := map[string]any{
			"records": rows, "replayed": replayed,
		}
		if item != nil {
			payload["snapshot"] = librarySnapshotSummary(*item)
		}
		writeJSON(w, http.StatusOK, payload)
		return
	}
	if len(parts) == 1 {
		if r.Method == http.MethodGet {
			rows, err := s.repo.ListLibraryRecords(r.Context(), resource)
			if err != nil {
				respondRepo(w, err)
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{"records": rows})
			return
		}
		if r.Method == http.MethodPost {
			key, ok := s.idempotencyKey(w, r)
			if !ok {
				return
			}
			var input struct {
				RecordKey string         `json:"recordKey"`
				Data      map[string]any `json:"data"`
				Password  string         `json:"password,omitempty"`
			}
			if !decode(w, r, &input) {
				return
			}
			hash, err := requestHash(input)
			if err != nil {
				internal(w, err)
				return
			}
			row, item, replayed, err := s.service.CreateLibraryRecord(
				r.Context(),
				model.LibraryRecord{
					Resource:  resource,
					RecordKey: input.RecordKey,
					Data:      input.Data,
				},
				input.Password,
				user.ID,
				key,
				hash,
			)
			if err != nil {
				respondRepo(w, err)
				return
			}
			writeJSON(w, http.StatusCreated, map[string]any{
				"record":   row,
				"snapshot": librarySnapshotSummary(item),
				"replayed": replayed,
			})
			return
		}
	}
	if len(parts) == 2 {
		id := parts[1]
		if r.Method == http.MethodGet {
			row, err := s.repo.GetLibraryRecord(r.Context(), resource, id)
			if err != nil {
				respondRepo(w, err)
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{"record": row})
			return
		}
		version, ok := ifMatch(w, r)
		if !ok {
			return
		}
		if r.Method == http.MethodPatch {
			var input struct {
				Data     map[string]any `json:"data"`
				Password string         `json:"password,omitempty"`
			}
			if !decode(w, r, &input) {
				return
			}
			row, item, err := s.service.UpdateLibraryRecord(
				r.Context(),
				resource,
				id,
				version,
				input.Data,
				input.Password,
				user.ID,
			)
			if err != nil {
				respondRepo(w, err)
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{
				"record":   row,
				"snapshot": librarySnapshotSummary(item),
			})
			return
		}
		if r.Method == http.MethodDelete {
			item, err := s.service.DeleteLibraryRecord(
				r.Context(),
				resource,
				id,
				version,
				r.URL.Query().Get("replaceTagWithOther") == "true",
				r.URL.Query().Get("deleteLoginCredential") == "true",
				user.ID,
			)
			if err != nil {
				respondRepo(w, err)
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{
				"deleted":  true,
				"snapshot": librarySnapshotSummary(item),
			})
			return
		}
	}
	if len(parts) == 3 && parts[2] == "deletion-impact" &&
		r.Method == http.MethodGet {
		if !user.Admin {
			problem(w, http.StatusForbidden, "library-admin-required", "只有管理员可以检查删除影响", nil)
			return
		}
		impact, err := s.repo.GetLibraryDeletionImpact(r.Context(), resource, parts[1])
		if err != nil {
			respondRepo(w, err)
			return
		}
		writeJSON(w, http.StatusOK, impact)
		return
	}
	problem(w, http.StatusNotFound, "not-found", "未找到基础资料接口", nil)
}

func librarySnapshotSummary(item model.LibrarySnapshot) map[string]any {
	return map[string]any{
		"id":              item.ID,
		"revision":        item.Revision,
		"sha256":          item.SHA256,
		"contentChecksum": item.ContentChecksum,
		"mirrorState":     item.MirrorState,
		"createdAt":       item.CreatedAt,
	}
}
