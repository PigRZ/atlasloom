// 基础资料页统一维护跨旅行复用的成员联系方式与标签。
import {
  IdentificationCard,
  PencilSimple,
  Plus,
  Sparkle,
  Tag,
  Trash
} from "@phosphor-icons/react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useEffect, useState } from "react";
import { useSearchParams } from "react-router";
import { EditorSurface } from "../../components/EditorSurface";
import { useConfirm } from "../../components/ConfirmDialog";
import { DataError } from "../../components/PageStates";
import { api } from "../../lib/api";
import {
  libraryDeletionBlockedMessage,
  memberCredentialDeletionConfirmation,
  tagDeletionConfirmation
} from "../../lib/deletionImpact";
import { LibraryForm } from "./LibraryForm";
import type {
  LibraryRecord,
  LibraryResource,
  Session,
  SnapshotSummary
} from "../../lib/types";

type LibraryPageProps = {
  admin: boolean;
  currentMemberId: string;
  mobile: string;
  setError: (message: string) => void;
  onSnapshot: (snapshot: SnapshotSummary) => void;
  onDirtyChange: (dirty: boolean) => void;
  onUserChange: (user: Session["user"]) => void;
};

type Section = {
  id: LibraryResource;
  label: string;
  singular: string;
  icon: typeof IdentificationCard;
};

const sections: Section[] = [
  {
    id: "members",
    label: "成员",
    singular: "成员",
    icon: IdentificationCard
  },
  {
    id: "tags",
    label: "标签",
    singular: "标签",
    icon: Tag
  }
];

const tagCategories = [
  { id: "platform-type", label: "平台类型" },
  { id: "place-type", label: "地点类型" },
  { id: "expense-type", label: "费用类型" },
  { id: "general", label: "通用" }
] as const;

