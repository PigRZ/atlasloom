// 导入面板先验证文件并完成基础资料映射，再提交一次事务化的新旅行创建。
import {
  CheckCircle,
  FileArrowUp,
  WarningCircle
} from "@phosphor-icons/react";
import { useQueryClient } from "@tanstack/react-query";
import { useEffect, useId, useState, type ChangeEvent } from "react";
import { api } from "../../lib/api";
import { formatDateRange } from "../../lib/formatters";
import type {
  LibraryRecord,
  SnapshotSummary,
  Trip,
  TripBackupDependencyMapping,
  TripBackupDependencyPreview,
  TripBackupImportPreview
} from "../../lib/types";

const createValue = "__create__";
const maximumBackupBytes = 16 * 1024 * 1024;

type TripImportPanelProps = {
  admin: boolean;
  onImported: (trip: Trip) => void;
  onSnapshot: (snapshot: SnapshotSummary) => void;
  onDirtyChange: (dirty: boolean) => void;
  onCancel: () => void;
};

export function TripImportPanel({
  admin,
  onImported,
  onSnapshot,
  onDirtyChange,
  onCancel
}: TripImportPanelProps) {
  const inputId = useId();
  const queryClient = useQueryClient();
  const [fileName, setFileName] = useState("");
  const [backup, setBackup] = useState<unknown>(null);
  const [preview, setPreview] = useState<TripBackupImportPreview | null>(null);
  const [members, setMembers] = useState<LibraryRecord[]>([]);
  const [tags, setTags] = useState<LibraryRecord[]>([]);
  const [memberSelections, setMemberSelections] = useState<Record<string, string>>({});
  const [tagSelections, setTagSelections] = useState<Record<string, string>>({});
  const [loading, setLoading] = useState(false);
  const [applying, setApplying] = useState(false);
  const [error, setError] = useState("");

  useEffect(() => () => onDirtyChange(false), [onDirtyChange]);

  const chooseFile = async (event: ChangeEvent<HTMLInputElement>) => {
    const file = event.target.files?.[0];
    event.target.value = "";
    if (!file) return;
    setError("");
    setPreview(null);
    setBackup(null);
    setFileName(file.name);
    onDirtyChange(true);
    if (file.size > maximumBackupBytes) {
      setError("备份文件不能超过 16 MB");
      return;
    }
    setLoading(true);
    try {
      const parsed = JSON.parse(await file.text()) as unknown;
      const [nextPreview, memberResult, tagResult] = await Promise.all([
        api.previewTripBackupImport(parsed),
        queryClient.fetchQuery({
          queryKey: ["library", "members"],
          queryFn: () => api.libraryRecords("members")
        }),
        queryClient.fetchQuery({
          queryKey: ["library", "tags"],
          queryFn: () => api.libraryRecords("tags")
        })
      ]);
      setBackup(parsed);
      setPreview(nextPreview);
      setMembers(memberResult.records);
      setTags(tagResult.records);
      setMemberSelections(defaultSelections(nextPreview.members));
      setTagSelections(defaultSelections(nextPreview.tags));
    } catch (reason) {
      setError(reason instanceof SyntaxError
        ? "文件不是有效的 JSON"
        : reason instanceof Error ? reason.message : "备份文件读取失败");
    } finally {
      setLoading(false);
    }
  };

  const duplicateMembers = duplicateTripBackupTarget(memberSelections);
  const duplicateTags = duplicateTripBackupTarget(tagSelections);
  const mappingsComplete = preview?.valid === true &&
    Boolean(preview.checksum) &&
    dependenciesMapped(preview.members, memberSelections) &&
    dependenciesMapped(preview.tags, tagSelections) &&
    !duplicateMembers &&
    !duplicateTags;

  const apply = async () => {
    if (!preview?.checksum || !backup || !mappingsComplete) return;
    setApplying(true);
    setError("");
    try {
      const result = await api.applyTripBackupImport(
        backup,
        preview.checksum,
        tripBackupMappingRequests(preview.members, memberSelections),
        tripBackupMappingRequests(preview.tags, tagSelections)
      );
      if (result.librarySnapshot) onSnapshot(result.librarySnapshot);
      onSnapshot(result.snapshot);
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: ["trips"] }),
        queryClient.invalidateQueries({ queryKey: ["library"] })
      ]);
      onDirtyChange(false);
      onImported(result.trip);
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : "旅行导入失败");
    } finally {
      setApplying(false);
    }
  };

  return (
    <div className="trip-import-panel" aria-busy={loading || applying}>
      <section className="trip-import-intro">
        <FileArrowUp aria-hidden="true" size={28} weight="duotone" />
        <div>
          <h3>从 JSON 生成新旅行</h3>
          <p>导入当前业务状态；登录凭据、历史快照和操作记录不会进入新旅行。</p>
        </div>
      </section>

      <label className="trip-import-file" htmlFor={inputId}>
        <span>{fileName || "选择旅行备份文件"}</span>
        <small>仅接受本项目导出的 JSON，最大 16 MB</small>
        <input
          id={inputId}
          type="file"
          accept="application/json,.json"
          onChange={chooseFile}
        />
      </label>

      {loading && <p className="trip-import-loading" role="status">正在校验旅行和基础资料引用…</p>}

      {preview && !preview.valid && (
        <section className="trip-import-invalid" role="alert">
          <WarningCircle aria-hidden="true" size={20} />
          <div>
            <b>这个文件不能导入</b>
            {preview.validationErrors.map((message) => <p key={message}>{message}</p>)}
          </div>
        </section>
      )}

      {preview?.valid && (
        <>
          <section className="trip-import-summary">
            <div>
              <span>将生成新旅行</span>
              <h3>{preview.trip.title}</h3>
              <p>{formatDateRange(preview.trip.startDate, preview.trip.endDate)}</p>
            </div>
            <div className="trip-import-counts">
              {resourceCounts(preview).map(([label, count]) => (
                <span key={label}><b>{count}</b>{label}</span>
              ))}
            </div>
          </section>

          {(preview.members.length > 0 || preview.tags.length > 0) && (
            <section className="trip-import-mappings">
              <header>
                <div>
                  <h3>确认基础资料</h3>
                  <p>不会覆盖现有成员或标签；缺失项需要明确选择。</p>
                </div>
                {preview.ready && (
                  <span className="mapping-ready">
                    <CheckCircle aria-hidden="true" size={16} weight="fill" />已自动匹配
                  </span>
                )}
              </header>

              {preview.members.length > 0 && (
                <MappingGroup
                  title="同行成员"
                  dependencies={preview.members}
                  records={members}
                  selections={memberSelections}
                  admin={admin}
                  createLabel="是，导入时创建无登录凭据成员"
                  optionLabel={(row) => memberOptionLabel(row)}
                  onChange={(sourceId, value) => {
                    setMemberSelections((current) => ({ ...current, [sourceId]: value }));
                    onDirtyChange(true);
                  }}
                />
              )}

              {preview.tags.length > 0 && (
                <MappingGroup
                  title="旅行标签"
                  dependencies={preview.tags}
                  records={tags}
                  selections={tagSelections}
                  admin={admin}
                  createLabel="是，导入时创建这个标签"
                  optionLabel={(row) => tagOptionLabel(row)}
                  filterRecord={(dependency, row) => row.data.category === dependency.category}
                  onChange={(sourceId, value) => {
                    setTagSelections((current) => ({ ...current, [sourceId]: value }));
                    onDirtyChange(true);
                  }}
                />
              )}

              {(duplicateMembers || duplicateTags) && (
                <p className="form-error" role="alert">
                  不同来源项目不能映射到同一个基础资料，请分别选择。
                </p>
              )}
            </section>
          )}
        </>
      )}

      {error && <p className="form-error" role="alert">{error}</p>}

      <div className="form-actions trip-import-actions">
        <button className="secondary-button" type="button" onClick={onCancel}>取消</button>
        <button
          className="primary-button"
          type="button"
          disabled={!mappingsComplete || applying}
          onClick={apply}
        >
          {applying ? "正在生成…" : "生成新旅行"}
        </button>
      </div>
    </div>
  );
}

