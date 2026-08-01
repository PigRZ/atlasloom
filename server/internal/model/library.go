// 基础库模型定义跨旅行复用的成员资料与分类标签。
package model

import (
	"fmt"
	"regexp"
	"strings"
	"time"
)

type LibraryResource string

const (
	LibraryMembers LibraryResource = "members"
	LibraryTags    LibraryResource = "tags"

	TagCategoryGeneral      = "general"
	TagCategoryPlaceType    = "place-type"
	TagCategoryExpenseType  = "expense-type"
	TagCategoryPlatformType = "platform-type"
)

var mobilePattern = regexp.MustCompile(`^[0-9]{11}$`)

type TagPreset struct {
	RecordKey string
	Name      string
	Category  string
	Color     string
}

var platformTagPresets = []TagPreset{
	{RecordKey: "platform-meituan", Name: "美团", Category: TagCategoryPlatformType, Color: "#F5C400"},
	{RecordKey: "platform-ctrip", Name: "携程", Category: TagCategoryPlatformType, Color: "#2577E3"},
	{RecordKey: "platform-fliggy", Name: "飞猪", Category: TagCategoryPlatformType, Color: "#FF5A1F"},
	{RecordKey: "platform-amap", Name: "高德", Category: TagCategoryPlatformType, Color: "#1677FF"},
	{RecordKey: "platform-alipay", Name: "支付宝", Category: TagCategoryPlatformType, Color: "#1677FF"},
	{RecordKey: "platform-scenic-official", Name: "景区官方", Category: TagCategoryPlatformType, Color: "#2D6854"},
	{RecordKey: "platform-12306", Name: "12306", Category: TagCategoryPlatformType, Color: "#1685A9"},
	{RecordKey: "platform-xiaohongshu", Name: "小红书", Category: TagCategoryPlatformType, Color: "#FF2442"},
	{RecordKey: "platform-company-benefit", Name: "公司内购", Category: TagCategoryPlatformType, Color: "#7C5DAA"},
	{RecordKey: "platform-hworld", Name: "华住会", Category: TagCategoryPlatformType, Color: "#9B744B"},
}

var travelTypeTagPresets = []TagPreset{
	{RecordKey: "place-scenic", Name: "景点", Category: TagCategoryPlaceType, Color: "#2D6854"},
	{RecordKey: "place-hotel", Name: "住宿", Category: TagCategoryPlaceType, Color: "#6B6FA9"},
	{RecordKey: "place-food", Name: "餐饮", Category: TagCategoryPlaceType, Color: "#C8793F"},
	{RecordKey: "place-transport-hub", Name: "交通枢纽", Category: TagCategoryPlaceType, Color: "#397B91"},
	{RecordKey: "place-scenic-entry", Name: "景区入口", Category: TagCategoryPlaceType, Color: "#4D8B68"},
	{RecordKey: "place-viewpoint", Name: "观景点", Category: TagCategoryPlaceType, Color: "#4F8F8B"},
	{RecordKey: "place-meeting", Name: "集合点", Category: TagCategoryPlaceType, Color: "#8B6B50"},
	{RecordKey: "place-supply", Name: "补给点", Category: TagCategoryPlaceType, Color: "#78904B"},
	{RecordKey: "place-parking", Name: "停车点", Category: TagCategoryPlaceType, Color: "#596B7B"},
	{RecordKey: "place-service-area", Name: "服务区", Category: TagCategoryPlaceType, Color: "#6C7D74"},
	{RecordKey: "place-shopping", Name: "购物", Category: TagCategoryPlaceType, Color: "#9B5F7F"},
	{RecordKey: "place-medical", Name: "医疗", Category: TagCategoryPlaceType, Color: "#B45555"},
	{RecordKey: "place-other", Name: "其他", Category: TagCategoryPlaceType, Color: "#718079"},
	{RecordKey: "expense-transport", Name: "交通", Category: TagCategoryExpenseType, Color: "#397B91"},
	{RecordKey: "expense-hotel", Name: "住宿", Category: TagCategoryExpenseType, Color: "#6B6FA9"},
	{RecordKey: "expense-car-rental", Name: "租车", Category: TagCategoryExpenseType, Color: "#596B7B"},
	{RecordKey: "expense-energy", Name: "油费/充电", Category: TagCategoryExpenseType, Color: "#B27A3D"},
	{RecordKey: "expense-road", Name: "停车/过路", Category: TagCategoryExpenseType, Color: "#6C7D74"},
	{RecordKey: "expense-ticket", Name: "门票", Category: TagCategoryExpenseType, Color: "#4D8B68"},
	{RecordKey: "expense-meals", Name: "三餐", Category: TagCategoryExpenseType, Color: "#C8793F"},
	{RecordKey: "expense-activity", Name: "活动", Category: TagCategoryExpenseType, Color: "#4F8F8B"},
	{RecordKey: "expense-shopping", Name: "购物", Category: TagCategoryExpenseType, Color: "#9B5F7F"},
	{RecordKey: "expense-supplies", Name: "物资", Category: TagCategoryExpenseType, Color: "#78904B"},
	{RecordKey: "expense-insurance", Name: "保险", Category: TagCategoryExpenseType, Color: "#5B7198"},
	{RecordKey: "expense-communication", Name: "通讯", Category: TagCategoryExpenseType, Color: "#5D7791"},
	{RecordKey: "expense-medical", Name: "医疗", Category: TagCategoryExpenseType, Color: "#B45555"},
	{RecordKey: "expense-service", Name: "服务费", Category: TagCategoryExpenseType, Color: "#8B6B50"},
	{RecordKey: "expense-emergency", Name: "应急", Category: TagCategoryExpenseType, Color: "#A85D47"},
	{RecordKey: "expense-other", Name: "其他", Category: TagCategoryExpenseType, Color: "#718079"},
}