export function LibraryPage({
  admin,
  currentMemberId,
  mobile,
  setError,
  onSnapshot,
  onDirtyChange,
  onUserChange
}: LibraryPageProps) {
  const [searchParams, setSearchParams] = useSearchParams();
  const requested = searchParams.get("section") as LibraryResource | null;
  const resource = sections.some((item) => item.id === requested)
    ? requested!
    : "members";
  const section = sections.find((item) => item.id === resource)!;
  const requestedCategory = searchParams.get("category");
  const tagCategoryFilter = tagCategories.some((category) => category.id === requestedCategory)
    ? requestedCategory!
    : "";
  const queryClient = useQueryClient();
  const confirm = useConfirm();
  const records = useQuery({
    queryKey: ["library", resource],
    queryFn: () => api.libraryRecords(resource)
  });
  const [editor, setEditor] = useState<LibraryRecord | "create" | null>(null);
  const [dirty, setDirty] = useState(false);
  const [presetNotice, setPresetNotice] = useState("");

  const closeEditor = () => {
    setEditor(null);
    setDirty(false);
    onDirtyChange(false);
  };
  const cancelEditor = async () => {
    if (!dirty || await confirm({
      title: "放弃未保存修改？",
      message: "当前表单有未保存的修改，关闭后不会保留。",
      confirmLabel: "放弃修改",
      tone: "danger"
    })) {
      closeEditor();
    }
  };
  const invalidate = () => Promise.all([
    queryClient.invalidateQueries({ queryKey: ["library"] }),
    queryClient.invalidateQueries({ queryKey: ["records"] })
  ]);
  const create = useMutation({
    mutationFn: ({
      data,
      password
    }: {
      data: Record<string, unknown>;
      password?: string;
    }) => api.createLibraryRecord(
      resource,
      crypto.randomUUID(),
      data,
      password
    ),
    onSuccess: async ({ snapshot }) => {
      onSnapshot(snapshot);
      await invalidate();
      closeEditor();
    },
    onError: (reason) => setError(
      reason instanceof Error ? reason.message : "基础资料保存失败"
    )
  });
  const update = useMutation({
    mutationFn: ({
      row,
      data,
      password
    }: {
      row: LibraryRecord;
      data: Record<string, unknown>;
      password?: string;
    }) => api.updateLibraryRecord(resource, row, data, password),
    onSuccess: async ({ snapshot }) => {
      onSnapshot(snapshot);
      await invalidate();
      closeEditor();
    },
    onError: (reason) => setError(
      reason instanceof Error ? reason.message : "基础资料保存失败"
    )
  });
  const updateSelf = useMutation({
    mutationFn: ({
      row,
      input
    }: {
      row: LibraryRecord;
      input: {
        displayName: string;
        mobile: string;
        alternateContact: string;
        emergencyContact: string;
        currentPassword: string;
        newPassword: string;
      };
    }) => api.updateProfile(row, input),
    onSuccess: async ({ snapshot, user }) => {
      onSnapshot(snapshot);
      onUserChange(user);
      await invalidate();
      closeEditor();
    },
    onError: (reason) => setError(
      reason instanceof Error ? reason.message : "个人资料保存失败"
    )
  });
  const remove = useMutation({
    mutationFn: ({
      row,
      replaceTagWithOther,
      deleteLoginCredential
    }: {
      row: LibraryRecord;
      replaceTagWithOther: boolean;
      deleteLoginCredential: boolean;
    }) => api.deleteLibraryRecord(resource, row, {
      replaceTagWithOther,
      deleteLoginCredential
    }),
    onSuccess: async ({ snapshot }) => {
      onSnapshot(snapshot);
      await invalidate();
    },
    onError: (reason) => setError(
      reason instanceof Error ? reason.message : "基础资料删除失败"
    )
  });
  const addPresets = useMutation({
    mutationFn: () => api.addTravelTypeTagPresets(),
    onSuccess: async ({ records: created, snapshot }) => {
      if (snapshot) onSnapshot(snapshot);
      await invalidate();
      setPresetNotice(
        created.length > 0
          ? `已补充 ${created.length} 个常用地点与费用类型。`
          : "常用地点与费用类型已经齐全。"
      );
    },
    onError: (reason) => setError(
      reason instanceof Error ? reason.message : "补充常用类型失败"
    )
  });

  useEffect(() => () => onDirtyChange(false), [onDirtyChange]);
  const rows = records.data?.records ?? [];
  const visibleRows = resource === "tags" && tagCategoryFilter
    ? rows.filter((row) => String(row.data.category ?? "general") === tagCategoryFilter)
    : rows;

  return (
    <div className="library-page page-enter">
      <section className="page-heading">
        <div>
          <h1>基础资料</h1>
        </div>
        <span className={admin ? "library-role is-admin" : "library-role"}>
          {mobile} · {admin ? "管理员" : "普通成员"}
        </span>
      </section>
      <nav className="library-tabs" aria-label="基础资料类型">
        {sections.map((item) => {
          const active = item.id === resource;
          return (
            <button
              key={item.id}
              type="button"
              aria-current={active ? "page" : undefined}
              className={active ? "is-active" : ""}
              onClick={() => setSearchParams({ section: item.id })}
            >
              <item.icon aria-hidden="true" size={18} weight={active ? "fill" : "regular"} />
              {item.label}
            </button>
          );
        })}
      </nav>
      <section className="library-panel">
        <header>
          <div>
            <h2>{section.label}</h2>
          </div>
          {admin && (
            <div className="library-header-actions">
              {resource === "tags" && (
                <button
                  className="secondary-button"
                  type="button"
                  disabled={addPresets.isPending}
                  onClick={() => addPresets.mutate()}
                >
                  <Sparkle aria-hidden="true" size={16} weight="duotone" />
                  {addPresets.isPending ? "补充中…" : "补充常用类型"}
                </button>
              )}
              <button
                className="primary-button"
                type="button"
                onClick={() => setEditor("create")}
              >
                <Plus aria-hidden="true" size={16} weight="bold" />
                新增{section.singular}
              </button>
            </div>
          )}
        </header>
        {resource === "tags" && presetNotice && (
          <p className="library-preset-notice" role="status">{presetNotice}</p>
        )}
        {resource === "tags" && (
          <div className="research-filters library-tag-filters" role="group" aria-label="标签类型筛选">
            <label>
              <span>按标签类型</span>
              <select
                value={tagCategoryFilter}
                onChange={(event) => {
                  const next = new URLSearchParams(searchParams);
                  if (event.target.value) next.set("category", event.target.value);
                  else next.delete("category");
                  setSearchParams(next, { replace: true });
                }}
              >
                <option value="">全部类型</option>
                {tagCategories.map((category) => (
                  <option key={category.id} value={category.id}>{category.label}</option>
                ))}
              </select>
            </label>
            <button
              className="secondary-button"
              type="button"
              disabled={!tagCategoryFilter}
              onClick={() => {
                const next = new URLSearchParams(searchParams);
                next.delete("category");
                setSearchParams(next, { replace: true });
              }}
            >
              清除筛选
            </button>
          </div>
        )}
        {!admin && (
          <p className="readonly-notice" role="status">
            你可以修改自己的联系方式和密码；其他基础资料仅管理员可以修改。
          </p>
        )}
        {records.isLoading ? (
          <div className="skeleton-block" role="status">
            <span className="sr-only">正在加载基础资料…</span>
          </div>
        ) : records.isError ? (
          <DataError message="基础资料读取失败" onRetry={() => records.refetch()} />
        ) : visibleRows.length ? (
          resource === "tags" ? (
            <div className="library-groups">
              {tagCategories
                .filter((category) => !tagCategoryFilter || category.id === tagCategoryFilter)
                .map((category) => {
                const groupedRows = visibleRows.filter((row) => (
                  String(row.data.category ?? "general") === category.id
                ));
                if (!groupedRows.length) return null;
                return (
                  <section className="library-group" key={category.id}>
                    <h3>{category.label}<span>{groupedRows.length}</span></h3>
                    <div className="library-list">
                      {groupedRows.map((row) => (
                        <LibraryRow
                          key={row.id}
                          row={row}
                          resource={resource}
                          admin={admin}
                          currentMemberId={currentMemberId}
                          onEdit={() => setEditor(row)}
                          onDelete={() => deleteLibraryRow(
                            row,
                            resource,
                            section.singular,
                            remove.mutate,
                            setError,
                            confirm
                          )}
                        />
                      ))}
                    </div>
                  </section>
                );
              })}
            </div>
          ) : (
            <div className="library-list">
              {visibleRows.map((row) => (
              <LibraryRow
                key={row.id}
                row={row}
                resource={resource}
                admin={admin}
                currentMemberId={currentMemberId}
                onEdit={() => setEditor(row)}
                onDelete={async () => {
                  const name = libraryRecordName(row);
                  try {
                    const impact = await api.libraryDeletionImpact(resource, row);
                    if (
                      impact.references.length
                      || impact.hasTripOwnership
                      || (
                        impact.hasLoginCredential
                        && !impact.canDeleteWithCredential
                      )
                    ) {
                      await confirm({
                        title: `暂时不能删除成员“${name}”`,
                        message: libraryDeletionBlockedMessage("成员", name, impact),
                        confirmLabel: "知道了",
                        tone: "danger",
                        acknowledgeOnly: true
                      });
                      return;
                    }
                    const deleteLoginCredential =
                      impact.hasLoginCredential
                      && impact.canDeleteWithCredential;
                    if (await confirm({
                      title: deleteLoginCredential
                        ? `永久删除成员“${name}”？`
                        : `删除${section.singular}“${name}”？`,
                      message: deleteLoginCredential
                        ? memberCredentialDeletionConfirmation(name)
                        : `确定删除成员“${name}”吗？此操作不能撤销。`,
                      confirmLabel: deleteLoginCredential
                        ? "注销账号并删除"
                        : "确认删除",
                      tone: "danger"
                    })) {
                      remove.mutate({
                        row,
                        replaceTagWithOther: false,
                        deleteLoginCredential
                      });
                    }
                  } catch (reason) {
                    setError(reason instanceof Error ? reason.message : "删除影响检查失败");
                  }
                }}
              />
              ))}
            </div>
          )
        ) : (
          <div className="empty-inline">
            <section.icon aria-hidden="true" size={30} weight="duotone" />
            <b>
              {resource === "tags" && tagCategoryFilter
                ? "没有符合筛选条件的标签"
                : `暂无${section.singular}`}
            </b>
            {!admin && <p>请联系管理员维护。</p>}
          </div>
        )}
      </section>
      <EditorSurface
        open={editor !== null}
        title={`${editor === "create" ? "新增" : "编辑"}${section.singular}`}
        variant="modal"
        dirty={dirty}
        onClose={closeEditor}
      >
        {editor && (
          <LibraryForm
            resource={resource}
            initial={editor === "create" ? undefined : editor}
            selfMode={
              !admin &&
              editor !== "create" &&
              resource === "members" &&
              editor.id === currentMemberId
            }
            pending={create.isPending || update.isPending || updateSelf.isPending}
            onDirtyChange={(value) => {
              setDirty(value);
              onDirtyChange(value);
            }}
            onCancel={cancelEditor}
            onSubmit={(data, password, profile) => editor === "create"
              ? create.mutateAsync({ data, password }).then(() => undefined)
              : !admin && resource === "members" && editor.id === currentMemberId
                ? updateSelf.mutateAsync({ row: editor, input: profile }).then(() => undefined)
                : update.mutateAsync({ row: editor, data, password }).then(() => undefined)}
          />
        )}
      </EditorSurface>
    </div>
  );
}

