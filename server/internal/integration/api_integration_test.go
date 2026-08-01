// 端到端集成测试通过真实 HTTP、服务层、事务和 MongoDB 完成最小旅行写入闭环。
package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
	"my-travel/server/internal/app"
	"my-travel/server/internal/httpapi"
	"my-travel/server/internal/model"
	"my-travel/server/internal/repository"
)

type apiClient struct {
	baseURL string
	client  *http.Client
	cookie  *http.Cookie
	csrf    string
}

func (client *apiClient) request(
	t *testing.T,
	method, path string,
	body any,
	headers map[string]string,
) (int, http.Header, map[string]any) {
	t.Helper()
	var payload io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		payload = bytes.NewReader(encoded)
	}
	request, err := http.NewRequest(method, client.baseURL+path, payload)
	if err != nil {
		t.Fatal(err)
	}
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	if client.cookie != nil {
		request.AddCookie(client.cookie)
	}
	if client.csrf != "" && method != http.MethodGet {
		request.Header.Set("X-CSRF-Token", client.csrf)
	}
	for key, value := range headers {
		request.Header.Set(key, value)
	}
	response, err := client.client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	result := map[string]any{}
	if len(bytes.TrimSpace(raw)) > 0 {
		if err := json.Unmarshal(raw, &result); err != nil {
			t.Fatalf("decode %s %s response: %v: %s", method, path, err, raw)
		}
	}
	return response.StatusCode, response.Header, result
}

func mongoIntegrationConfig(t *testing.T) (app.Config, func()) {
	t.Helper()
	uri := strings.TrimSpace(os.Getenv("MONGODB_TEST_URI"))
	if uri == "" {
		t.Skip("MONGODB_TEST_URI 未设置，跳过真实 API 集成测试")
	}
	database := fmt.Sprintf("my_travel_api_%d", time.Now().UnixNano())
	config := app.Config{
		MongoURI:       uri,
		MongoDatabase:  database,
		SnapshotDir:    t.TempDir(),
		CookieName:     "travel_integration",
		CookieSecure:   false,
		SessionTTL:     time.Hour,
		BootstrapToken: "integration-bootstrap-token",
		AppBasePath:    "travel",
	}
	cleanup := func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		client, err := mongo.Connect(options.Client().ApplyURI(uri))
		if err != nil {
			return
		}
		defer client.Disconnect(ctx)
		_ = client.Database(database).Drop(ctx)
	}
	return config, cleanup
}

