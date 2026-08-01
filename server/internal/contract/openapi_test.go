// 契约测试加载全部外部引用，并要求每个成功响应都有明确 JSON schema。
package contract

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
)

func openAPIPath(t *testing.T) string {
	t.Helper()
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate OpenAPI contract test")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(filename), "../../../api/openapi.yaml"))
}

func loadOpenAPI(t *testing.T) *openapi3.T {
	t.Helper()
	loader := openapi3.NewLoader()
	loader.IsExternalRefsAllowed = true
	document, err := loader.LoadFromFile(openAPIPath(t))
	if err != nil {
		t.Fatalf("load modular OpenAPI: %v", err)
	}
	if err := document.Validate(context.Background()); err != nil {
		t.Fatalf("validate modular OpenAPI: %v", err)
	}
	return document
}

func TestOpenAPIModulesResolveAndValidate(t *testing.T) {
	document := loadOpenAPI(t)
	if document.Paths == nil || document.Paths.Len() < 40 {
		t.Fatalf("OpenAPI paths=%d, want at least 40", document.Paths.Len())
	}
	if len(document.Components.Schemas) < 60 {
		t.Fatalf("OpenAPI schemas=%d, want at least 60", len(document.Components.Schemas))
	}
}

func TestPackingOwnerIsStoredOutputNotClientInput(t *testing.T) {
	document := loadOpenAPI(t)
	stored := document.Components.Schemas["PackingItemData"]
	if stored == nil || stored.Value == nil {
		t.Fatal("PackingItemData schema did not resolve")
	}
	if _, ok := stored.Value.Properties["ownerMemberId"]; !ok {
		t.Fatal("PackingItemData omitted ownerMemberId")
	}
	required := false
	for _, field := range stored.Value.Required {
		required = required || field == "ownerMemberId"
	}
	if !required {
		t.Fatal("PackingItemData does not require the stored ownerMemberId")
	}

	input := document.Components.Schemas["PackingItemInputData"]
	if input == nil || input.Value == nil {
		t.Fatal("PackingItemInputData schema did not resolve")
	}
	if _, ok := input.Value.Properties["ownerMemberId"]; ok {
		t.Fatal("PackingItemInputData lets clients submit ownerMemberId")
	}
}

func TestValidationFailuresDeclareProblemResponse(t *testing.T) {
	document := loadOpenAPI(t)
	want := map[string]bool{
		"createRecord":    false,
		"updateRecord":    false,
		"restoreSnapshot": false,
	}
	for _, pathItem := range document.Paths.Map() {
		for _, operation := range operations(pathItem) {
			if operation == nil {
				continue
			}
			if _, exists := want[operation.OperationID]; !exists {
				continue
			}
			response := operation.Responses.Value("400")
			if response == nil || response.Value == nil {
				t.Errorf("%s does not declare a resolved 400 response", operation.OperationID)
				continue
			}
			media := response.Value.Content["application/problem+json"]
			if media == nil || media.Schema == nil || media.Schema.Value == nil {
				t.Errorf("%s 400 response is not the resolved Problem schema", operation.OperationID)
				continue
			}
			want[operation.OperationID] = true
		}
	}
	for operationID, found := range want {
		if !found {
			t.Errorf("%s did not expose a valid 400 Problem response", operationID)
		}
	}
}

func TestEverySuccessfulOpenAPIResponseHasSchema(t *testing.T) {
	document := loadOpenAPI(t)
	operationIDs := map[string]string{}
	for path, pathItem := range document.Paths.Map() {
		for method, operation := range operations(pathItem) {
			if operation == nil {
				continue
			}
			location := method + " " + path
			if operation.OperationID == "" {
				t.Errorf("%s has no operationId", location)
			} else if previous, exists := operationIDs[operation.OperationID]; exists {
				t.Errorf(
					"operationId %s is duplicated by %s and %s",
					operation.OperationID,
					previous,
					location,
				)
			} else {
				operationIDs[operation.OperationID] = location
			}
			if len(operation.Tags) != 1 {
				t.Errorf("%s must belong to exactly one module tag", location)
			}
			for status, responseRef := range operation.Responses.Map() {
				if !strings.HasPrefix(status, "2") || status == "204" {
					continue
				}
				if responseRef == nil || responseRef.Value == nil {
					t.Errorf("%s response %s did not resolve", location, status)
					continue
				}
				media, ok := responseRef.Value.Content["application/json"]
				if !ok || media == nil || media.Schema == nil || media.Schema.Value == nil {
					t.Errorf(
						"%s response %s lacks a resolved application/json schema",
						location,
						status,
					)
				}
			}
		}
	}
	if len(operationIDs) < 50 {
		t.Fatalf("validated operations=%d, want at least 50", len(operationIDs))
	}
}

func TestMapLookupOperationsDocumentUnconfiguredResponse(t *testing.T) {
	document := loadOpenAPI(t)
	for _, path := range []string{
		"/trips/{tripId}/place-search",
		"/trips/{tripId}/reverse-geocode",
	} {
		item := document.Paths.Map()[path]
		if item == nil || item.Get == nil {
			t.Fatalf("GET %s is missing", path)
		}
		if response := item.Get.Responses.Map()["503"]; response == nil ||
			response.Value == nil {
			t.Errorf("GET %s does not document the unconfigured 503 response", path)
		}
	}
}

func TestSnapshotPreviewsExposeMembershipAccessChanges(t *testing.T) {
	document := loadOpenAPI(t)
	for _, name := range []string{"RestorePreview", "SnapshotImportPreview"} {
		schemaRef := document.Components.Schemas[name]
		if schemaRef == nil || schemaRef.Value == nil {
			t.Fatalf("%s schema did not resolve", name)
		}
		if _, exists := schemaRef.Value.Properties["membershipAccess"]; !exists {
			t.Fatalf("%s does not expose membershipAccess", name)
		}
		required := false
		for _, field := range schemaRef.Value.Required {
			if field == "membershipAccess" {
				required = true
				break
			}
		}
		if !required {
			t.Fatalf("%s membershipAccess is not required", name)
		}
	}
}

func operations(item *openapi3.PathItem) map[string]*openapi3.Operation {
	return map[string]*openapi3.Operation{
		http.MethodGet:     item.Get,
		http.MethodPost:    item.Post,
		http.MethodPut:     item.Put,
		http.MethodPatch:   item.Patch,
		http.MethodDelete:  item.Delete,
		http.MethodHead:    item.Head,
		http.MethodOptions: item.Options,
		http.MethodTrace:   item.Trace,
	}
}

func TestRootOpenAPIOnlyAggregatesModules(t *testing.T) {
	path := openAPIPath(t)
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Count(string(content), "\n") + 1
	if lines > 400 {
		t.Fatalf("root OpenAPI grew to %d lines; keep module details in referenced files", lines)
	}
	for _, directory := range []string{"paths", "components"} {
		matches, err := filepath.Glob(filepath.Join(filepath.Dir(path), directory, "*.yaml"))
		if err != nil {
			t.Fatal(err)
		}
		if len(matches) < 8 {
			t.Fatalf("%s modules=%d, want at least 8", directory, len(matches))
		}
	}
}
