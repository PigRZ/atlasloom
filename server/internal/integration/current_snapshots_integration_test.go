// 可选的真实数据回归只读取本地受限快照，并在一次性数据库证明当前契约可写入和往返。
package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
	"my-travel/server/internal/app"
	"my-travel/server/internal/model"
	"my-travel/server/internal/repository"
)

var currentSnapshotTripIDPattern = regexp.MustCompile(
	`^[a-z0-9](?:[a-z0-9-]{0,62}[a-z0-9])?$`,
)

type currentTripSnapshotIdentity struct {
	TripID     string
	ExportedAt time.Time
	Revision   int64
}

type currentTripSnapshotCandidate struct {
	Path string
	currentTripSnapshotIdentity
}

type currentLibrarySnapshotIdentity struct {
	ExportedAt time.Time
	Revision   int64
}

type currentLibrarySnapshotCandidate struct {
	Path string
	currentLibrarySnapshotIdentity
}

func TestCurrentSnapshotPathsSelectsOnlyCurrentTripAndLibraryFiles(t *testing.T) {
	root := t.TempDir()
	first := time.Date(2026, 7, 28, 8, 0, 0, 0, time.UTC)
	second := first.Add(time.Hour)

	writeCurrentTripIdentity(
		t, root, "history/trip-alpha.json", "trip-alpha", second, 2,
	)
	currentAlpha := writeCurrentTripIdentity(
		t, root, "current/trip-alpha.json", "trip-alpha", second.Add(time.Hour), 1,
	)
	writeCurrentTripIdentity(
		t, root, "trip-beta/base.json", "trip-beta", first, 3,
	)
	currentBeta := writeCurrentTripIdentity(
		t, root, "trip-beta/working.json", "trip-beta", first, 3,
	)
	workingBeta := currentTripIdentityJSON("trip-beta", first, 3, 3)
	var workingBetaContent map[string]any
	if err := json.Unmarshal(workingBeta, &workingBetaContent); err != nil {
		t.Fatal(err)
	}
	workingBetaContent["localEdit"] = "must-select-working"
	workingBeta, _ = json.Marshal(workingBetaContent)
	if err := os.WriteFile(currentBeta, workingBeta, 0o600); err != nil {
		t.Fatal(err)
	}
	writeCurrentTripIdentity(
		t, root, "trip-gamma/revision-2.json", "trip-gamma", second, 2,
	)
	currentGamma := writeCurrentTripIdentity(
		t, root, "trip-gamma/revision-3.json", "trip-gamma", second, 3,
	)
	writeCurrentLibraryIdentity(t, root, "library/history.json", first, 9)
	currentLibrary := writeCurrentLibraryIdentity(
		t, root, "library/base.json", second, 1,
	)

	tripPaths, libraryPaths := currentSnapshotPaths(t, root)
	wantTrips := []string{currentAlpha, currentBeta, currentGamma}
	sort.Strings(wantTrips)
	if !reflect.DeepEqual(tripPaths, wantTrips) {
		t.Fatalf("current Trip paths=%v, want %v", tripPaths, wantTrips)
	}
	if want := []string{currentLibrary}; !reflect.DeepEqual(libraryPaths, want) {
		t.Fatalf("current library paths=%v, want %v", libraryPaths, want)
	}
}

func TestParseCurrentTripSnapshotIdentityRejectsInvalidCurrentIdentity(t *testing.T) {
	exportedAt := time.Date(2026, 7, 28, 8, 0, 0, 0, time.UTC)
	for name, raw := range map[string][]byte{
		"invalid-json": []byte(`{"trip":`),
		"missing-trip-id": currentTripIdentityJSON(
			"", exportedAt, 1, 1,
		),
		"unsafe-trip-id": currentTripIdentityJSON("../escape", exportedAt, 1, 1),
		"missing-exported-at": currentTripIdentityJSON(
			"trip-a", time.Time{}, 1, 1,
		),
		"invalid-revision":  currentTripIdentityJSON("trip-a", exportedAt, 0, 0),
		"revision-mismatch": currentTripIdentityJSON("trip-a", exportedAt, 2, 1),
	} {
		t.Run(name, func(t *testing.T) {
			_, isTrip, err := parseCurrentTripSnapshotIdentity(raw)
			if err == nil || isTrip {
				t.Fatalf(
					"invalid current Trip identity was accepted: isTrip=%v err=%v",
					isTrip,
					err,
				)
			}
		})
	}
}

