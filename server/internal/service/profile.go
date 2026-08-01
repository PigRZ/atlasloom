// 个人资料服务允许登录成员修改自己的公开资料和登录凭据，不改变现有会话。
package service

import (
	"context"
	"fmt"
	"strings"

	"my-travel/server/internal/auth"
	"my-travel/server/internal/model"
)

type ProfileUpdateInput struct {
	DisplayName      string `json:"displayName"`
	Mobile           string `json:"mobile"`
	AlternateContact string `json:"alternateContact"`
	EmergencyContact string `json:"emergencyContact"`
	CurrentPassword  string `json:"currentPassword"`
	NewPassword      string `json:"newPassword"`
}

func (s Service) UpdateOwnProfile(
	ctx context.Context,
	memberID string,
	version int64,
	input ProfileUpdateInput,
) (model.LibraryRecord, model.MemberIdentity, model.LibrarySnapshot, error) {
	var updated model.LibraryRecord
	var identity model.MemberIdentity
	var item model.LibrarySnapshot
	err := s.Repo.WithTransaction(ctx, func(tx context.Context) error {
		if err := s.requireActiveActor(tx, memberID); err != nil {
			return err
		}
		current, err := s.Repo.GetLibraryRecord(tx, model.LibraryMembers, memberID)
		if err != nil {
			return err
		}
		mobile, err := model.NormalizeMobile(input.Mobile, true)
		if err != nil {
			return err
		}
		currentMobile := strings.TrimSpace(fmt.Sprint(current.Data["mobile"]))
		credentialChange := mobile != currentMobile || input.NewPassword != ""
		if credentialChange {
			if input.CurrentPassword == "" {
				return fmt.Errorf("修改手机号或密码前请输入当前密码")
			}
			credential, err := s.Repo.GetMemberCredential(tx, memberID)
			if err != nil || !auth.VerifyPassword(
				credential.PasswordHash,
				input.CurrentPassword,
			) {
				return fmt.Errorf("当前密码不正确")
			}
		}
		admin, _ := current.Data["admin"].(bool)
		status := strings.TrimSpace(fmt.Sprint(current.Data["status"]))
		if status == "" || status == "<nil>" {
			status = "active"
		}
		data := map[string]any{
			"displayName":      strings.TrimSpace(input.DisplayName),
			"mobile":           mobile,
			"alternateContact": strings.TrimSpace(input.AlternateContact),
			"emergencyContact": strings.TrimSpace(input.EmergencyContact),
			"admin":            admin,
			"status":           status,
		}
		if note := strings.TrimSpace(fmt.Sprint(current.Data["note"])); note != "" &&
			note != "<nil>" {
			data["note"] = note
		}
		updated, err = s.Repo.UpdateLibraryRecord(
			tx,
			model.LibraryMembers,
			memberID,
			version,
			data,
		)
		if err != nil {
			return err
		}
		if input.NewPassword != "" {
			hash, err := auth.HashPassword(input.NewPassword)
			if err != nil {
				return err
			}
			if err := s.Repo.SetMemberCredential(tx, memberID, hash); err != nil {
				return err
			}
		}
		if err := s.Repo.InsertAuditLog(tx, model.AuditLog{
			MemberID: memberID, Module: "个人资料", Action: "修改",
			Resource: string(model.LibraryMembers), RecordID: memberID,
			RecordLabel: strings.TrimSpace(input.DisplayName),
			Before:      cloneMap(current.Data),
			After:       cloneMap(updated.Data),
		}); err != nil {
			return err
		}
		if _, err := s.Repo.BumpLibraryRevision(tx); err != nil {
			return err
		}
		item, err = s.Repo.BuildLibrarySnapshot(tx, "profile-updated")
		if err != nil {
			return err
		}
		identity, err = s.Repo.GetMemberIdentity(tx, memberID)
		return err
	})
	if err != nil {
		return model.LibraryRecord{}, model.MemberIdentity{}, model.LibrarySnapshot{}, err
	}
	s.mirrorLibrary(ctx, &item)
	return updated, identity, item, nil
}
