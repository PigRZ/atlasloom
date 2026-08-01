import { describe, expect, it } from "vitest";
import {
  libraryDeletionBlockedMessage,
  libraryReferenceLines,
  memberCredentialDeletionConfirmation,
  tagDeletionConfirmation,
  tripMemberCascadeConfirmation,
  tripMemberDeletionBlockedMessage,
} from "./deletionImpact";

describe("deletion impact copy", () => {
  it("explains the exact trip, record and relation, including deleted trips", () => {
    const lines = libraryReferenceLines([{
      tripId: "trip-a",
      tripTitle: "山海旅行",
      tripDeleted: true,
      resource: "places",
      recordId: "place-a",
      label: "示例观景点",
      relations: ["place-type"]
    }]);

    expect(lines).toContain("山海旅行（已删除旅行）");
    expect(lines).toContain("地点 / 示例观景点（地点类型）");
  });

  it("directs active login members to disable before deleting", () => {
    const message = libraryDeletionBlockedMessage("成员", "同行成员", {
      references: [],
      hasLoginCredential: true,
      hasTripOwnership: false,
      canDeleteWithCredential: false,
      canReplaceWithFallback: false
    });

    expect(message).toContain("绑定登录账号");
    expect(message).toContain("将成员停用");
  });

  it("explains the irreversible credential revocation for an inactive member", () => {
    const message = memberCredentialDeletionConfirmation("待删除成员");

    expect(message).toContain("注销其登录凭据");
    expect(message).toContain("清除全部会话");
    expect(message).toContain("历史操作记录仍会保留");
  });

  it("explains fallback replacement without implying business data deletion", () => {
    const message = tagDeletionConfirmation("酒店", {
      references: [{
        tripId: "trip-a",
        tripTitle: "测试旅行",
        tripDeleted: false,
        resource: "places",
        recordId: "place-a",
        label: "测试酒店",
        relations: ["place-type"]
      }],
      hasLoginCredential: false,
      hasTripOwnership: false,
      canDeleteWithCredential: false,
      canReplaceWithFallback: true
    });

    expect(message).toContain("改用同分类的“其他”标签");
    expect(message).toContain("原业务记录不会被删除");
  });

  it("lists shared roles but redacts private packing details", () => {
    const message = tripMemberDeletionBlockedMessage("同行成员", {
      references: [{
        resource: "research-evidence",
        recordId: "research-a",
        label: "景区攻略",
        relations: ["research-recorder"]
      }],
      cascadeExpenses: [{
        resource: "expenses",
        recordId: "expense-a",
        label: "晚餐",
        relations: ["expense-payer", "expense-split"]
      }],
      hasPrivatePackingItems: true
    });

    expect(message).toContain("资料“景区攻略”（资料记录人）");
    expect(message).toContain("费用“晚餐”（付款人、分摊成员）");
    expect(message).toContain("不展示物品名称和数量");
    expect(message).toContain("一次确认中删除成员");
    expect(message).not.toContain("隐私物品");
  });

  it("confirms exact manual expenses and redacts private item details", () => {
    const message = tripMemberCascadeConfirmation("同行成员", {
      references: [],
      cascadeExpenses: [{
        resource: "expenses",
        recordId: "expense-a",
        label: "晚餐",
        relations: ["expense-payer"]
      }],
      hasPrivatePackingItems: true
    });

    expect(message).toContain("费用“晚餐”（付款人）");
    expect(message).toContain("全部个人物品");
    expect(message).toContain("移除同行成员关系");
    expect(message).toContain("不展示物品名称和数量");
    expect(message).not.toContain("隐私物品");
  });
});
