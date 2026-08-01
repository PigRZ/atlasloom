// API 路由先建立会话与 Trip 授权边界，再分派到具体资源处理器。
package httpapi

import (
	"net/http"
	"strings"

	"my-travel/server/internal/model"
)

func (s *Server) api(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/")
	switch path {
	case "auth/bootstrap":
		if r.Method == http.MethodPost {
			s.bootstrap(w, r)
			return
		}
	case "auth/users":
		if r.Method == http.MethodPost {
			s.provisionUser(w, r)
			return
		}
	case "auth/login":
		if r.Method == http.MethodPost {
			s.login(w, r)
			return
		}
	case "auth/logout":
		if r.Method == http.MethodPost {
			s.logout(w, r)
			return
		}
	case "session":
		if r.Method == http.MethodGet {
			s.session(w, r)
			return
		}
	case "profile":
		s.profile(w, r)
		return
	}

	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) >= 1 && parts[0] == "library" {
		s.libraryRoute(w, r, parts[1:])
		return
	}
	if len(parts) >= 1 && parts[0] == "trip-imports" {
		s.tripBackupImportRoute(w, r, parts[1:])
		return
	}
	if len(parts) == 1 && parts[0] == "trips" {
		if r.Method == http.MethodGet {
			s.listTrips(w, r)
			return
		}
		if r.Method == http.MethodPost {
			s.createTrip(w, r)
			return
		}
	}
	if len(parts) >= 2 && parts[0] == "trips" {
		s.tripRoute(w, r, parts)
		return
	}
	problem(w, http.StatusNotFound, "not-found", "没有找到该接口。", nil)
}

func (s *Server) tripRoute(w http.ResponseWriter, r *http.Request, parts []string) {
	tripID := parts[1]
	write := r.Method != http.MethodGet && r.Method != http.MethodHead
	user, _, ok := s.requireSession(w, r, write)
	if !ok {
		return
	}
	adminManagement := isAdminTripManagementRoute(user.Admin, r.Method, parts)
	role, err := s.repo.RoleFor(r.Context(), tripID, user.ID)
	if err != nil && !adminManagement {
		problem(w, http.StatusForbidden, "trip-forbidden", "你没有查看这次旅行的权限。", nil)
		return
	}
	if write && role == "viewer" && !adminManagement {
		problem(w, http.StatusForbidden, "trip-read-only", "你目前只能查看这次旅行。", nil)
		return
	}
	if _, err := s.repo.GetTrip(r.Context(), tripID); err != nil {
		respondRepo(w, err)
		return
	}

	if len(parts) == 2 {
		if r.Method == http.MethodGet {
			trip, err := s.repo.GetTrip(r.Context(), tripID)
			if err != nil {
				respondRepo(w, err)
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{"trip": trip})
			return
		}
		if r.Method == http.MethodPatch {
			version, ok := ifMatch(w, r)
			if !ok {
				return
			}
			var patch map[string]any
			if !decode(w, r, &patch) {
				return
			}
			trip, snapshot, err := s.service.UpdateTrip(
				r.Context(), tripID, version, patch, user.ID,
			)
			if err != nil {
				respondRepo(w, err)
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{"trip": trip, "snapshot": snapshotSummary(snapshot)})
			return
		}
		if r.Method == http.MethodDelete {
			if role != "owner" && !user.Admin {
				problem(w, http.StatusForbidden, "owner-required", "只有旅行负责人或管理员可以删除旅行。", nil)
				return
			}
			version, ok := ifMatch(w, r)
			if !ok {
				return
			}
			snapshot, err := s.service.DeleteTrip(r.Context(), tripID, version, user.ID)
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
	if len(parts) == 3 && parts[2] == "overview" && r.Method == http.MethodGet {
		s.overview(w, r, tripID, user.ID)
		return
	}
	if len(parts) == 3 && parts[2] == "backup" && r.Method == http.MethodGet {
		s.exportTripBackup(w, r, tripID, role, user.Admin)
		return
	}
	if len(parts) == 3 && parts[2] == "weather" && r.Method == http.MethodGet {
		s.weatherForecast(w, r)
		return
	}
	if len(parts) == 3 && parts[2] == "map-config" && r.Method == http.MethodGet {
		s.mapConfig(w)
		return
	}
	if len(parts) == 3 && parts[2] == "route" && r.Method == http.MethodGet {
		s.routePlan(w, r)
		return
	}
	if len(parts) == 3 && parts[2] == "place-search" && r.Method == http.MethodGet {
		s.placeSearch(w, r)
		return
	}
	if len(parts) == 3 && parts[2] == "reverse-geocode" && r.Method == http.MethodGet {
		s.reverseGeocode(w, r)
		return
	}
	if len(parts) >= 3 && parts[2] == "snapshots" {
		s.snapshotRoute(w, r, tripID, role, user.ID, parts[3:])
		return
	}
	if len(parts) >= 3 && parts[2] == "snapshot-imports" {
		s.snapshotImportRoute(w, r, tripID, role, user.ID, parts[3:])
		return
	}
	if len(parts) == 3 && parts[2] == "audit-logs" && r.Method == http.MethodGet {
		rows, err := s.repo.ListAuditLogs(r.Context(), tripID, user.ID, 200)
		if err != nil {
			respondRepo(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"records": rows})
		return
	}
	if len(parts) >= 3 {
		resource, err := model.ParseResource(parts[2])
		if err != nil {
			problem(w, http.StatusNotFound, "unknown-resource", "未知的旅行资料类型。", nil)
			return
		}
		s.records(w, r, tripID, resource, parts[3:], user.ID)
		return
	}
	problem(w, http.StatusNotFound, "not-found", "没有找到该接口。", nil)
}

func isAdminTripManagementRoute(admin bool, method string, parts []string) bool {
	if !admin {
		return false
	}
	if len(parts) == 2 {
		return method == http.MethodDelete
	}
	return len(parts) == 3 &&
		parts[2] == "backup" &&
		method == http.MethodGet
}
