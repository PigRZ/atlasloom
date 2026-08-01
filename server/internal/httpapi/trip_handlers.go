// Trip 处理器负责可见旅行列表、聚合根创建和旅行资源计数概览。
package httpapi

import (
	"net/http"

	"my-travel/server/internal/model"
)

type visibleTrip struct {
	model.Trip
	Role string `json:"role,omitempty"`
}

type tripCreateInput struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	Summary   string `json:"summary"`
	StartDate string `json:"startDate"`
	EndDate   string `json:"endDate"`
	Timezone  string `json:"timezone"`
	Currency  string `json:"currency"`
}

func (input tripCreateInput) trip() model.Trip {
	return model.Trip{
		ID:        input.ID,
		Title:     input.Title,
		Summary:   input.Summary,
		StartDate: input.StartDate,
		EndDate:   input.EndDate,
		Timezone:  input.Timezone,
		Currency:  input.Currency,
	}
}

func (s *Server) listTrips(w http.ResponseWriter, r *http.Request) {
	user, _, ok := s.requireSession(w, r, false)
	if !ok {
		return
	}
	trips, err := s.repo.ListTrips(r.Context())
	if err != nil {
		internal(w, err)
		return
	}
	visible := make([]visibleTrip, 0, len(trips))
	for _, trip := range trips {
		if role, err := s.repo.RoleFor(r.Context(), trip.ID, user.ID); err == nil {
			visible = append(visible, visibleTrip{Trip: trip, Role: role})
		} else if user.Admin {
			visible = append(visible, visibleTrip{Trip: trip})
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"trips": visible})
}

func (s *Server) createTrip(w http.ResponseWriter, r *http.Request) {
	user, _, ok := s.requireSession(w, r, true)
	if !ok {
		return
	}
	var input tripCreateInput
	if !decode(w, r, &input) {
		return
	}
	key, ok := s.idempotencyKey(w, r)
	if !ok {
		return
	}
	hash, err := requestHash(input)
	if err != nil {
		internal(w, err)
		return
	}
	trip := input.trip()
	created, snapshot, replayed, err := s.service.CreateTrip(r.Context(), user.ID, trip, key, hash)
	if err != nil {
		respondRepo(w, err)
		return
	}
	if replayed {
		w.Header().Set("Idempotency-Replayed", "true")
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"trip": created, "snapshot": snapshotSummary(snapshot), "replayed": replayed,
	})
}

func (s *Server) overview(
	w http.ResponseWriter,
	r *http.Request,
	tripID, actorMemberID string,
) {
	result := map[string]any{}
	for _, resource := range model.Resources() {
		rows, err := s.repo.ListRecordsForMember(
			r.Context(),
			tripID,
			resource,
			actorMemberID,
		)
		if err != nil {
			respondRepo(w, err)
			return
		}
		result[string(resource)] = len(rows)
	}
	writeJSON(w, http.StatusOK, map[string]any{"counts": result})
}
