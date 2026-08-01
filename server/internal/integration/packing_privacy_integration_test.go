// 双成员集成测试验证物品隐私不会从概览、审计、排序和快照入口旁路。
package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"my-travel/server/internal/app"
	"my-travel/server/internal/httpapi"
	"my-travel/server/internal/repository"
)

func TestMongoIntegrationPackingPrivacyBoundaries(t *testing.T) {
	config, cleanup := mongoIntegrationConfig(t)
	defer cleanup()
	config.ProvisionToken = "packing-privacy-provision-token"

	repo, server := packingPrivacyServer(t, config)
	defer repo.Close(context.Background())
	defer server.Close()

	ownerClient := &apiClient{baseURL: server.URL, client: server.Client()}
	status, _, bootstrap := ownerClient.request(
		t,
		http.MethodPost,
		"/auth/bootstrap",
		map[string]any{
			"token":       config.BootstrapToken,
			"displayName": "旅行负责人",
			"mobile":      "13800138000",
			"password":    "packing-owner-password",
		},
		nil,
	)
	if status != http.StatusCreated {
		t.Fatalf("bootstrap status=%d body=%#v", status, bootstrap)
	}
	ownerID := privacyResponseUserID(t, bootstrap)
	loginPrivacyClient(t, ownerClient, "13800138000", "packing-owner-password")

	status, _, createdTrip := ownerClient.request(
		t,
		http.MethodPost,
		"/trips",
		map[string]any{
			"id": "packing-privacy-trip", "title": "物品隐私测试",
			"startDate": "2026-09-01", "endDate": "2026-09-02",
		},
		map[string]string{"Idempotency-Key": "packing-privacy-trip-create"},
	)
	if status != http.StatusCreated {
		t.Fatalf("create trip status=%d body=%#v", status, createdTrip)
	}

	status, _, provisioned := ownerClient.request(
		t,
		http.MethodPost,
		"/auth/users",
		map[string]any{
			"token":       config.ProvisionToken,
			"displayName": "同行成员",
			"mobile":      "13900139000",
			"password":    "packing-member-password",
		},
		nil,
	)
	if status != http.StatusCreated {
		t.Fatalf("provision member status=%d body=%#v", status, provisioned)
	}
	memberID := privacyResponseUserID(t, provisioned)

	createTripMemberForPrivacyTest(t, ownerClient, "owner-selection", ownerID, "owner")
	createTripMemberForPrivacyTest(t, ownerClient, "member-selection", memberID, "editor")

	memberClient := &apiClient{baseURL: server.URL, client: server.Client()}
	loginPrivacyClient(t, memberClient, "13900139000", "packing-member-password")

	const ownerSecret = "OWNER-PRIVATE-PASSPORT"
	const memberSecret = "MEMBER-PRIVATE-MEDICINE"
	ownerPacking := createPackingForPrivacyTest(
		t, ownerClient, "owner-private-item", ownerSecret, "", ownerID,
	)
	memberPacking := createPackingForPrivacyTest(
		t, memberClient, "member-private-item", memberSecret, ownerID, memberID,
	)

	assertPackingOverviewCount(t, ownerClient, 1)
	assertPackingOverviewCount(t, memberClient, 1)
	assertPackingListAndOwnDetail(
		t, ownerClient, ownerPacking, ownerSecret, memberSecret,
	)
	assertPackingListAndOwnDetail(
		t, memberClient, memberPacking, memberSecret, ownerSecret,
	)
	ownerPacking = assertPackingOwnerCannotTransfer(
		t, ownerClient, ownerPacking, ownerID, memberID,
	)
	assertForeignPackingCRUDHasNoSideEffects(
		t, repo, ownerClient, memberPacking, ownerID, memberID,
	)
	assertForeignPackingCRUDHasNoSideEffects(
		t, repo, memberClient, ownerPacking, memberID, ownerID,
	)
	assertAuditPrivacy(t, ownerClient, ownerSecret, memberSecret)
	assertAuditPrivacy(t, memberClient, memberSecret, ownerSecret)
	assertCrossMemberPackingIdempotencyIsolation(t, ownerClient, memberClient)
	assertPackingReorderRejected(t, ownerClient)
	assertSnapshotOwnerBoundary(t, ownerClient, memberClient)
}

