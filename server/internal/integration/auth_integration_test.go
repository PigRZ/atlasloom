// 认证集成测试验证成员状态与登录、现有会话使用同一条授权边界。
package integration

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"my-travel/server/internal/httpapi"
	"my-travel/server/internal/model"
	"my-travel/server/internal/repository"
	"my-travel/server/internal/service"
)

func TestMongoIntegrationInactiveMemberCannotAuthenticate(t *testing.T) {
	config, cleanup := mongoIntegrationConfig(t)
	defer cleanup()
	config.ProvisionToken = "integration-provision-token"

	repo, err := repository.Connect(context.Background(), config)
	if err != nil {
		t.Fatal(err)
	}
	defer repo.Close(context.Background())
	if err := repo.Initialize(context.Background()); err != nil {
		t.Fatal(err)
	}

	server := httptest.NewServer(httpapi.New(config, repo))
	defer server.Close()

	adminClient := &apiClient{baseURL: server.URL, client: server.Client()}
	status, _, bootstrap := adminClient.request(
		t,
		http.MethodPost,
		"/auth/bootstrap",
		map[string]any{
			"token":       config.BootstrapToken,
			"displayName": "管理员",
			"mobile":      "13800138000",
			"password":    "integration-admin-password",
		},
		nil,
	)
	if status != http.StatusCreated {
		t.Fatalf("bootstrap status=%d body=%#v", status, bootstrap)
	}
	adminID := responseUserID(t, bootstrap)
	loginClient(t, adminClient, "13800138000", "integration-admin-password")
	status, _, ownerTrip := adminClient.request(
		t,
		http.MethodPost,
		"/trips",
		map[string]any{
			"id": "inactive-owner-continuity", "title": "负责人连续性",
			"startDate": "2026-09-01", "endDate": "2026-09-02",
		},
		map[string]string{"Idempotency-Key": "inactive-owner-continuity-create"},
	)
	if status != http.StatusCreated {
		t.Fatalf("create owner trip status=%d body=%#v", status, ownerTrip)
	}

	status, _, provisioned := adminClient.request(
		t,
		http.MethodPost,
		"/auth/users",
		map[string]any{
			"token":       config.ProvisionToken,
			"displayName": "待停用成员",
			"mobile":      "13900139000",
			"password":    "integration-member-password",
		},
		nil,
	)
	if status != http.StatusCreated {
		t.Fatalf("provision status=%d body=%#v", status, provisioned)
	}
	memberID := responseUserID(t, provisioned)

	memberClient := &apiClient{baseURL: server.URL, client: server.Client()}
	loginClient(t, memberClient, "13900139000", "integration-member-password")
	originalSessionID := memberClient.cookie.Value
	status, _, session := memberClient.request(t, http.MethodGet, "/session", nil, nil)
	if status != http.StatusOK {
		t.Fatalf("active member session status=%d body=%#v", status, session)
	}

	memberRow, err := repo.GetLibraryRecord(
		context.Background(),
		model.LibraryMembers,
		memberID,
	)
	if err != nil {
		t.Fatal(err)
	}
	inactiveData := copyLibraryData(memberRow.Data)
	inactiveData["status"] = "inactive"
	status, _, updated := adminClient.request(
		t,
		http.MethodPatch,
		"/library/members/"+memberID,
		map[string]any{"data": inactiveData},
		map[string]string{"If-Match": fmt.Sprint(memberRow.Version)},
	)
	if status != http.StatusOK {
		t.Fatalf("deactivate member status=%d body=%#v", status, updated)
	}

	if _, err := repo.GetSession(context.Background(), originalSessionID); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("inactive member session was not deleted: %v", err)
	}
	if _, err := repo.GetMemberIdentity(context.Background(), memberID); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("inactive member identity remained authenticatable: %v", err)
	}
	travelService := service.Service{Repo: repo, Config: config}
	if _, _, _, err := travelService.CreateTrip(
		context.Background(),
		memberID,
		model.Trip{
			ID: "inactive-member-trip", Title: "停用成员不能创建旅行",
			StartDate: "2026-09-03", EndDate: "2026-09-04",
		},
		"inactive-member-trip-create",
		strings.Repeat("a", 64),
	); !errors.Is(err, repository.ErrForbidden) {
		t.Fatalf("inactive member created a trip owner: %v", err)
	}
	if _, err := repo.GetTrip(
		context.Background(),
		"inactive-member-trip",
	); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("rejected inactive owner creation left a trip behind: %v", err)
	}
	backup, err := repo.ExportTripBackup(
		context.Background(),
		"inactive-owner-continuity",
	)
	if err != nil {
		t.Fatal(err)
	}
	backupRaw, err := json.Marshal(backup)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := travelService.ApplyTripBackupImport(
		context.Background(),
		backupRaw,
		backup.ContentChecksum,
		nil,
		nil,
		model.MemberIdentity{ID: memberID},
		"inactive-member-trip-import",
		strings.Repeat("b", 64),
	); !errors.Is(err, repository.ErrForbidden) {
		t.Fatalf("inactive member imported a trip owner: %v", err)
	}

	status, headers, expired := memberClient.request(t, http.MethodGet, "/session", nil, nil)
	if status != http.StatusUnauthorized || expired["code"] != "session-expired" {
		t.Fatalf("inactive existing session status=%d body=%#v", status, expired)
	}
	if !strings.Contains(headers.Get("Set-Cookie"), "Max-Age=0") {
		t.Fatalf("expired session did not clear its cookie: %q", headers.Get("Set-Cookie"))
	}

	freshClient := &apiClient{baseURL: server.URL, client: server.Client()}
	status, _, rejected := freshClient.request(
		t,
		http.MethodPost,
		"/auth/login",
		map[string]any{
			"mobile": "13900139000", "password": "integration-member-password",
		},
		nil,
	)
	if status != http.StatusUnauthorized || rejected["code"] != "invalid-credentials" {
		t.Fatalf("inactive fresh login status=%d body=%#v", status, rejected)
	}

	memberRow, err = repo.GetLibraryRecord(
		context.Background(),
		model.LibraryMembers,
		memberID,
	)
	if err != nil {
		t.Fatal(err)
	}
	reactivatedData := copyLibraryData(memberRow.Data)
	delete(reactivatedData, "status")
	status, _, updated = adminClient.request(
		t,
		http.MethodPatch,
		"/library/members/"+memberID,
		map[string]any{"data": reactivatedData},
		map[string]string{"If-Match": fmt.Sprint(memberRow.Version)},
	)
	if status != http.StatusOK {
		t.Fatalf("remove member status status=%d body=%#v", status, updated)
	}
	loginClient(t, freshClient, "13900139000", "integration-member-password")

	memberRow, err = repo.GetLibraryRecord(
		context.Background(),
		model.LibraryMembers,
		memberID,
	)
	if err != nil {
		t.Fatal(err)
	}
	promotedData := copyLibraryData(memberRow.Data)
	promotedData["admin"] = true
	status, _, updated = adminClient.request(
		t,
		http.MethodPatch,
		"/library/members/"+memberID,
		map[string]any{"data": promotedData},
		map[string]string{"If-Match": fmt.Sprint(memberRow.Version)},
	)
	if status != http.StatusOK {
		t.Fatalf("promote second admin status=%d body=%#v", status, updated)
	}

	adminRow, err := repo.GetLibraryRecord(
		context.Background(),
		model.LibraryMembers,
		adminID,
	)
	if err != nil {
		t.Fatal(err)
	}
	inactiveAdminData := copyLibraryData(adminRow.Data)
	inactiveAdminData["status"] = "inactive"
	status, _, rejected = adminClient.request(
		t,
		http.MethodPatch,
		"/library/members/"+adminID,
		map[string]any{"data": inactiveAdminData},
		map[string]string{"If-Match": fmt.Sprint(adminRow.Version)},
	)
	if status != http.StatusBadRequest ||
		!strings.Contains(fmt.Sprint(rejected["detail"]), "未删除旅行的负责人") {
		t.Fatalf("active trip owner deactivation status=%d body=%#v", status, rejected)
	}

	memberRow, err = repo.GetLibraryRecord(
		context.Background(),
		model.LibraryMembers,
		memberID,
	)
	if err != nil {
		t.Fatal(err)
	}
	inactiveSecondAdmin := copyLibraryData(memberRow.Data)
	inactiveSecondAdmin["status"] = "inactive"
	status, _, updated = adminClient.request(
		t,
		http.MethodPatch,
		"/library/members/"+memberID,
		map[string]any{"data": inactiveSecondAdmin},
		map[string]string{"If-Match": fmt.Sprint(memberRow.Version)},
	)
	if status != http.StatusOK {
		t.Fatalf("deactivate second admin status=%d body=%#v", status, updated)
	}

	status, _, rejected = adminClient.request(
		t,
		http.MethodPatch,
		"/library/members/"+adminID,
		map[string]any{"data": inactiveAdminData},
		map[string]string{"If-Match": fmt.Sprint(adminRow.Version)},
	)
	if status != http.StatusBadRequest ||
		!strings.Contains(fmt.Sprint(rejected["detail"]), "至少保留一名已启用且可登录的管理员") {
		t.Fatalf("last active admin deactivation status=%d body=%#v", status, rejected)
	}
}