function MappingGroup({
  title,
  dependencies,
  records,
  selections,
  admin,
  createLabel,
  optionLabel,
  filterRecord = () => true,
  onChange
}: {
  title: string;
  dependencies: TripBackupDependencyPreview[];
  records: LibraryRecord[];
  selections: Record<string, string>;
  admin: boolean;
  createLabel: string;
  optionLabel: (record: LibraryRecord) => string;
  filterRecord?: (dependency: TripBackupDependencyPreview, record: LibraryRecord) => boolean;
  onChange: (sourceId: string, value: string) => void;
}) {
  return (
    <div className="mapping-group">
      <h4>{title}</h4>
      {dependencies.map((dependency) => (
        <label key={dependency.sourceId}>
          <span>
            <b>{dependency.displayName || dependency.name}</b>
            {dependency.category && <small>{tagCategoryLabel(dependency.category)}</small>}
          </span>
          <select
            value={selections[dependency.sourceId] ?? ""}
            aria-label={`映射${dependency.displayName || dependency.name}`}
            onChange={(event) => onChange(dependency.sourceId, event.target.value)}
          >
            <option value="">请选择</option>
            {records.filter((row) => filterRecord(dependency, row)).map((row) => (
              <option key={row.id} value={row.id}>{optionLabel(row)}</option>
            ))}
            {admin && <option value={createValue}>{createLabel}</option>}
          </select>
          <small>
            {selections[dependency.sourceId] === createValue
              ? `已确认：${createLabel}`
              : mappingStatusLabel(dependency.status)}
          </small>
        </label>
      ))}
    </div>
  );
}

