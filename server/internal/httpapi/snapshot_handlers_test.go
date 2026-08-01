package httpapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"my-travel/server/internal/repository"
)

func TestSnapshotRestoreRequiresExplicitDeleteDecision(t *testing.T) {
	request := httptest.NewRequest(
		http.MethodPost,
		"/trips/trip-a/snapshots/snapshot-a/restore",
		bytes.NewBufferString(`{"confirmSnapshotId":"snapshot-a"}`),
	)
	recorder := httptest.NewRecorder()
	(&Server{}).snapshotRoute(
		recorder,
		request,
		"trip-a",
		"owner",
		"member-a",
		[]string{"snapshot-a", "restore"},
	)
	var response map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if recorder.Code != http.StatusBadRequest ||
		response["code"] != "delete-confirmation-required" {
		t.Fatalf("status=%d body=%#v", recorder.Code, response)
	}
}

func TestRespondRepoTreatsRepositoryUnavailableAsInternalFailure(t *testing.T) {
	recorder := httptest.NewRecorder()
	respondRepo(recorder, repository.ErrRepositoryUnavailable)

	var response map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if recorder.Code != http.StatusInternalServerError ||
		response["code"] != "internal-error" {
		t.Fatalf("status=%d body=%#v", recorder.Code, response)
	}
}