func assertCrossMemberPackingIdempotencyIsolation(
	t *testing.T,
	ownerClient, memberClient *apiClient,
) {
	t.Helper()
	const secret = "OWNER-IDEMPOTENCY-PRIVATE"
	body := map[string]any{
		"recordKey": "cross-member-idempotency-private",
		"data": map[string]any{
			"item": secret, "category": "document",
			"quantity": 1, "packed": false,
		},
	}
	headers := map[string]string{
		"Idempotency-Key": "cross-member-packing-idempotency",
	}
	status, _, response := ownerClient.request(
		t,
		http.MethodPost,
		"/trips/packing-privacy-trip/packing-items",
		body,
		headers,
	)
	if status != http.StatusCreated {
		t.Fatalf("owner idempotent create status=%d body=%#v", status, response)
	}

	status, _, response = memberClient.request(
		t,
		http.MethodPost,
		"/trips/packing-privacy-trip/packing-items",
		body,
		headers,
	)
	raw, err := json.Marshal(response)
	if err != nil {
		t.Fatal(err)
	}
	if status == http.StatusCreated ||
		response["replayed"] == true ||
		strings.Contains(string(raw), secret) {
		t.Fatalf("cross-member idempotency replay leaked status=%d body=%s", status, raw)
	}

	status, _, response = memberClient.request(
		t,
		http.MethodGet,
		"/trips/packing-privacy-trip/packing-items",
		nil,
		nil,
	)
	raw, err = json.Marshal(response)
	if err != nil {
		t.Fatal(err)
	}
	if status != http.StatusOK || strings.Contains(string(raw), secret) {
		t.Fatalf("cross-member idempotency item entered member view status=%d body=%s", status, raw)
	}
}

func packingPrivacyServer(
	t *testing.T,
	config app.Config,
) (*repository.Repository, *httptest.Server) {
	t.Helper()
	repo, err := repository.Connect(context.Background(), config)
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.Initialize(context.Background()); err != nil {
		_ = repo.Close(context.Background())
		t.Fatal(err)
	}
	return repo, httptest.NewServer(httpapi.New(config, repo))
}

func loginPrivacyClient(t *testing.T, client *apiClient, mobile, password string) {
	t.Helper()
	status, headers, login := client.request(
		t,
		http.MethodPost,
		"/auth/login",
		map[string]any{"mobile": mobile, "password": password},
		nil,
	)
	if status != http.StatusOK {
		t.Fatalf("login %s status=%d body=%#v", mobile, status, login)
	}
	cookies := (&http.Response{Header: headers}).Cookies()
	if len(cookies) != 1 {
		t.Fatalf("login %s cookies=%d", mobile, len(cookies))
	}
	client.cookie = cookies[0]
	client.csrf = fmt.Sprint(login["csrfToken"])
}

func privacyResponseUserID(t *testing.T, payload map[string]any) string {
	t.Helper()
	user, ok := payload["user"].(map[string]any)
	if !ok {
		t.Fatalf("response has no user: %#v", payload)
	}
	id := strings.TrimSpace(fmt.Sprint(user["id"]))
	if id == "" {
		t.Fatalf("response user has no id: %#v", payload)
	}
	return id
}

func createTripMemberForPrivacyTest(
	t *testing.T,
	client *apiClient,
	recordKey, memberID, role string,
) {
	t.Helper()
	status, _, response := client.request(
		t,
		http.MethodPost,
		"/trips/packing-privacy-trip/members",
		map[string]any{
			"recordKey": recordKey,
			"data":      map[string]any{"memberId": memberID, "role": role},
		},
		map[string]string{"Idempotency-Key": "packing-privacy-" + recordKey},
	)
	if status != http.StatusCreated {
		t.Fatalf("create trip member %s status=%d body=%#v", memberID, status, response)
	}
}

type privacyPackingRecord struct {
	ID      string
	Version string
}