func writeCurrentTripIdentity(
	t *testing.T,
	root, relativePath, tripID string,
	exportedAt time.Time,
	revision int64,
) string {
	t.Helper()
	path := filepath.Join(root, relativePath)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		path,
		currentTripIdentityJSON(tripID, exportedAt, revision, revision),
		0o600,
	); err != nil {
		t.Fatal(err)
	}
	return path
}

func currentTripIdentityJSON(
	tripID string,
	exportedAt time.Time,
	revision, tripVersion int64,
) []byte {
	raw, _ := json.Marshal(map[string]any{
		"exportedAt": exportedAt,
		"revision":   revision,
		"trip": map[string]any{
			"id": tripID, "version": tripVersion,
		},
	})
	return raw
}

func writeCurrentLibraryIdentity(
	t *testing.T,
	root, relativePath string,
	exportedAt time.Time,
	revision int64,
) string {
	t.Helper()
	path := filepath.Join(root, relativePath)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(map[string]any{
		"exportedAt": exportedAt,
		"revision":   revision,
		"datasets": map[string]any{
			"members": []any{},
			"tags":    []any{},
		},
	})
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestCurrentSnapshotsRoundTripThroughMongoContract(t *testing.T) {
	root := strings.TrimSpace(os.Getenv("CURRENT_SNAPSHOT_DIR"))
	if root == "" {
		t.Skip("CURRENT_SNAPSHOT_DIR 未设置，跳过本地真实快照回归")
	}
	uri := strings.TrimSpace(os.Getenv("MONGODB_TEST_URI"))
	if uri == "" {
		t.Skip("MONGODB_TEST_URI 未设置，跳过本地真实快照回归")
	}

	tripPaths, libraryPaths := currentSnapshotPaths(t, root)
	if len(tripPaths)+len(libraryPaths) == 0 {
		t.Fatal("CURRENT_SNAPSHOT_DIR 中没有当前完整快照")
	}
	var currentLibrary model.LibrarySnapshotPayload
	if len(tripPaths) > 0 {
		if len(libraryPaths) == 0 {
			t.Fatal("Trip 当前快照回归必须同时提供基础库完整快照")
		}
		currentLibrary = latestCurrentLibrarySnapshot(t, libraryPaths)
	}
	for index, path := range tripPaths {
		path := path
		t.Run(fmt.Sprintf("trip-snapshot-%d", index+1), func(t *testing.T) {
			assertCurrentSnapshotRoundTrip(t, uri, path, index, currentLibrary)
		})
	}
	for index, path := range libraryPaths {
		path := path
		t.Run(fmt.Sprintf("library-snapshot-%d", index+1), func(t *testing.T) {
			assertCurrentLibrarySnapshotRoundTrip(t, uri, path, index)
		})
	}
}

func currentSnapshotPaths(t *testing.T, root string) ([]string, []string) {
	t.Helper()
	latestTrips := make(map[string]currentTripSnapshotCandidate)
	var latestLibrary *currentLibrarySnapshotCandidate
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || strings.ToLower(filepath.Ext(path)) != ".json" {
			return nil
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		identity, isTrip, err := parseCurrentTripSnapshotIdentity(raw)
		if err != nil {
			return fmt.Errorf("读取快照身份 %s 失败: %w", path, err)
		}
		if isTrip {
			candidate := currentTripSnapshotCandidate{
				Path:                        path,
				currentTripSnapshotIdentity: identity,
			}
			current, exists := latestTrips[identity.TripID]
			if !exists || newerCurrentTripSnapshot(candidate, current) {
				latestTrips[identity.TripID] = candidate
			}
			return nil
		}
		libraryIdentity, isLibrary, err := parseCurrentLibrarySnapshotIdentity(raw)
		if err != nil {
			return fmt.Errorf("读取基础库快照身份 %s 失败: %w", path, err)
		}
		if isLibrary {
			candidate := currentLibrarySnapshotCandidate{
				Path:                           path,
				currentLibrarySnapshotIdentity: libraryIdentity,
			}
			if latestLibrary == nil ||
				newerCurrentLibrarySnapshot(candidate, *latestLibrary) {
				latestLibrary = &candidate
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("扫描本地快照失败：%v", err)
	}
	tripPaths := make([]string, 0, len(latestTrips))
	for _, candidate := range latestTrips {
		tripPaths = append(tripPaths, candidate.Path)
	}
	libraryPaths := make([]string, 0, 1)
	if latestLibrary != nil {
		libraryPaths = append(libraryPaths, latestLibrary.Path)
	}
	sort.Strings(tripPaths)
	return tripPaths, libraryPaths
}

func parseCurrentTripSnapshotIdentity(
	raw []byte,
) (currentTripSnapshotIdentity, bool, error) {
	var probe struct {
		Trip json.RawMessage `json:"trip"`
	}
	if err := json.Unmarshal(raw, &probe); err != nil {
		return currentTripSnapshotIdentity{}, false, err
	}
	if len(probe.Trip) == 0 {
		return currentTripSnapshotIdentity{}, false, nil
	}
	var tripProbe struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(probe.Trip, &tripProbe); err != nil {
		return currentTripSnapshotIdentity{}, false, err
	}
	if strings.TrimSpace(tripProbe.ID) == "" {
		return currentTripSnapshotIdentity{}, false, fmt.Errorf(
			"Trip 快照缺少 trip.id",
		)
	}
	var envelope struct {
		ExportedAt time.Time `json:"exportedAt"`
		Revision   int64     `json:"revision"`
		Trip       struct {
			ID      string `json:"id"`
			Version int64  `json:"version"`
		} `json:"trip"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return currentTripSnapshotIdentity{}, false, err
	}
	if !currentSnapshotTripIDPattern.MatchString(envelope.Trip.ID) {
		return currentTripSnapshotIdentity{}, false, fmt.Errorf(
			"Trip ID 不是安全 slug: %q",
			envelope.Trip.ID,
		)
	}
	if envelope.ExportedAt.IsZero() {
		return currentTripSnapshotIdentity{}, false, fmt.Errorf(
			"Trip %s 缺少 exportedAt",
			envelope.Trip.ID,
		)
	}
	if envelope.Revision < 1 || envelope.Trip.Version != envelope.Revision {
		return currentTripSnapshotIdentity{}, false, fmt.Errorf(
			"Trip %s 的 revision 与 trip.version 不一致或无效",
			envelope.Trip.ID,
		)
	}
	return currentTripSnapshotIdentity{
		TripID:     envelope.Trip.ID,
		ExportedAt: envelope.ExportedAt,
		Revision:   envelope.Revision,
	}, true, nil
}

func newerCurrentTripSnapshot(
	candidate, current currentTripSnapshotCandidate,
) bool {
	if !candidate.ExportedAt.Equal(current.ExportedAt) {
		return candidate.ExportedAt.After(current.ExportedAt)
	}
	if candidate.Revision != current.Revision {
		return candidate.Revision > current.Revision
	}
	return preferCurrentSnapshotPath(candidate.Path, current.Path)
}

func parseCurrentLibrarySnapshotIdentity(
	raw []byte,
) (currentLibrarySnapshotIdentity, bool, error) {
	var probe struct {
		Datasets map[string]json.RawMessage `json:"datasets"`
	}
	if err := json.Unmarshal(raw, &probe); err != nil {
		return currentLibrarySnapshotIdentity{}, false, nil
	}
	_, hasMembers := probe.Datasets[string(model.LibraryMembers)]
	_, hasTags := probe.Datasets[string(model.LibraryTags)]
	if !hasMembers || !hasTags {
		return currentLibrarySnapshotIdentity{}, false, nil
	}
	var envelope struct {
		ExportedAt time.Time `json:"exportedAt"`
		Revision   int64     `json:"revision"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return currentLibrarySnapshotIdentity{}, false, err
	}
	if envelope.ExportedAt.IsZero() {
		return currentLibrarySnapshotIdentity{}, false, fmt.Errorf(
			"基础库快照缺少 exportedAt",
		)
	}
	if envelope.Revision < 1 {
		return currentLibrarySnapshotIdentity{}, false, fmt.Errorf(
			"基础库快照 revision 无效",
		)
	}
	return currentLibrarySnapshotIdentity{
		ExportedAt: envelope.ExportedAt,
		Revision:   envelope.Revision,
	}, true, nil
}

func newerCurrentLibrarySnapshot(
	candidate, current currentLibrarySnapshotCandidate,
) bool {
	if !candidate.ExportedAt.Equal(current.ExportedAt) {
		return candidate.ExportedAt.After(current.ExportedAt)
	}
	if candidate.Revision != current.Revision {
		return candidate.Revision > current.Revision
	}
	return preferCurrentSnapshotPath(candidate.Path, current.Path)
}

func preferCurrentSnapshotPath(candidate, current string) bool {
	priority := func(path string) int {
		switch filepath.Base(path) {
		case "working.json":
			return 2
		case "readonly.json":
			return 1
		default:
			return 0
		}
	}
	candidatePriority, currentPriority := priority(candidate), priority(current)
	if candidatePriority != currentPriority {
		return candidatePriority > currentPriority
	}
	return candidate > current
}

func latestCurrentLibrarySnapshot(
	t *testing.T,
	paths []string,
) model.LibrarySnapshotPayload {
	t.Helper()
	var latest model.LibrarySnapshotPayload
	for _, path := range paths {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal("读取本地基础库快照失败")
		}
		payload, err := repository.ValidateLibrarySnapshotPayload(raw)
		if err != nil {
			t.Fatalf("本地基础库快照不符合当前领域契约：%v", err)
		}
		if latest.Revision == 0 ||
			payload.ExportedAt.After(latest.ExportedAt) ||
			(payload.ExportedAt.Equal(latest.ExportedAt) &&
				payload.Revision > latest.Revision) {
			latest = payload
		}
	}
	return latest
}

func assertCurrentSnapshotRoundTrip(
	t *testing.T,
	uri, path string,
	index int,
	library model.LibrarySnapshotPayload,
) {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal("读取本地快照失败")
	}
	var identity struct {
		Trip struct {
			ID string `json:"id"`
		} `json:"trip"`
	}
	if err := json.Unmarshal(raw, &identity); err != nil {
		t.Fatal("本地快照不是 JSON 对象")
	}
	payload, err := repository.ValidateSnapshotPayload(raw, identity.Trip.ID)
	if err != nil {
		t.Fatalf("本地快照不符合当前领域契约：%v", err)
	}

	database := fmt.Sprintf("my_travel_current_snapshot_%d_%d", time.Now().UnixNano(), index)
	config := app.Config{MongoURI: uri, MongoDatabase: database}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	repo, err := repository.Connect(ctx, config)
	if err != nil {
		t.Fatal("无法连接一次性 MongoDB")
	}
	defer repo.Close(context.Background())
	client, err := mongo.Connect(options.Client().ApplyURI(uri))
	if err != nil {
		t.Fatal("无法建立快照写入连接")
	}
	defer client.Disconnect(context.Background())
	defer client.Database(database).Drop(context.Background())

	if err := repo.Initialize(ctx); err != nil {
		t.Fatalf("无法安装当前 MongoDB 契约：%v", err)
	}
	db := client.Database(database)
	installCurrentLibrarySnapshot(t, ctx, repo, db, library)
	if _, err := db.Collection("trips").InsertOne(ctx, payload.Trip); err != nil {
		t.Fatal("真实 Trip 快照被当前 MongoDB validator 拒绝")
	}
	for resource, dataset := range payload.Datasets {
		rows := make([]any, 0, len(dataset))
		for _, row := range dataset {
			row.GroupKey = currentSnapshotGroupKey(row)
			rows = append(rows, row)
		}
		if len(rows) == 0 {
			continue
		}
		if _, err := db.Collection("records").InsertMany(ctx, rows); err != nil {
			t.Fatalf("真实快照资源 %s 被当前 MongoDB validator 或索引拒绝", resource)
		}
	}
	for _, member := range payload.Datasets[string(model.Members)] {
		memberID := strings.TrimSpace(fmt.Sprint(member.Data["memberId"]))
		role := strings.TrimSpace(fmt.Sprint(member.Data["role"]))
		if role != "owner" && role != "editor" && role != "viewer" {
			role = "editor"
		}
		if err := repo.SetMembership(ctx, payload.Trip.ID, memberID, role); err != nil {
			t.Fatalf("真实快照成员访问关系无法建立：%v", err)
		}
	}

	rebuilt, err := repo.BuildSnapshot(ctx, payload.Trip.ID, "contract-roundtrip")
	if err != nil {
		t.Fatalf("当前 MongoDB 数据无法重新生成完整快照：%v", err)
	}
	if rebuilt.ContentChecksum != payload.ContentChecksum {
		t.Fatalf(
			"真实快照往返语义校验和变化：原始 %s，往返 %s",
			payload.ContentChecksum,
			rebuilt.ContentChecksum,
		)
	}
	rebuiltPayload, err := repository.ValidateSnapshotPayload(rebuilt.Payload, payload.Trip.ID)
	if err != nil {
		t.Fatalf("往返快照不符合当前领域契约：%v", err)
	}
	// 逻辑删除的 Trip 仍须保留可校验、可往返的完整快照，但业务查询会刻意隐藏它，
	// 因而不具备在线恢复预览语义。上面的内容校验和已经证明其数据没有变化。
	if payload.Trip.DeletedAt != nil {
		return
	}
	diff, err := repo.DiffSnapshot(ctx, payload.Trip.ID, rebuiltPayload)
	if err != nil {
		t.Fatalf("往返快照无法执行服务端语义预览：%v", err)
	}
	if len(diff.TripChanges) != 0 {
		t.Fatalf("往返快照误报 Trip 字段变化：%#v", diff.TripChanges)
	}
	for _, resource := range model.Resources() {
		resourceDiff := diff.Resources[string(resource)]
		if resourceDiff.Created != 0 ||
			resourceDiff.Updated != 0 ||
			resourceDiff.Deleted != 0 {
			t.Fatalf("往返快照误报 %s 资源变化：%#v", resource, resourceDiff)
		}
	}
}

func currentSnapshotGroupKey(row model.Record) string {
	if row.Resource == model.Segments {
		return strings.TrimSpace(fmt.Sprint(row.Data["dayId"]))
	}
	return ""
}

func assertCurrentLibrarySnapshotRoundTrip(t *testing.T, uri, path string, index int) {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal("读取本地基础库快照失败")
	}
	payload, err := repository.ValidateLibrarySnapshotPayload(raw)
	if err != nil {
		t.Fatalf("本地基础库快照不符合当前领域契约：%v", err)
	}

	database := fmt.Sprintf("my_travel_current_library_%d_%d", time.Now().UnixNano(), index)
	config := app.Config{MongoURI: uri, MongoDatabase: database}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	repo, err := repository.Connect(ctx, config)
	if err != nil {
		t.Fatal("无法连接一次性 MongoDB")
	}
	defer repo.Close(context.Background())
	client, err := mongo.Connect(options.Client().ApplyURI(uri))
	if err != nil {
		t.Fatal("无法建立基础库快照写入连接")
	}
	defer client.Disconnect(context.Background())
	defer client.Database(database).Drop(context.Background())

	if err := repo.Initialize(ctx); err != nil {
		t.Fatalf("无法安装当前 MongoDB 契约：%v", err)
	}
	db := client.Database(database)
	installCurrentLibrarySnapshot(t, ctx, repo, db, payload)

	rebuilt, err := repo.BuildLibrarySnapshot(ctx, "contract-roundtrip")
	if err != nil {
		t.Fatalf("当前 MongoDB 数据无法重新生成基础库快照：%v", err)
	}
	if rebuilt.ContentChecksum != payload.ContentChecksum {
		t.Fatalf(
			"真实基础库快照往返语义校验和变化：原始 %s，往返 %s",
			payload.ContentChecksum,
			rebuilt.ContentChecksum,
		)
	}
	if _, err := repository.ValidateLibrarySnapshotPayload(rebuilt.Payload); err != nil {
		t.Fatalf("往返基础库快照不符合当前领域契约：%v", err)
	}
}

func installCurrentLibrarySnapshot(
	t *testing.T,
	ctx context.Context,
	repo *repository.Repository,
	db *mongo.Database,
	payload model.LibrarySnapshotPayload,
) {
	t.Helper()
	state, err := repo.GetLibraryState(ctx)
	if err != nil {
		t.Fatal("无法读取一次性基础库状态")
	}
	state.Version = payload.Revision
	state.UpdatedAt = payload.ExportedAt
	if _, err := db.Collection("library_state").ReplaceOne(
		ctx,
		map[string]any{"_id": state.ID},
		state,
	); err != nil {
		t.Fatal("真实基础库状态被当前 MongoDB validator 拒绝")
	}
	for resource, dataset := range payload.Datasets {
		rows := make([]any, 0, len(dataset))
		for _, row := range dataset {
			rows = append(rows, row)
		}
		if len(rows) == 0 {
			continue
		}
		if _, err := db.Collection("library_records").InsertMany(ctx, rows); err != nil {
			t.Fatalf("真实基础资料 %s 被当前 MongoDB validator 或索引拒绝", resource)
		}
	}
}
