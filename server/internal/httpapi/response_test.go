package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"my-travel/server/internal/repository"
)

func TestRespondRepoMapsTransactionTimeAuthorizationChangeToForbidden(t *testing.T) {
	recorder := httptest.NewRecorder()
	respondRepo(recorder, repository.ErrForbidden)

	var response map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if recorder.Code != http.StatusForbidden ||
		response["code"] != "permission-changed" {
		t.Fatalf("status=%d body=%#v", recorder.Code, response)
	}
}

func TestRespondRepoExplainsMissingAndReferencedResources(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		status     int
		code       string
		title      string
		detailPart string
	}{
		{
			name: "missing", err: repository.ErrNotFound,
			status: http.StatusNotFound, code: "not-found", title: "未找到",
			detailPart: "请刷新页面后重试",
		},
		{
			name: "referenced",
			err: fmt.Errorf(
				"该同行成员仍被费用“晚餐”（付款人）使用，请先修改后再删除: %w",
				repository.ErrReferenced,
			),
			status: http.StatusConflict, code: "resource-referenced",
			title: "操作有冲突", detailPart: "费用“晚餐”",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			respondRepo(recorder, test.err)
			var response map[string]any
			if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
				t.Fatal(err)
			}
			if recorder.Code != test.status ||
				response["code"] != test.code ||
				response["title"] != test.title ||
				!containsText(response["detail"], test.detailPart) {
				t.Fatalf("status=%d body=%#v", recorder.Code, response)
			}
		})
	}
}

func TestVisibleErrorTranslatesValidationAndHidesInternalEnglish(t *testing.T) {
	if got := visibleError(fmt.Errorf("payerMemberId is required")); got != "请选择这笔费用的付款人。" {
		t.Fatalf("translated validation=%q", got)
	}
	if got := visibleError(fmt.Errorf("unknown internal failure")); got != "请求内容不符合要求，请检查后重试。" {
		t.Fatalf("internal English leaked: %q", got)
	}
}

func containsText(value any, expected string) bool {
	text, ok := value.(string)
	return ok && strings.Contains(text, expected)
}