func createPackingForPrivacyTest(
	t *testing.T,
	client *apiClient,
	recordKey, secret, forgedOwnerID, wantOwnerID string,
) privacyPackingRecord {
	t.Helper()
	data := map[string]any{
		"item": secret, "category": "other",
		"quantity": 1, "packed": false, "notes": secret + "-NOTE",
	}
	if forgedOwnerID != "" {
		data["ownerMemberId"] = forgedOwnerID
	}
	status, _, response := client.request(
		t,
		http.MethodPost,
		"/trips/packing-privacy-trip/packing-items",
		map[string]any{
			"recordKey": recordKey,
			"data":      data,
		},
		map[string]string{"Idempotency-Key": "packing-privacy-" + recordKey},
	)
	if forgedOwnerID != "" {
		if status != http.StatusBadRequest ||
			response["code"] != "packing-owner-managed" {
			t.Fatalf(
				"forged packing owner status=%d body=%#v",
				status,
				response,
			)
		}
		delete(data, "ownerMemberId")
		status, _, response = client.request(
			t,
			http.MethodPost,
			"/trips/packing-privacy-trip/packing-items",
			map[string]any{
				"recordKey": recordKey,
				"data":      data,
			},
			map[string]string{"Idempotency-Key": "packing-privacy-" + recordKey},
		)
	}
	if status != http.StatusCreated {
		t.Fatalf("create packing %s status=%d body=%#v", recordKey, status, response)
	}
	record := privacyResponseRecord(t, response)
	if got := fmt.Sprint(record["data"].(map[string]any)["ownerMemberId"]); got != wantOwnerID {
		t.Fatalf("create packing owner=%q want=%q body=%#v", got, wantOwnerID, response)
	}
	return privacyPackingRecord{
		ID:      strings.TrimSpace(fmt.Sprint(record["id"])),
		Version: strings.TrimSpace(fmt.Sprint(record["version"])),
	}
}

func privacyResponseRecord(t *testing.T, payload map[string]any) map[string]any {
	t.Helper()
	record, ok := payload["record"].(map[string]any)
	if !ok {
		t.Fatalf("response has no record: %#v", payload)
	}
	if _, ok := record["data"].(map[string]any); !ok {
		t.Fatalf("response record has no data: %#v", payload)
	}
	return record
}

func assertPackingListAndOwnDetail(
	t *testing.T,
	client *apiClient,
	own privacyPackingRecord,
	visibleSecret, hiddenSecret string,
) {
	t.Helper()
	status, _, response := client.request(
		t, http.MethodGet, "/trips/packing-privacy-trip/packing-items", nil, nil,
	)
	raw, err := json.Marshal(response)
	if err != nil {
		t.Fatal(err)
	}
	if status != http.StatusOK ||
		!strings.Contains(string(raw), visibleSecret) ||
		strings.Contains(string(raw), hiddenSecret) {
		t.Fatalf("packing list privacy status=%d body=%s", status, raw)
	}
	records, ok := response["records"].([]any)
	if !ok || len(records) != 1 {
		t.Fatalf("packing list returned unexpected records: %#v", response)
	}

	status, _, response = client.request(
		t,
		http.MethodGet,
		"/trips/packing-privacy-trip/packing-items/"+own.ID,
		nil,
		nil,
	)
	raw, err = json.Marshal(response)
	if err != nil {
		t.Fatal(err)
	}
	if status != http.StatusOK || !strings.Contains(string(raw), visibleSecret) {
		t.Fatalf("own packing detail status=%d body=%s", status, raw)
	}
}