function defaultSelections(
  dependencies: TripBackupDependencyPreview[]
): Record<string, string> {
  return Object.fromEntries(
    dependencies.map((dependency) => [dependency.sourceId, dependency.targetId ?? ""])
  );
}

function dependenciesMapped(
  dependencies: TripBackupDependencyPreview[],
  selections: Record<string, string>
): boolean {
  return dependencies.every((dependency) => Boolean(selections[dependency.sourceId]));
}

export function duplicateTripBackupTarget(selections: Record<string, string>): boolean {
  const targets = Object.values(selections).filter((value) => value && value !== createValue);
  return new Set(targets).size !== targets.length;
}

export function tripBackupMappingRequests(
  dependencies: TripBackupDependencyPreview[],
  selections: Record<string, string>
): TripBackupDependencyMapping[] {
  return dependencies.map((dependency) => {
    const value = selections[dependency.sourceId] ?? "";
    return {
      sourceId: dependency.sourceId,
      targetId: value === createValue ? undefined : value,
      create: value === createValue
    };
  });
}

function resourceCounts(preview: TripBackupImportPreview): Array<[string, number]> {
  return [
    ["天日程", preview.counts.days ?? 0],
    ["个地点", preview.counts.places ?? 0],
    ["笔费用", preview.counts.expenses ?? 0],
    ["项预订", preview.counts.bookings ?? 0]
  ];
}

function mappingStatusLabel(status: TripBackupDependencyPreview["status"]): string {
  switch (status) {
    case "matched-id":
      return "已按原始身份匹配";
    case "matched-semantic":
      return "已按名称和分类匹配";
    case "conflict":
      return "存在同名或身份冲突；不会自动创建，请选择或明确确认创建";
    default:
      return "当前基础资料中缺失；不会自动创建，请选择或明确确认创建";
  }
}

function memberOptionLabel(row: LibraryRecord): string {
  const name = String(row.data.displayName ?? "未命名成员");
  const mobile = String(row.data.mobile ?? "").trim();
  return `${name} · ${mobile || `编号 …${row.id.slice(-6)}`}`;
}

function tagOptionLabel(row: LibraryRecord): string {
  return `${String(row.data.name ?? "未命名标签")} · 编号 …${row.id.slice(-6)}`;
}

function tagCategoryLabel(category: NonNullable<TripBackupDependencyPreview["category"]>): string {
  return {
    general: "通用",
    "place-type": "地点类型",
    "expense-type": "费用类型",
    "platform-type": "平台类型"
  }[category];
}
