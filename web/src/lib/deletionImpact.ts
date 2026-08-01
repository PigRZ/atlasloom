// 删除影响文案把稳定接口代码转成可操作的中文位置说明。
import type {
  LibraryDeletionImpact,
  LibraryReference,
  TripMemberDeletionImpact,
  TripMemberReference
} from "./types";

const visibleReferenceLimit = 8;

const resourceLabels: Record<string, string> = {
  days: "执行日",
  places: "地点",
  segments: "交通段",
  bookings: "预订",
  expenses: "费用",
  "packing-items": "物品",
  "research-evidence": "资料",
  members: "旅行成员"
};

const relationLabels: Record<string, string> = {
  "trip-member": "旅行成员",
  "booking-booker": "预订人",
  "research-recorder": "资料记录人",
  "record-creator": "创建人",
  "record-updater": "最后修改人",
  "place-type": "地点类型",
  "booking-type": "预订类型",
  "expense-type": "费用类型",
  "booking-platform": "预订平台",
  "research-platform": "资料平台",
  "expense-payer": "付款人",
  "expense-split": "分摊成员",
  "packing-owner": "个人物品所有人"
};

function relationText(relations: string[]): string {
  return relations.map((relation) => relationLabels[relation] ?? "关联资料").join("、");
}

export function libraryReferenceLines(references: LibraryReference[]): string {
  const lines = references.slice(0, visibleReferenceLimit).map((reference) => {
    const trip = reference.tripDeleted
      ? `${reference.tripTitle}（已删除旅行）`
      : reference.tripTitle;
    return `• ${trip} / ${resourceLabels[reference.resource] ?? "数据"} / ${reference.label}（${relationText(reference.relations)}）`;
  });
  if (references.length > lines.length) {
    lines.push(`• 另有 ${references.length - lines.length} 条引用`);
  }
  return lines.join("\n");
}

export function libraryDeletionBlockedMessage(
  singular: "成员" | "标签",
  name: string,
  impact: LibraryDeletionImpact
): string {
  const sections = [`${singular}“${name}”现在不能直接删除。`];
  if (impact.references.length) {
    sections.push(
      `以下 ${impact.references.length} 条资料仍在使用：`,
      libraryReferenceLines(impact.references)
    );
  }
  if (impact.hasTripOwnership) {
    sections.push("该成员仍是旅行负责人。负责人身份必须保留，不能物理删除。");
  }
  if (impact.hasLoginCredential) {
    sections.push(
      impact.canDeleteWithCredential
        ? "该成员已停用，可在明确确认后同时注销登录凭据并删除。"
        : "该成员仍绑定登录账号。请先确认其不再使用系统并将成员停用，再执行永久删除。"
    );
  } else if (singular === "成员") {
    sections.push("请先在对应旅行中修改以上资料；如只是暂时不用，也可以停用成员。");
  } else {
    sections.push("“其他”是当前分类的兜底标签。请先把以上资料改用同分类的其他标签。");
  }
  return sections.join("\n\n");
}

export function memberCredentialDeletionConfirmation(name: string): string {
  return [
    `成员“${name}”已停用且没有业务引用。`,
    "继续后将同时注销其登录凭据、清除全部会话并删除基础成员。历史操作记录仍会保留为历史信息。",
    "此操作不能撤销。"
  ].join("\n\n");
}

export function tagDeletionConfirmation(
  name: string,
  impact: LibraryDeletionImpact
): string {
  if (!impact.references.length) return `确定删除标签“${name}”吗？`;
  return [
    `标签“${name}”影响 ${impact.references.length} 条现有资料：`,
    libraryReferenceLines(impact.references),
    "",
    "继续后，这些资料会改用同分类的“其他”标签，原业务记录不会被删除。"
  ].join("\n");
}

export function tripMemberReferenceLines(references: TripMemberReference[]): string {
  const lines = references.slice(0, visibleReferenceLimit).map((reference) => (
    `• ${resourceLabels[reference.resource] ?? "资料"}“${reference.label}”（${relationText(reference.relations)}）`
  ));
  if (references.length > lines.length) {
    lines.push(`• 另有 ${references.length - lines.length} 条共享资料`);
  }
  return lines.join("\n");
}

export function tripMemberDeletionBlockedMessage(
  name: string,
  impact: TripMemberDeletionImpact
): string {
  const sections = [`同行成员“${name}”还有不能自动清理的关联资料：`];
  if (impact.references.length) {
    sections.push(tripMemberReferenceLines(impact.references));
  }
  if (impact.cascadeExpenses.length) {
    sections.push(
      "以下手动费用不需要逐条修改；清空上述阻碍后，可在删除成员时明确确认一并删除：",
      tripMemberReferenceLines(impact.cascadeExpenses)
    );
  }
  if (impact.hasPrivatePackingItems) {
    sections.push("• 该成员还有个人物品。为保护隐私，这里不展示物品名称和数量。");
  }
  sections.push(
    impact.cascadeExpenses.length || impact.hasPrivatePackingItems
      ? "请先修改不能自动清理的资料；之后可在一次确认中删除成员、上述手动费用和个人物品。"
      : "请先修改以上资料后再删除。"
  );
  return sections.join("\n\n");
}

export function tripMemberCascadeConfirmation(
  name: string,
  impact: TripMemberDeletionImpact
): string {
  const sections = [`同行成员“${name}”只剩可连带清理的关联内容。`];
  if (impact.cascadeExpenses.length) {
    sections.push(
      `将永久删除以下 ${impact.cascadeExpenses.length} 条手动费用：`,
      tripMemberReferenceLines(impact.cascadeExpenses)
    );
  }
  if (impact.hasPrivatePackingItems) {
    sections.push(
      "将永久删除该成员在本次旅行中的全部个人物品。为保护隐私，这里不展示物品名称和数量。"
    );
  }
  sections.push(
    "继续后还会移除同行成员关系并撤销其非负责人访问权限。",
    "此操作不能撤销。"
  );
  return sections.join("\n\n");
}