func assertPackingOwnerCannotTransfer(
	t *testing.T,
	client *apiClient,
	own privacyPackingRecord,
	wantOwnerID, forgedOwnerID string,
) privacyPackingRecord {
	t.Helper()
	status, _, response := client.request(
		t,
		http.MethodPatch,
		"/trips/packing-privacy-trip/packing-items/"+own.ID,
		map[string]any{"data": map[string]any{
			"item":     "OWNER-PRIVATE-PASSPORT-UPDATED",
			"category": "other", "quantity": 1, "packed": true,
			"ownerMemberId": forgedOwnerID,
		}},
		map[string]string{"If-Match": own.Version},
	)
	if status != http.StatusBadRequest ||
		response["code"] != "packing-owner-managed" {
		t.Fatalf("forged packing update status=%d body=%#v", status, response)
	}
	status, _, response = client.request(
		t,
		http.MethodPatch,
		"/trips/packing-privacy-trip/packing-items/"+own.ID,
		map[string]any{"data": map[string]any{
			"item":     "OWNER-PRIVATE-PASSPORT-UPDATED",
			"category": "other", "quantity": 1, "packed": true,
		}},
		map[string]string{"If-Match": own.Version},
	)
	if status != http.StatusOK {
		t.Fatalf("update own packing status=%d body=%#v", status, response)
	}
	record := privacyResponseRecord(t, response)
	if got := fmt.Sprint(record["data"].(map[string]any)["ownerMemberId"]); got != wantOwnerID {
		t.Fatalf("packing owner changed to %q, want %q", got, wantOwnerID)
	}
	return privacyPackingRecord{
		ID: own.ID, Version: strings.TrimSpace(fmt.Sprint(record["version"])),
	}
}

func assertForeignPackingCRUDHasNoSideEffects(
	t *testing.T,
	repo *repository.Repository,
	client *apiClient,
	foreign privacyPackingRecord,
	actorMemberID, targetMemberID string,
) {
	t.Helper()
	ctx := context.Background()
	beforeTrip, err := repo.GetTrip(ctx, "packing-privacy-trip")
	if err != nil {
		t.Fatal(err)
	}
	beforeSnapshots, err := repo.ListSnapshots(ctx, "packing-privacy-trip")
	if err != nil {
		t.Fatal(err)
	}
	beforeAudit, err := repo.ListAuditLogs(ctx, "packing-privacy-trip", actorMemberID, 200)
	if err != nil {
		t.Fatal(err)
	}
	beforeTargetAudit, err := repo.ListAuditLogs(
		ctx, "packing-privacy-trip", targetMemberID, 200,
	)
	if err != nil {
		t.Fatal(err)
	}
	beforeRecord, err := repo.GetRecord(
		ctx, "packing-privacy-trip", "packing-items", foreign.ID,
	)
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		method string
		body   any
	}{
		{method: http.MethodGet},
		{method: http.MethodPatch, body: map[string]any{"data": map[string]any{
			"item": "ATTACKER-UPDATE", "category": "other",
			"quantity": 1, "packed": true,
		}}},
		{method: http.MethodDelete},
	}
	for _, test := range tests {
		headers := map[string]string{}
		if test.method != http.MethodGet {
			headers["If-Match"] = foreign.Version
		}
		status, _, response := client.request(
			t,
			test.method,
			"/trips/packing-privacy-trip/packing-items/"+foreign.ID,
			test.body,
			headers,
		)
		if status != http.StatusNotFound || response["code"] != "not-found" {
			t.Fatalf("foreign packing %s status=%d body=%#v", test.method, status, response)
		}
	}

	afterTrip, err := repo.GetTrip(ctx, "packing-privacy-trip")
	if err != nil {
		t.Fatal(err)
	}
	afterSnapshots, err := repo.ListSnapshots(ctx, "packing-privacy-trip")
	if err != nil {
		t.Fatal(err)
	}
	afterAudit, err := repo.ListAuditLogs(ctx, "packing-privacy-trip", actorMemberID, 200)
	if err != nil {
		t.Fatal(err)
	}
	afterTargetAudit, err := repo.ListAuditLogs(
		ctx, "packing-privacy-trip", targetMemberID, 200,
	)
	if err != nil {
		t.Fatal(err)
	}
	afterRecord, err := repo.GetRecord(
		ctx, "packing-privacy-trip", "packing-items", foreign.ID,
	)
	if err != nil {
		t.Fatal(err)
	}
	if afterTrip.Version != beforeTrip.Version ||
		len(afterSnapshots) != len(beforeSnapshots) ||
		len(afterAudit) != len(beforeAudit) ||
		len(afterTargetAudit) != len(beforeTargetAudit) ||
		!reflect.DeepEqual(afterRecord, beforeRecord) {
		t.Fatalf(
			"foreign CRUD caused side effects: version %d->%d snapshots %d->%d "+
				"actor audit %d->%d target audit %d->%d recordEqual=%v",
			beforeTrip.Version, afterTrip.Version,
			len(beforeSnapshots), len(afterSnapshots),
			len(beforeAudit), len(afterAudit),
			len(beforeTargetAudit), len(afterTargetAudit),
			reflect.DeepEqual(afterRecord, beforeRecord),
		)
	}
}

