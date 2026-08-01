// 基础库模型测试固定无登录凭据的成员可以省略手机号，并校验联系方式格式。
package model

import (
	"reflect"
	"testing"
)

func TestPlatformTagPresetsAreStableAndComplete(t *testing.T) {
	var names []string
	keys := map[string]struct{}{}
	for _, preset := range PlatformTagPresets() {
		names = append(names, preset.Name)
		if preset.RecordKey == "" || preset.Color == "" {
			t.Fatalf("incomplete platform preset: %#v", preset)
		}
		if preset.Category != TagCategoryPlatformType {
			t.Fatalf("unexpected platform preset category: %#v", preset)
		}
		if _, duplicate := keys[preset.RecordKey]; duplicate {
			t.Fatalf("duplicate platform preset key: %s", preset.RecordKey)
		}
		keys[preset.RecordKey] = struct{}{}
	}
	want := []string{
		"美团", "携程", "飞猪", "高德", "支付宝",
		"景区官方", "12306", "小红书", "公司内购", "华住会",
	}
	if !reflect.DeepEqual(names, want) {
		t.Fatalf("platform presets=%#v, want %#v", names, want)
	}
}

func TestTravelTypeTagPresetsCoverCommonPlaceAndExpenseTypes(t *testing.T) {
	names := map[string]map[string]struct{}{
		TagCategoryPlaceType:   {},
		TagCategoryExpenseType: {},
	}
	keys := map[string]struct{}{}
	for _, preset := range TravelTypeTagPresets() {
		if preset.RecordKey == "" || preset.Name == "" || preset.Color == "" {
			t.Fatalf("incomplete travel type preset: %#v", preset)
		}
		if _, ok := names[preset.Category]; !ok {
			t.Fatalf("unexpected travel type category: %#v", preset)
		}
		if _, duplicate := keys[preset.RecordKey]; duplicate {
			t.Fatalf("duplicate travel type preset key: %s", preset.RecordKey)
		}
		keys[preset.RecordKey] = struct{}{}
		names[preset.Category][preset.Name] = struct{}{}
	}
	for _, required := range []struct {
		category string
		name     string
	}{
		{TagCategoryPlaceType, "餐饮"},
		{TagCategoryPlaceType, "交通枢纽"},
		{TagCategoryPlaceType, "医疗"},
		{TagCategoryExpenseType, "油费/充电"},
		{TagCategoryExpenseType, "保险"},
		{TagCategoryExpenseType, "应急"},
	} {
		if _, ok := names[required.category][required.name]; !ok {
			t.Fatalf("missing %s preset %q", required.category, required.name)
		}
	}
}

func TestValidateLibraryMemberAllowsMissingMobile(t *testing.T) {
	err := ValidateLibraryRecordData(LibraryMembers, map[string]any{
		"displayName": "未绑定成员",
		"status":      "active",
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestValidateLibraryMemberAcceptsContactFields(t *testing.T) {
	err := ValidateLibraryRecordData(LibraryMembers, map[string]any{
		"displayName":      "示例成员",
		"mobile":           "13800138000",
		"alternateContact": "微信同号",
		"emergencyContact": "家人 13800138000",
		"admin":            true,
		"status":           "active",
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestValidateLibraryMemberRejectsInvalidMobile(t *testing.T) {
	for name, data := range map[string]map[string]any{
		"不足 11 位": {
			"displayName": "成员", "mobile": "1590500243", "status": "active",
		},
		"包含非数字": {
			"displayName": "成员", "mobile": "1590500243x", "status": "active",
		},
	} {
		t.Run(name, func(t *testing.T) {
			if err := ValidateLibraryRecordData(LibraryMembers, data); err == nil {
				t.Fatal("期望校验失败")
			}
		})
	}
}
