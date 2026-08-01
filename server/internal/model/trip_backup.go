// 旅行备份模型只描述当前 Trip 业务状态及其必要的基础资料引用。
package model

import "time"

const TripBackupKind = "my-travel-trip-backup"

type TripBackup struct {
	Kind            string                 `json:"kind"`
	ExportedAt      time.Time              `json:"exportedAt"`
	ContentChecksum string                 `json:"contentChecksum"`
	Snapshot        SnapshotPayload        `json:"snapshot"`
	Dependencies    TripBackupDependencies `json:"dependencies"`
}

type TripBackupDependencies struct {
	Members []TripBackupMemberDependency `json:"members"`
	Tags    []TripBackupTagDependency    `json:"tags"`
}

type TripBackupMemberDependency struct {
	SourceID    string `json:"sourceId"`
	DisplayName string `json:"displayName"`
}

type TripBackupTagDependency struct {
	SourceID string `json:"sourceId"`
	Name     string `json:"name"`
	Category string `json:"category"`
	Color    string `json:"color,omitempty"`
}

func (b TripBackup) ComputedContentChecksum() (string, error) {
	content := struct {
		Kind         string                 `json:"kind"`
		Snapshot     SnapshotPayload        `json:"snapshot"`
		Dependencies TripBackupDependencies `json:"dependencies"`
	}{
		Kind:         b.Kind,
		Snapshot:     b.Snapshot,
		Dependencies: b.Dependencies,
	}
	return canonicalJSONChecksum(content)
}