func assertPackingOverviewCount(t *testing.T, client *apiClient, want float64) {
	t.Helper()
	status, _, response := client.request(
		t,
		http.MethodGet,
		"/trips/packing-privacy-trip/overview",
		nil,
		nil,
	)
	counts, ok := response["counts"].(map[string]any)
	if status != http.StatusOK || !ok || counts["packing-items"] != want {
		t.Fatalf("overview status=%d body=%#v, want packing-items=%v", status, response, want)
	}
}

func assertAuditPrivacy(
	t *testing.T,
	client *apiClient,
	visibleSecret, hiddenSecret string,
) {
	t.Helper()
	status, _, response := client.request(
		t,
		http.MethodGet,
		"/trips/packing-privacy-trip/audit-logs",
		nil,
		nil,
	)
	if status != http.StatusOK {
		t.Fatalf("audit status=%d body=%#v", status, response)
	}
	raw, err := json.Marshal(response)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), visibleSecret) {
		t.Fatalf("own packing audit is missing: %s", raw)
	}
	if strings.Contains(string(raw), hiddenSecret) {
		t.Fatalf("foreign packing audit leaked: %s", raw)
	}
	records, _ := response["records"].([]any)
	for _, rawRecord := range records {
		record, _ := rawRecord.(map[string]any)
		if fmt.Sprint(record["tripId"]) != "packing-privacy-trip" {
			t.Fatalf("trip audit mixed a global record: %#v", record)
		}
	}
}

func assertPackingReorderRejected(t *testing.T, client *apiClient) {
	t.Helper()
	status, _, response := client.request(
		t,
		http.MethodPost,
		"/trips/packing-privacy-trip/packing-items/reorder",
		map[string]any{"groupKey": "", "ids": []string{"untrusted-id"}},
		nil,
	)
	if status != http.StatusBadRequest || response["code"] != "reorder-not-supported" {
		t.Fatalf("packing reorder status=%d body=%#v", status, response)
	}

	status, _, response = client.request(
		t,
		http.MethodPost,
		"/trips/packing-privacy-trip/segments/reorder",
		map[string]any{"groupKey": "day-id", "ids": []string{"segment-id"}},
		nil,
	)
	if status != http.StatusPreconditionRequired ||
		response["code"] != "precondition-required" {
		t.Fatalf("segment reorder did not reach its precondition gate: status=%d body=%#v", status, response)
	}
}

func assertSnapshotOwnerBoundary(
	t *testing.T,
	ownerClient, memberClient *apiClient,
) {
	t.Helper()
	status, _, ownerListing := ownerClient.request(
		t,
		http.MethodGet,
		"/trips/packing-privacy-trip/snapshots",
		nil,
		nil,
	)
	snapshots, ok := ownerListing["snapshots"].([]any)
	if status != http.StatusOK || !ok || len(snapshots) == 0 {
		t.Fatalf("owner snapshots status=%d body=%#v", status, ownerListing)
	}
	snapshot, _ := snapshots[0].(map[string]any)
	snapshotID := strings.TrimSpace(fmt.Sprint(snapshot["id"]))
	if snapshotID == "" {
		t.Fatalf("owner snapshot has no id: %#v", snapshot)
	}

	for _, path := range []string{
		"/trips/packing-privacy-trip/snapshots",
		"/trips/packing-privacy-trip/snapshots/" + snapshotID,
		"/trips/packing-privacy-trip/snapshots/" + snapshotID + "/restore-preview",
	} {
		status, _, response := memberClient.request(t, http.MethodGet, path, nil, nil)
		if status != http.StatusForbidden || response["code"] != "owner-required" {
			t.Fatalf("member snapshot access %s status=%d body=%#v", path, status, response)
		}
	}
}
