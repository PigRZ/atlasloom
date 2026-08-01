// 个人资料接口只允许当前会话成员修改自己的资料与密码。
package httpapi

import (
	"net/http"

	"my-travel/server/internal/model"
	"my-travel/server/internal/service"
)

func (s *Server) profile(w http.ResponseWriter, r *http.Request) {
	write := r.Method != http.MethodGet && r.Method != http.MethodHead
	user, _, ok := s.requireSession(w, r, write)
	if !ok {
		return
	}
	if r.Method == http.MethodGet {
		row, err := s.repo.GetLibraryRecord(
			r.Context(),
			model.LibraryMembers,
			user.ID,
		)
		if err != nil {
			respondRepo(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"record": row, "user": user})
		return
	}
	if r.Method != http.MethodPatch {
		problem(w, http.StatusNotFound, "not-found", "没有找到个人资料接口。", nil)
		return
	}
	version, ok := ifMatch(w, r)
	if !ok {
		return
	}
	var input service.ProfileUpdateInput
	if !decode(w, r, &input) {
		return
	}
	row, identity, snapshot, err := s.service.UpdateOwnProfile(
		r.Context(),
		user.ID,
		version,
		input,
	)
	if err != nil {
		respondRepo(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"record":   row,
		"user":     identity,
		"snapshot": librarySnapshotSummary(snapshot),
	})
}