func PlatformTagPresets() []TagPreset {
	return append([]TagPreset(nil), platformTagPresets...)
}

func TravelTypeTagPresets() []TagPreset {
	return append([]TagPreset(nil), travelTypeTagPresets...)
}

func DefaultTagPresets() []TagPreset {
	result := make([]TagPreset, 0, len(platformTagPresets)+len(travelTypeTagPresets))
	result = append(result, platformTagPresets...)
	return append(result, travelTypeTagPresets...)
}

func NormalizeMobile(value string, required bool) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" && !required {
		return "", nil
	}
	if !mobilePattern.MatchString(value) {
		return "", fmt.Errorf("手机号必须是 11 位数字")
	}
	return value, nil
}

func LibraryResources() []LibraryResource {
	return []LibraryResource{
		LibraryMembers,
		LibraryTags,
	}
}

func ParseLibraryResource(value string) (LibraryResource, error) {
	resource := LibraryResource(value)
	for _, candidate := range LibraryResources() {
		if resource == candidate {
			return resource, nil
		}
	}
	return "", fmt.Errorf("unknown library resource: %s", value)
}

type LibraryState struct {
	ID             string    `bson:"_id" json:"id"`
	Version        int64     `bson:"version" json:"version"`
	ReferenceEpoch int64     `bson:"reference_epoch,omitempty" json:"-"`
	UpdatedAt      time.Time `bson:"updated_at" json:"updatedAt"`
}

type LibraryRecord struct {
	ID        string          `bson:"_id" json:"id"`
	Resource  LibraryResource `bson:"resource" json:"resource"`
	RecordKey string          `bson:"record_key" json:"recordKey"`
	Position  int64           `bson:"position" json:"position"`
	Version   int64           `bson:"version" json:"version"`
	Data      map[string]any  `bson:"data" json:"data"`
	CreatedAt time.Time       `bson:"created_at" json:"createdAt"`
	UpdatedAt time.Time       `bson:"updated_at" json:"updatedAt"`
}

type LibrarySnapshot struct {
	ID              string    `bson:"_id" json:"id"`
	Revision        int64     `bson:"revision" json:"revision"`
	Reason          string    `bson:"reason" json:"reason"`
	Payload         []byte    `bson:"payload" json:"-"`
	SHA256          string    `bson:"sha256" json:"sha256"`
	ContentChecksum string    `bson:"content_checksum" json:"contentChecksum"`
	MirrorState     string    `bson:"mirror_state" json:"mirrorState"`
	CreatedAt       time.Time `bson:"created_at" json:"createdAt"`
}

type LibrarySnapshotPayload struct {
	ExportedAt      time.Time                  `json:"exportedAt"`
	Reason          string                     `json:"reason"`
	Revision        int64                      `json:"revision"`
	ContentChecksum string                     `json:"contentChecksum"`
	Datasets        map[string][]LibraryRecord `json:"datasets"`
}

func (p LibrarySnapshotPayload) ComputedContentChecksum() (string, error) {
	content := struct {
		Revision int64                      `json:"revision"`
		Datasets map[string][]LibraryRecord `json:"datasets"`
	}{p.Revision, p.Datasets}
	return canonicalJSONChecksum(content)
}

func ValidateLibraryRecordData(resource LibraryResource, data map[string]any) error {
	if len(data) == 0 {
		return fmt.Errorf("data is required")
	}
	allowed := map[LibraryResource]map[string]struct{}{
		LibraryMembers: fieldSet(
			"displayName", "mobile", "alternateContact", "emergencyContact",
			"admin", "status", "note",
		),
		LibraryTags: fieldSet("name", "category", "color", "status"),
	}
	fields, ok := allowed[resource]
	if !ok {
		return fmt.Errorf("unknown library resource: %s", resource)
	}
	for field := range data {
		if _, exists := fields[field]; !exists {
			return fmt.Errorf("%s is not allowed for %s", field, resource)
		}
	}
	required := "name"
	if resource == LibraryMembers {
		required = "displayName"
	}
	if strings.TrimSpace(fmt.Sprint(data[required])) == "" {
		return fmt.Errorf("%s is required", required)
	}
	for field, value := range data {
		switch field {
		case "displayName", "mobile", "alternateContact", "emergencyContact",
			"status", "note", "name", "category", "color":
			if _, ok := value.(string); !ok {
				return fmt.Errorf("%s must be a string", field)
			}
		case "admin":
			if resource != LibraryMembers {
				return fmt.Errorf("admin is only allowed for members")
			}
			if _, ok := value.(bool); !ok {
				return fmt.Errorf("admin must be a boolean")
			}
		}
	}
	if resource == LibraryMembers {
		if raw, exists := data["mobile"]; exists {
			mobile, ok := raw.(string)
			if !ok {
				return fmt.Errorf("mobile must be a string")
			}
			if _, err := NormalizeMobile(mobile, false); err != nil {
				return err
			}
		}
	}
	if resource == LibraryTags {
		category := strings.TrimSpace(fmt.Sprint(data["category"]))
		if category != TagCategoryGeneral &&
			category != TagCategoryPlaceType &&
			category != TagCategoryExpenseType &&
			category != TagCategoryPlatformType {
			return fmt.Errorf("invalid tag category")
		}
	}
	if rawStatus, exists := data["status"]; exists && rawStatus != nil {
		status := strings.TrimSpace(fmt.Sprint(rawStatus))
		if status != "" && status != "active" && status != "inactive" {
			return fmt.Errorf("invalid status")
		}
	}
	return nil
}