func TestMongoIntegrationInactiveUnreferencedMemberCanBePermanentlyDeleted(t *testing.T) {
	config, cleanup := mongoIntegrationConfig(t)
	defer cleanup()
	config.ProvisionToken = "delete-member-provision-token"

	repo, err := repository.Connect(context.Background(), config)
	if err != nil {
		t.Fatal(err)
	}
	defer repo.Close(context.Background())
	if err := repo.Initialize(context.Background()); err != nil {
		t.Fatal(err)
	}

	server := httptest.NewServer(httpapi.New(config, repo))
	defer server.Close()
	adminClient := &apiClient{baseURL: server.URL, client: server.Client()}
	status, _, bootstrap := adminClient.request(
		t,
		http.MethodPost,
		"/auth/bootstrap",
		map[string]any{
			"token":       config.BootstrapToken,
			"displayName": "管理员",
			"mobile":      "13800138000",
			"password":    "integration-admin-password",
		},
		nil,
	)
	if status != http.StatusCreated {
		t.Fatalf("bootstrap status=%d body=%#v", status, bootstrap)
	}
	loginClient(t, adminClient, "13800138000", "integration-admin-password")

	status, _, provisioned := adminClient.request(
		t,
		http.MethodPost,
		"/auth/users",
		map[string]any{
			"token":       config.ProvisionToken,
			"displayName": "待永久删除成员",
			"mobile":      "13900139000",
			"password":    "integration-member-password",
		},
		nil,
	)
	if status != http.StatusCreated {
		t.Fatalf("provision status=%d body=%#v", status, provisioned)
	}
	memberID := responseUserID(t, provisioned)
	memberRow, err := repo.GetLibraryRecord(
		context.Background(),
		model.LibraryMembers,
		memberID,
	)
	if err != nil {
		t.Fatal(err)
	}
	status, _, activeDelete := adminClient.request(
		t,
		http.MethodDelete,
		"/library/members/"+memberID+"?deleteLoginCredential=true",
		nil,
		map[string]string{"If-Match": fmt.Sprint(memberRow.Version)},
	)
	if status != http.StatusConflict ||
		activeDelete["code"] != "resource-referenced" ||
		!strings.Contains(fmt.Sprint(activeDelete["detail"]), "请先停用成员") {
		t.Fatalf("active credential delete status=%d body=%#v", status, activeDelete)
	}

	inactiveData := copyLibraryData(memberRow.Data)
	inactiveData["status"] = "inactive"
	status, _, deactivated := adminClient.request(
		t,
		http.MethodPatch,
		"/library/members/"+memberID,
		map[string]any{"data": inactiveData},
		map[string]string{"If-Match": fmt.Sprint(memberRow.Version)},
	)
	if status != http.StatusOK {
		t.Fatalf("deactivate status=%d body=%#v", status, deactivated)
	}
	memberRow, err = repo.GetLibraryRecord(
		context.Background(),
		model.LibraryMembers,
		memberID,
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.CreateSession(context.Background(), model.Session{
		ID:        "stale-session-for-deleted-member",
		MemberID:  memberID,
		CSRFToken: "stale-csrf",
		ExpiresAt: time.Now().UTC().Add(time.Hour),
	}); err != nil {
		t.Fatal(err)
	}
	status, _, impact := adminClient.request(
		t,
		http.MethodGet,
		"/library/members/"+memberID+"/deletion-impact",
		nil,
		nil,
	)
	references, ok := impact["references"].([]any)
	if status != http.StatusOK ||
		!ok ||
		len(references) != 0 ||
		impact["hasLoginCredential"] != true ||
		impact["hasTripOwnership"] != false ||
		impact["canDeleteWithCredential"] != true {
		t.Fatalf("inactive member impact status=%d body=%#v", status, impact)
	}
	status, _, unconfirmed := adminClient.request(
		t,
		http.MethodDelete,
		"/library/members/"+memberID,
		nil,
		map[string]string{"If-Match": fmt.Sprint(memberRow.Version)},
	)
	if status != http.StatusConflict ||
		unconfirmed["code"] != "resource-referenced" {
		t.Fatalf("unconfirmed credential delete status=%d body=%#v", status, unconfirmed)
	}
	status, _, deleted := adminClient.request(
		t,
		http.MethodDelete,
		"/library/members/"+memberID+"?deleteLoginCredential=true",
		nil,
		map[string]string{"If-Match": fmt.Sprint(memberRow.Version)},
	)
	snapshot, ok := deleted["snapshot"].(map[string]any)
	if status != http.StatusOK ||
		deleted["deleted"] != true ||
		!ok ||
		snapshot["mirrorState"] != "mirrored" {
		t.Fatalf("permanent member delete status=%d body=%#v", status, deleted)
	}
	if _, err := repo.GetLibraryRecord(
		context.Background(),
		model.LibraryMembers,
		memberID,
	); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("deleted member still exists: %v", err)
	}
	if _, err := repo.GetMemberCredential(
		context.Background(),
		memberID,
	); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("deleted member credential still exists: %v", err)
	}
	if _, err := repo.GetSession(
		context.Background(),
		"stale-session-for-deleted-member",
	); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("deleted member session still exists: %v", err)
	}
}

func loginClient(t *testing.T, client *apiClient, mobile, password string) {
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

func responseUserID(t *testing.T, payload map[string]any) string {
	t.Helper()
	user, ok := payload["user"].(map[string]any)
	if !ok {
		t.Fatalf("missing user response: %#v", payload)
	}
	id := strings.TrimSpace(fmt.Sprint(user["id"]))
	if id == "" {
		t.Fatalf("missing user id: %#v", payload)
	}
	return id
}

func copyLibraryData(data map[string]any) map[string]any {
	copied := make(map[string]any, len(data))
	for key, value := range data {
		copied[key] = value
	}
	return copied
}
