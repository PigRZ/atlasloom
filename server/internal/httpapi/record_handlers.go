// 记录处理器把旅行资源的 CRUD 与排序请求交给事务化 service/repository。
package httpapi

import (
	"net/http"

	"my-travel/server/internal/model"
)

func (s *Server) records(
	w http.ResponseWriter,
	r *http.Request,
	tripID string,
	resource model.Resource,
	tail []string,
	actorMemberID string,
) {
	if resource == model.Days &&
		len(tail) == 2 &&
		tail[1] == "places" &&
		r.Method == http.MethodPost {
		dayVersion, ok := ifMatch(w, r)
		if !ok {
			return
		}
		key, ok := s.idempotencyKey(w, r)
		if !ok {
			return
		}
		var input struct {
			RecordKey string         `json:"recordKey"`
			Data      map[string]any `json:"data"`
		}
		if !decode(w, r, &input) {
			return
		}
		hash, err := requestHash(struct {
			DayVersion int64 `json:"dayVersion"`
			Input      any   `json:"input"`
		}{DayVersion: dayVersion, Input: input})
		if err != nil {
			internal(w, err)
			return
		}
		place, day, snapshot, replayed, err := s.service.CreatePlaceForDay(
			r.Context(),
			tripID,
			tail[0],
			dayVersion,
			model.Record{RecordKey: input.RecordKey, Data: input.Data},
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
		writeJSON(w, http.StatusCreated, map[string]any{
			"place": place, "day": day,
			"snapshot": snapshotSummary(snapshot), "replayed": replayed,
		})
		return
	}

	if len(tail) == 0 {
		if r.Method == http.MethodGet {
			rows, err := s.repo.ListRecordsForMember(
				r.Context(), tripID, resource, actorMemberID,
			)
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
			}
			if !decode(w, r, &input) {
				return
			}
			if resource == model.PackingItems {
				if _, supplied := input.Data["ownerMemberId"]; supplied {
					problem(
						w,
						http.StatusBadRequest,
						"packing-owner-managed",
						"物品所属成员由当前登录成员确定，请勿提交 ownerMemberId。",
						nil,
					)
					return
				}
			}
			hash, err := requestHash(input)
			if err != nil {
				internal(w, err)
				return
			}
			row, snapshot, replayed, err := s.service.CreateRecord(r.Context(), model.Record{
				TripID: tripID, Resource: resource, RecordKey: input.RecordKey, Data: input.Data,
			}, actorMemberID, key, hash)
			if err != nil {
				respondRepo(w, err)
				return
			}
			if replayed {
				w.Header().Set("Idempotency-Replayed", "true")
			}
			writeJSON(w, http.StatusCreated, map[string]any{
				"record": row, "snapshot": snapshotSummary(snapshot), "replayed": replayed,
			})
			return
		}
	}

	if len(tail) == 1 && tail[0] == "reorder" && r.Method == http.MethodPost {
		if resource == model.PackingItems {
			problem(w, http.StatusBadRequest, "reorder-not-supported", "个人物品不支持共享排序。", nil)
			return
		}
		baseRevision, ok := ifMatch(w, r)
		if !ok {
			return
		}
		key, ok := s.idempotencyKey(w, r)
		if !ok {
			return
		}
		var input struct {
			GroupKey string   `json:"groupKey"`
			IDs      []string `json:"ids"`
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
		snapshot, replayed, err := s.service.Reorder(
			r.Context(), tripID, resource, input.GroupKey, input.IDs, baseRevision,
			actorMemberID, key, hash,
		)
		if err != nil {
			respondRepo(w, err)
			return
		}
		if replayed {
			w.Header().Set("Idempotency-Replayed", "true")
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"snapshot": snapshotSummary(snapshot), "replayed": replayed,
		})
		return
	}

	if resource == model.Members &&
		len(tail) == 2 &&
		tail[1] == "deletion-impact" &&
		r.Method == http.MethodGet {
		impact, err := s.service.GetTripMemberDeletionImpact(
			r.Context(),
			tripID,
			tail[0],
			actorMemberID,
		)
		if err != nil {
			respondRepo(w, err)
			return
		}
		writeJSON(w, http.StatusOK, impact)
		return
	}

	if len(tail) == 1 {
		id := tail[0]
		if r.Method == http.MethodGet {
			row, err := s.repo.GetRecordForMember(
				r.Context(), tripID, resource, id, actorMemberID,
			)
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
				Data map[string]any `json:"data"`
			}
			if !decode(w, r, &input) {
				return
			}
			if resource == model.PackingItems {
				if _, supplied := input.Data["ownerMemberId"]; supplied {
					problem(
						w,
						http.StatusBadRequest,
						"packing-owner-managed",
						"物品所属成员由当前登录成员确定，请勿提交 ownerMemberId。",
						nil,
					)
					return
				}
			}
			row, snapshot, err := s.service.UpdateRecord(
				r.Context(), tripID, resource, id, version, input.Data, actorMemberID,
			)
			if err != nil {
				respondRepo(w, err)
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{
				"record": row, "snapshot": snapshotSummary(snapshot),
			})
			return
		}
		if r.Method == http.MethodDelete {
			cascade := (resource == model.Places || resource == model.Members) &&
				r.URL.Query().Get("cascade") == "true"
			snapshot, err := s.service.DeleteRecordWithOptions(
				r.Context(), tripID, resource, id, version, cascade,
				actorMemberID,
			)
			if err != nil {
				respondRepo(w, err)
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{
				"deleted": true, "snapshot": snapshotSummary(snapshot),
			})
			return
		}
	}
	problem(w, http.StatusNotFound, "not-found", "没有找到该资料接口。", nil)
}