function LibraryRow({
  row,
  resource,
  admin,
  currentMemberId,
  onEdit,
  onDelete
}: {
  row: LibraryRecord;
  resource: LibraryResource;
  admin: boolean;
  currentMemberId: string;
  onEdit: () => void;
  onDelete: () => Promise<void>;
}) {
  const detail = resource === "members"
    ? memberAccountDetail(row, currentMemberId)
    : String(row.data.status ?? "active") === "inactive" ? "已停用" : "";
  const canEdit = admin || (resource === "members" && row.id === currentMemberId);
  return (
    <article className="library-row">
      {resource === "tags" && (
        <span
          className="tag-color"
          style={{ backgroundColor: String(row.data.color ?? "#477566") }}
          aria-hidden="true"
        />
      )}
      <div>
        <b>{libraryRecordName(row)}</b>
        <small>{detail}</small>
      </div>
      {canEdit && (
        <div>
          <button type="button" onClick={onEdit}>
            <PencilSimple aria-hidden="true" size={15} />
            {admin ? "编辑" : "编辑自己"}
          </button>
          {admin && (
            <button className="danger" type="button" onClick={onDelete}>
              <Trash aria-hidden="true" size={15} />删除
            </button>
          )}
        </div>
      )}
    </article>
  );
}

async function deleteLibraryRow(
  row: LibraryRecord,
  resource: LibraryResource,
  singular: string,
  remove: (variables: {
    row: LibraryRecord;
    replaceTagWithOther: boolean;
    deleteLoginCredential: boolean;
  }) => void,
  setError: (message: string) => void,
  confirm: ReturnType<typeof useConfirm>
) {
  try {
    const impact = await api.libraryDeletionImpact(resource, row);
    const name = libraryRecordName(row);
    if (
      impact.references.length > 0 &&
      !impact.canReplaceWithFallback
    ) {
      await confirm({
        title: `暂时不能删除标签“${name}”`,
        message: libraryDeletionBlockedMessage("标签", name, impact),
        confirmLabel: "知道了",
        tone: "danger",
        acknowledgeOnly: true
      });
      return;
    }
    if (await confirm({
      title: `删除${singular}“${name}”？`,
      message: tagDeletionConfirmation(name, impact),
      confirmLabel: impact.references.length ? "改用其他并删除" : "确认删除",
      tone: "danger"
    })) {
      remove({
        row,
        replaceTagWithOther: impact.references.length > 0,
        deleteLoginCredential: false
      });
    }
  } catch (reason) {
    setError(reason instanceof Error ? reason.message : "删除影响检查失败");
  }
}

function libraryRecordName(row: LibraryRecord): string {
  return String(row.data.displayName ?? row.data.name ?? "未命名");
}

function memberAccountDetail(
  row: LibraryRecord,
  currentMemberId: string
): string {
  const mobile = String(row.data.mobile ?? "");
  return [
    mobile || "未填写手机号",
    String(row.data.status ?? "active") === "inactive" ? "已停用" : "",
    row.data.admin ? "管理员" : "",
    row.id === currentMemberId ? "当前成员" : ""
  ].filter(Boolean).join(" · ");
}