func TestMongoIntegrationAPIWorkflow(t *testing.T) {
	config, cleanup := mongoIntegrationConfig(t)
	defer cleanup()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	repo, err := repository.Connect(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	defer repo.Close(context.Background())
	if err := repo.Initialize(ctx); err != nil {
		t.Fatal(err)
	}

	server := httptest.NewServer(httpapi.New(config, repo))
	defer server.Close()
	client := &apiClient{baseURL: server.URL, client: server.Client()}

	status, _, _ := client.request(
		t,
		http.MethodPost,
		"/auth/bootstrap",
		map[string]any{
			"token":       config.BootstrapToken,
			"displayName": "集成测试成员",
			"mobile":      "13800138000",
			"password":    "integration-password",
		},
		nil,
	)
	if status != http.StatusCreated {
		t.Fatalf("bootstrap status=%d", status)
	}

	status, headers, login := client.request(
		t,
		http.MethodPost,
		"/auth/login",
		map[string]any{
			"mobile": "13800138000", "password": "integration-password",
		},
		nil,
	)
	if status != http.StatusOK {
		t.Fatalf("login status=%d body=%#v", status, login)
	}
	cookies := (&http.Response{Header: headers}).Cookies()
	if len(cookies) != 1 {
		t.Fatalf("login cookies=%d", len(cookies))
	}
	client.cookie = cookies[0]
	client.csrf = fmt.Sprint(login["csrfToken"])
	user, ok := login["user"].(map[string]any)
	if !ok {
		t.Fatalf("login user=%#v", login)
	}
	memberID := fmt.Sprint(user["id"])

	tripBody := map[string]any{
		"id": "integration-trip", "title": "集成测试旅行",
		"startDate": "2026-09-01", "endDate": "2026-09-03",
	}
	status, _, created := client.request(
		t,
		http.MethodPost,
		"/trips",
		tripBody,
		map[string]string{"Idempotency-Key": "integration-create-trip-0001"},
	)
	if status != http.StatusCreated || created["replayed"] != false {
		t.Fatalf("create trip status=%d body=%#v", status, created)
	}
	snapshot, ok := created["snapshot"].(map[string]any)
	if !ok || snapshot["mirrorState"] != "mirrored" {
		t.Fatalf("initial snapshot was not mirrored: %#v", created)
	}

	status, replayHeaders, replayed := client.request(
		t,
		http.MethodPost,
		"/trips",
		tripBody,
		map[string]string{"Idempotency-Key": "integration-create-trip-0001"},
	)
	if status != http.StatusCreated ||
		replayHeaders.Get("Idempotency-Replayed") != "true" ||
		replayed["replayed"] != true {
		t.Fatalf("idempotent replay status=%d headers=%v body=%#v", status, replayHeaders, replayed)
	}

	status, _, selectedMember := client.request(
		t,
		http.MethodPost,
		"/trips/integration-trip/members",
		map[string]any{
			"recordKey": "integration-owner",
			"data": map[string]any{
				"memberId": memberID,
				"role":     "owner",
			},
		},
		map[string]string{"Idempotency-Key": "integration-select-owner-0001"},
	)
	if status != http.StatusCreated {
		t.Fatalf("select trip member status=%d body=%#v", status, selectedMember)
	}

	status, _, expenseTagPayload := client.request(
		t,
		http.MethodPost,
		"/library/tags",
		map[string]any{
			"recordKey": "integration-expense-type",
			"data": map[string]any{
				"name": "餐饮", "category": "expense-type",
				"color": "#a4663f", "status": "active",
			},
		},
		map[string]string{"Idempotency-Key": "integration-create-expense-type-0001"},
	)
	if status != http.StatusCreated {
		t.Fatalf("create expense type status=%d body=%#v", status, expenseTagPayload)
	}
	expenseTag, ok := expenseTagPayload["record"].(map[string]any)
	if !ok {
		t.Fatalf("created expense type=%#v", expenseTagPayload)
	}

	status, _, packingMemberPayload := client.request(
		t,
		http.MethodPost,
		"/library/members",
		map[string]any{
			"recordKey": "integration-packing-member",
			"data": map[string]any{
				"displayName": "仅有物品成员",
				"mobile":      "13900139000",
				"status":      "active",
				"admin":       false,
			},
			"password": "integration-packing-password",
		},
		map[string]string{"Idempotency-Key": "integration-create-packing-member"},
	)
	if status != http.StatusCreated {
		t.Fatalf("create packing member status=%d body=%#v", status, packingMemberPayload)
	}
	packingMember, ok := packingMemberPayload["record"].(map[string]any)
	if !ok {
		t.Fatalf("created packing member=%#v", packingMemberPayload)
	}
	packingMemberID := fmt.Sprint(packingMember["id"])
	status, _, selectedPackingMemberPayload := client.request(
		t,
		http.MethodPost,
		"/trips/integration-trip/members",
		map[string]any{
			"recordKey": "integration-packing-trip-member",
			"data": map[string]any{
				"memberId": packingMemberID,
				"role":     "同行成员",
			},
		},
		map[string]string{"Idempotency-Key": "integration-select-packing-member"},
	)
	if status != http.StatusCreated {
		t.Fatalf(
			"select packing member status=%d body=%#v",
			status,
			selectedPackingMemberPayload,
		)
	}
	selectedPackingMember, ok := selectedPackingMemberPayload["record"].(map[string]any)
	if !ok {
		t.Fatalf("selected packing member=%#v", selectedPackingMemberPayload)
	}
	status, _, cascadeExpensePayload := client.request(
		t,
		http.MethodPost,
		"/trips/integration-trip/expenses",
		map[string]any{
			"recordKey": "integration-cascade-expense",
			"data": map[string]any{
				"item":           "待连带删除早餐",
				"occurredAt":     "2026-09-01T09:00",
				"type":           fmt.Sprint(expenseTag["id"]),
				"amountMinor":    3200,
				"payerMemberId":  packingMemberID,
				"splitMode":      "selected",
				"splitMemberIds": []string{packingMemberID},
			},
		},
		map[string]string{"Idempotency-Key": "integration-create-cascade-expense"},
	)
	if status != http.StatusCreated {
		t.Fatalf(
			"create cascade expense status=%d body=%#v",
			status,
			cascadeExpensePayload,
		)
	}
	cascadeExpense, ok := cascadeExpensePayload["record"].(map[string]any)
	if !ok {
		t.Fatalf("created cascade expense=%#v", cascadeExpensePayload)
	}
	packingClient := &apiClient{baseURL: server.URL, client: server.Client()}
	loginClient(t, packingClient, "13900139000", "integration-packing-password")
	status, _, packingPayload := packingClient.request(
		t,
		http.MethodPost,
		"/trips/integration-trip/packing-items",
		map[string]any{
			"recordKey": "integration-private-packing",
			"data": map[string]any{
				"item":     "隐私物品",
				"category": "other",
			},
		},
		map[string]string{"Idempotency-Key": "integration-create-private-packing"},
	)
	if status != http.StatusCreated {
		t.Fatalf("create private packing status=%d body=%#v", status, packingPayload)
	}
	packingRecord, ok := packingPayload["record"].(map[string]any)
	if !ok {
		t.Fatalf("created private packing=%#v", packingPayload)
	}
	status, _, packingImpact := client.request(
		t,
		http.MethodGet,
		"/trips/integration-trip/members/"+
			fmt.Sprint(selectedPackingMember["id"])+
			"/deletion-impact",
		nil,
		nil,
	)
	packingReferences, ok := packingImpact["references"].([]any)
	cascadeExpenses, cascadeExpensesOK := packingImpact["cascadeExpenses"].([]any)
	if status != http.StatusOK ||
		!ok ||
		len(packingReferences) != 0 ||
		!cascadeExpensesOK ||
		len(cascadeExpenses) != 1 ||
		packingImpact["hasPrivatePackingItems"] != true {
		t.Fatalf(
			"cascadeable member impact status=%d body=%#v",
			status,
			packingImpact,
		)
	}
	status, _, blockedPackingMemberDelete := client.request(
		t,
		http.MethodDelete,
		"/trips/integration-trip/members/"+fmt.Sprint(selectedPackingMember["id"]),
		nil,
		map[string]string{
			"If-Match": fmt.Sprint(selectedPackingMember["version"]),
		},
	)
	if status != http.StatusConflict ||
		blockedPackingMemberDelete["code"] != "resource-referenced" {
		t.Fatalf(
			"unconfirmed packing cascade status=%d body=%#v",
			status,
			blockedPackingMemberDelete,
		)
	}
	status, _, cascadedPackingMemberDelete := client.request(
		t,
		http.MethodDelete,
		"/trips/integration-trip/members/"+
			fmt.Sprint(selectedPackingMember["id"])+
			"?cascade=true",
		nil,
		map[string]string{
			"If-Match": fmt.Sprint(selectedPackingMember["version"]),
		},
	)
	cascadeSnapshot, ok := cascadedPackingMemberDelete["snapshot"].(map[string]any)
	if status != http.StatusOK ||
		!ok ||
		cascadeSnapshot["mirrorState"] != "mirrored" {
		t.Fatalf(
			"confirmed packing cascade status=%d body=%#v",
			status,
			cascadedPackingMemberDelete,
		)
	}
	if _, err := repo.GetRecord(
		ctx,
		"integration-trip",
		model.Members,
		fmt.Sprint(selectedPackingMember["id"]),
	); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("cascaded trip member still exists: %v", err)
	}
	if _, err := repo.GetRecord(
		ctx,
		"integration-trip",
		model.PackingItems,
		fmt.Sprint(packingRecord["id"]),
	); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("cascaded private packing item still exists: %v", err)
	}
	if _, err := repo.GetRecord(
		ctx,
		"integration-trip",
		model.Expenses,
		fmt.Sprint(cascadeExpense["id"]),
	); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("cascaded manual expense still exists: %v", err)
	}

	status, _, daysPayload := client.request(
		t,
		http.MethodGet,
		"/trips/integration-trip/days",
		nil,
		nil,
	)
	days, ok := daysPayload["records"].([]any)
	if status != http.StatusOK || !ok || len(days) != 3 {
		t.Fatalf("initial days status=%d body=%#v", status, daysPayload)
	}
	firstDay, ok := days[0].(map[string]any)
	if !ok {
		t.Fatalf("first initial day=%#v", days[0])
	}

	status, _, tagPayload := client.request(
		t,
		http.MethodPost,
		"/library/tags",
		map[string]any{
			"recordKey": "integration-place-type",
			"data": map[string]any{
				"name": "测试地点", "category": "place-type",
				"color": "#477566", "status": "active",
			},
		},
		map[string]string{"Idempotency-Key": "integration-create-place-type-0001"},
	)
	if status != http.StatusCreated {
		t.Fatalf("create place type status=%d body=%#v", status, tagPayload)
	}
	tag, ok := tagPayload["record"].(map[string]any)
	if !ok {
		t.Fatalf("created place type=%#v", tagPayload)
	}

	dayID := fmt.Sprint(firstDay["id"])
	dayVersion := fmt.Sprint(firstDay["version"])
	placeBody := map[string]any{
		"recordKey": "integration-place",
		"data": map[string]any{
			"name": "集成测试地点", "type": fmt.Sprint(tag["id"]),
			"longitude": 104.066, "latitude": 30.572,
		},
	}
	status, _, dayPlace := client.request(
		t,
		http.MethodPost,
		"/trips/integration-trip/days/"+dayID+"/places",
		placeBody,
		map[string]string{
			"If-Match":        dayVersion,
			"Idempotency-Key": "integration-create-day-place-0001",
		},
	)
	if status != http.StatusCreated {
		t.Fatalf("create day place status=%d body=%#v", status, dayPlace)
	}
	updatedDay, ok := dayPlace["day"].(map[string]any)
	if !ok {
		t.Fatalf("updated day=%#v", dayPlace)
	}
	updatedData, ok := updatedDay["data"].(map[string]any)
	if !ok {
		t.Fatalf("updated day data=%#v", updatedDay)
	}
	entries, ok := updatedData["placeEntries"].([]any)
	if !ok || len(entries) != 1 {
		t.Fatalf("updated day entries=%#v", updatedData)
	}

	status, replayHeaders, replayedDayPlace := client.request(
		t,
		http.MethodPost,
		"/trips/integration-trip/days/"+dayID+"/places",
		placeBody,
		map[string]string{
			"If-Match":        dayVersion,
			"Idempotency-Key": "integration-create-day-place-0001",
		},
	)
	if status != http.StatusCreated ||
		replayHeaders.Get("Idempotency-Replayed") != "true" ||
		replayedDayPlace["replayed"] != true {
		t.Fatalf(
			"idempotent day place status=%d headers=%v body=%#v",
			status,
			replayHeaders,
			replayedDayPlace,
		)
	}

	expenseData := map[string]any{
		"item":           "集成测试午餐",
		"occurredAt":     "2026-09-01T12:00",
		"type":           fmt.Sprint(expenseTag["id"]),
		"amountMinor":    8800,
		"payerMemberId":  memberID,
		"splitMode":      "selected",
		"splitMemberIds": []string{memberID},
		"notes":          "手工录入",
	}
	status, _, expenseCreated := client.request(
		t,
		http.MethodPost,
		"/trips/integration-trip/expenses",
		map[string]any{"recordKey": "integration-expense", "data": expenseData},
		map[string]string{"Idempotency-Key": "integration-create-expense-0001"},
	)
	if status != http.StatusCreated {
		t.Fatalf("create manual expense status=%d body=%#v", status, expenseCreated)
	}
	expense, ok := expenseCreated["record"].(map[string]any)
	if !ok {
		t.Fatalf("created manual expense=%#v", expenseCreated)
	}
	expenseData["item"] = "集成测试晚餐"
	expenseData["notes"] = "手工修改"
	status, _, expenseUpdated := client.request(
		t,
		http.MethodPatch,
		"/trips/integration-trip/expenses/"+fmt.Sprint(expense["id"]),
		map[string]any{"data": expenseData},
		map[string]string{"If-Match": fmt.Sprint(expense["version"])},
	)
	if status != http.StatusOK {
		t.Fatalf("update manual expense status=%d body=%#v", status, expenseUpdated)
	}
	updatedExpense, ok := expenseUpdated["record"].(map[string]any)
	if !ok {
		t.Fatalf("updated manual expense=%#v", expenseUpdated)
	}
	updatedExpenseData, ok := updatedExpense["data"].(map[string]any)
	if !ok || updatedExpenseData["item"] != "集成测试晚餐" {
		t.Fatalf("updated manual expense data=%#v", updatedExpense)
	}
	selectedMemberRecord, ok := selectedMember["record"].(map[string]any)
	if !ok {
		t.Fatalf("selected member record=%#v", selectedMember)
	}
	status, _, deletionImpact := client.request(
		t,
		http.MethodGet,
		"/trips/integration-trip/members/"+
			fmt.Sprint(selectedMemberRecord["id"])+
			"/deletion-impact",
		nil,
		nil,
	)
	references, ok := deletionImpact["references"].([]any)
	cascadeExpenses, cascadeExpensesOK = deletionImpact["cascadeExpenses"].([]any)
	if status != http.StatusOK ||
		!ok ||
		len(references) != 0 ||
		!cascadeExpensesOK ||
		len(cascadeExpenses) != 1 {
		t.Fatalf("member deletion impact status=%d body=%#v", status, deletionImpact)
	}
	status, _, blockedDeletion := client.request(
		t,
		http.MethodDelete,
		"/trips/integration-trip/members/"+fmt.Sprint(selectedMemberRecord["id"]),
		nil,
		map[string]string{"If-Match": fmt.Sprint(selectedMemberRecord["version"])},
	)
	if status != http.StatusConflict ||
		blockedDeletion["code"] != "resource-referenced" ||
		!strings.Contains(fmt.Sprint(blockedDeletion["detail"]), "集成测试晚餐") {
		t.Fatalf("blocked member deletion status=%d body=%#v", status, blockedDeletion)
	}

	researchIDs := make([]string, 0, 2)
	var latestRevision any
	for index, title := range []string{"集成测试资料一", "集成测试资料二"} {
		status, _, createdResearch := client.request(
			t,
			http.MethodPost,
			"/trips/integration-trip/research-evidence",
			map[string]any{
				"recordKey": fmt.Sprintf("integration-research-%d", index+1),
				"data":      map[string]any{"title": title, "content": "排序测试"},
			},
			map[string]string{
				"Idempotency-Key": fmt.Sprintf("integration-create-research-%04d", index+1),
			},
		)
		if status != http.StatusCreated {
			t.Fatalf("create research %d status=%d body=%#v", index, status, createdResearch)
		}
		research, ok := createdResearch["record"].(map[string]any)
		if !ok {
			t.Fatalf("created research %d=%#v", index, createdResearch)
		}
		snapshot, ok := createdResearch["snapshot"].(map[string]any)
		if !ok {
			t.Fatalf("created research snapshot %d=%#v", index, createdResearch)
		}
		researchIDs = append(researchIDs, fmt.Sprint(research["id"]))
		latestRevision = snapshot["revision"]
	}
	reversedResearchIDs := []string{researchIDs[1], researchIDs[0]}
	status, _, reordered := client.request(
		t,
		http.MethodPost,
		"/trips/integration-trip/research-evidence/reorder",
		map[string]any{"groupKey": "", "ids": reversedResearchIDs},
		map[string]string{
			"If-Match":        fmt.Sprint(latestRevision),
			"Idempotency-Key": "integration-reorder-research-0001",
		},
	)
	if status != http.StatusOK {
		t.Fatalf("reorder research status=%d body=%#v", status, reordered)
	}
	status, _, reorderedResearchPayload := client.request(
		t,
		http.MethodGet,
		"/trips/integration-trip/research-evidence",
		nil,
		nil,
	)
	reorderedResearch, ok := reorderedResearchPayload["records"].([]any)
	if status != http.StatusOK || !ok || len(reorderedResearch) != len(reversedResearchIDs) {
		t.Fatalf("reordered research status=%d body=%#v", status, reorderedResearchPayload)
	}
	for index, expectedID := range reversedResearchIDs {
		research, ok := reorderedResearch[index].(map[string]any)
		if !ok || fmt.Sprint(research["id"]) != expectedID {
			t.Fatalf(
				"reordered research %d=%#v, want %s",
				index,
				reorderedResearch[index],
				expectedID,
			)
		}
	}
	status, _, rejectedCategoryChange := client.request(
		t,
		http.MethodPatch,
		"/library/tags/"+fmt.Sprint(tag["id"]),
		map[string]any{"data": map[string]any{
			"name": "景点", "category": "expense-type",
			"color": "#477566", "status": "active",
		}},
		map[string]string{"If-Match": fmt.Sprint(tag["version"])},
	)
	if status != http.StatusConflict ||
		rejectedCategoryChange["code"] != "resource-referenced" ||
		!strings.Contains(
			fmt.Sprint(rejectedCategoryChange["detail"]),
			"集成测试旅行 / 地点 / 集成测试地点（地点类型）",
		) {
		t.Fatalf(
			"referenced tag category change status=%d body=%#v",
			status,
			rejectedCategoryChange,
		)
	}

	status, _, overview := client.request(
		t,
		http.MethodGet,
		"/trips/integration-trip/overview",
		nil,
		nil,
	)
	counts, ok := overview["counts"].(map[string]any)
	if status != http.StatusOK ||
		!ok ||
		counts["days"] != float64(3) ||
		counts["places"] != float64(1) {
		t.Fatalf("overview status=%d body=%#v", status, overview)
	}

	clientDB, err := mongo.Connect(options.Client().ApplyURI(config.MongoURI))
	if err != nil {
		t.Fatal(err)
	}
	defer clientDB.Disconnect(context.Background())
	pending, err := clientDB.Database(config.MongoDatabase).
		Collection("snapshots").
		CountDocuments(ctx, bson.D{{Key: "mirror_state", Value: "pending"}})
	if err != nil {
		t.Fatal(err)
	}
	if pending != 0 {
		t.Fatalf("pending snapshots=%d after successful file mirror", pending)
	}
}
