// 数据库契约单元测试固定指纹、集合覆盖和索引命名，避免结构声明静默漂移。
package repository

import "testing"

func TestDatabaseContractFingerprintIsStableAndComplete(t *testing.T) {
	first := CurrentDatabaseContractFingerprint()
	second := CurrentDatabaseContractFingerprint()
	if len(first) != 64 || first != second {
		t.Fatalf("unstable database contract fingerprint: %q / %q", first, second)
	}

	required := map[string]bool{
		"trips": false, "records": false, "library_state": false,
		"library_records": false, "member_credentials": false,
		"memberships": false, "sessions": false, "audit_logs": false,
		"snapshots": false, "library_snapshots": false,
		"idempotency_requests": false, "system_metadata": false,
	}
	for _, contract := range databaseCollectionContracts() {
		if _, exists := required[contract.Name]; !exists {
			t.Fatalf("unexpected collection contract %q", contract.Name)
		}
		if required[contract.Name] {
			t.Fatalf("duplicate collection contract %q", contract.Name)
		}
		required[contract.Name] = true
	}
	for name, found := range required {
		if !found {
			t.Fatalf("missing collection contract %q", name)
		}
	}
}

func TestDatabaseIndexesHaveUniqueManagedNamesPerCollection(t *testing.T) {
	names := map[string]struct{}{}
	for _, contract := range databaseIndexContracts() {
		key := contract.Collection + "/" + contract.Name
		if _, exists := names[key]; exists {
			t.Fatalf("duplicate managed index %s", key)
		}
		names[key] = struct{}{}
	}
	for _, required := range []string{
		"snapshots/trip_created_at",
		"snapshots/mirror_state_created_at",
		"library_snapshots/created_at",
		"library_snapshots/mirror_state_created_at",
	} {
		if _, exists := names[required]; !exists {
			t.Fatalf("missing snapshot index %s", required)
		}
	}
}
