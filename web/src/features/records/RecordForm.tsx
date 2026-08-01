// 记录表单只收集字段与报告脏状态，编辑浮层和写入生命周期由调用方管理。
import { Plus, X } from "@phosphor-icons/react";
import {
  useEffect,
  useId,
  useMemo,
  useRef,
  useState,
  type FormEvent
} from "react";
import {
  resourceCoreFields,
  type ResourceDefinition
} from "../../lib/resourceDefinitions";
import { taxonomyOptions } from "../../lib/taxonomy";
import { TemporalInput } from "../../components/TemporalInput";
import { PlacePicker } from "./PlacePicker";

const helperText: Record<string, string> = {
  dayId: "选择记录所属的自然日。",
  dayIds: "选择一个或多个真实执行日；不要填写 D1/D3 之类的组合文本。",
  placeId: "选择关联地点。",
  fromPlaceId: "选择交通段的起点。",
  toPlaceId: "选择交通段的终点。",
  mode: "交通方式决定地图路径与导航降级方式。",
  amountMinor: "直接填写实际或预计金额，例如 12.34。",
  dateTime: "选择预订发生或开始的日期和时间；时间范围等补充信息可写入备注。",
  platformId: "平台类型由基础资料中的标签统一维护。",
  bookerMemberId: "自动生成费用的支付人会与预订人保持一致。",
  payerMemberId: "选择实际垫付这笔费用的成员。",
  recordedByMemberId: "选择实际记录这条资料的旅行成员。",
  splitMemberIds: "单人或指定成员分摊时选择受益成员。",
  weatherAdcode: "填写 6 位高德行政区划代码。",
  navigationURL: "可留空；坐标完整时会生成高德导航入口。",
  additionalInfo: "填写需要同行成员留意的信息。"
};

type RecordFormProps = {
  definition: ResourceDefinition;
  initial?: Record<string, unknown>;
  onSubmit: (recordKey: string, data: Record<string, unknown>) => Promise<void>;
  onCancel: () => void;
  onDirtyChange?: (dirty: boolean) => void;
  pending?: boolean;
  submitLabel?: string;
  defaults?: Record<string, unknown>;
  options?: Record<string, FieldOption[]>;
  tripId?: string;
  hiddenFields?: string[];
  readOnlyFields?: string[];
  contextNote?: string;
  fieldConstraints?: Record<string, FieldConstraint>;
  onCreatePlace?: PlaceCreationRequest;
  createPlaceDisabled?: boolean;
  createPlaceDisabledReason?: string;
};

export type FieldOption = { value: string; label: string };
export type PlaceCreationRequest = (
  targetLabel: string,
  onCreated: (option: FieldOption) => void
) => void;
export type FieldConstraint = {
  min?: string;
  max?: string;
  helperText?: string;
  errorMessage?: string;
};

export function RecordForm({
  definition,
  initial,
  onSubmit,
  onCancel,
  onDirtyChange,
  pending,
  submitLabel = "保存",
  defaults,
  options = {},
  tripId,
  hiddenFields = [],
  readOnlyFields = [],
  contextNote,
  fieldConstraints = {},
  onCreatePlace,
  createPlaceDisabled = false,
  createPlaceDisabledReason
}: RecordFormProps) {
  const formId = useId().replaceAll(":", "");
  const recordKeyRef = useRef(crypto.randomUUID());
  const errorRef = useRef<HTMLParagraphElement>(null);
  const initialFields = useMemo(
    () => Object.fromEntries(definition.fields.map(([key, , type]) => {
      const source = initial?.[key] ?? defaults?.[key] ?? defaultValue(key);
      return [
        key,
        type === "money"
          ? moneyDraftValue(source)
          : (type === "tags" || type === "url-list") && Array.isArray(source)
            ? source.join("，")
            : source
      ];
    })),
    [definition, defaults, initial]
  );
  const [data, setData] = useState<Record<string, unknown>>(initialFields);
  const [changed, setChanged] = useState(new Set<string>());
  const [submitting, setSubmitting] = useState(false);
  const [formError, setFormError] = useState("");
  const [extraOpen, setExtraOpen] = useState(true);
  const [createdPlaceOptions, setCreatedPlaceOptions] = useState<
    Record<string, FieldOption[]>
  >({});
  const [placeCreatedMessage, setPlaceCreatedMessage] = useState("");
  const dirty = changed.size > 0;
  const busy = Boolean(pending || submitting);

  useEffect(() => {
    onDirtyChange?.(dirty);
    return () => onDirtyChange?.(false);
  }, [dirty, onDirtyChange]);

  useEffect(() => {
    if (!dirty) return undefined;
    const warnBeforeUnload = (event: BeforeUnloadEvent) => event.preventDefault();
    window.addEventListener("beforeunload", warnBeforeUnload);
    return () => window.removeEventListener("beforeunload", warnBeforeUnload);
  }, [dirty]);

  useEffect(() => {
    if (formError) errorRef.current?.focus();
  }, [formError]);

  const updateField = (key: string, value: unknown) => {
    setData((previous) => ({ ...previous, [key]: value }));
    setChanged((previous) => new Set(previous).add(key));
    setFormError("");
  };
  const selectCreatedPlace = (
    fieldKey: string,
    label: string,
    option: FieldOption
  ) => {
    setCreatedPlaceOptions((previous) => ({
      ...previous,
      [fieldKey]: mergeFieldOptions(previous[fieldKey], [option]) ?? []
    }));
    updateField(fieldKey, option.value);
    setPlaceCreatedMessage(`已创建并选择地点“${option.label}”作为${label}`);
  };
  const submit = async (event: FormEvent) => {
    event.preventDefault();
    setFormError("");
    const constraintError = validateFieldConstraints(definition, data, fieldConstraints);
    if (constraintError) {
      setFormError(constraintError);
      return;
    }
    if (definition.id === "places") {
      if (!String(data.type ?? "").trim()) {
        setFormError("请选择地点类型");
        return;
      }
    }
    if (definition.id === "places" && (
      !Number.isFinite(Number(data.longitude))
      || !Number.isFinite(Number(data.latitude))
      || data.longitude === ""
      || data.latitude === ""
    )) {
      setFormError("请先通过高德地图搜索或点击，确认地点位置");
      return;
    }
    setSubmitting(true);
    try {
      await onSubmit(recordKeyRef.current, {
        ...preserveSystemFields(definition, initial),
        ...normalizeRecordData(definition, data)
      });
      setChanged(new Set());
    } catch (reason) {
      setFormError(reason instanceof Error ? reason.message : "保存失败，请重试");
    } finally {
      setSubmitting(false);
    }
  };
  const coreFieldNames = new Set(resourceCoreFields[definition.id]);
  const hiddenFieldNames = new Set(hiddenFields);
  const readOnlyFieldNames = new Set(readOnlyFields);
  const locationFields = definition.id === "places"
    ? new Set(["address", "region", "latitude", "longitude", "weatherAdcode", "navigationURL"])
    : new Set<string>();
  const primaryFields = definition.fields.filter(([key]) => (
    coreFieldNames.has(key) && !locationFields.has(key) && !hiddenFieldNames.has(key)
  ));
  const extraFields = definition.fields.filter(([key]) => (
    !coreFieldNames.has(key) && !locationFields.has(key) && !hiddenFieldNames.has(key)
  )).filter(([key]) => key !== "splitMemberIds" || data.splitMode === "selected");

  return (
    <form
      className="record-form"
      aria-busy={busy}
      data-dirty={dirty ? "true" : "false"}
      onSubmit={submit}
    >
      <div className="form-title">
        <div>
          <p>{initial ? "编辑" : "新增"}</p>
          <h2>{definition.singular}</h2>
        </div>
        <button type="button" onClick={onCancel} aria-label="关闭表单">
          <X aria-hidden="true" size={16} weight="bold" />
        </button>
      </div>
      {dirty && <p className="unsaved-note" role="status">有未保存的修改</p>}
      {contextNote && <p className="form-context-note">{contextNote}</p>}
      <div className="form-fields">
        {primaryFields.map((field) => (
          <Field
            key={field[0]}
            formId={formId}
            resource={definition.id}
            field={field}
            value={data[field[0]]}
            required={requiredField(definition, field[0], data)}
            readOnly={readOnlyFieldNames.has(field[0])}
            options={mergeFieldOptions(
              options[field[0]],
              createdPlaceOptions[field[0]]
            )}
            constraint={fieldConstraints[field[0]]}
            onConstraintError={setFormError}
            onChange={(value) => updateField(field[0], value)}
            onCreatePlace={isPlaceAssociationField(field[0]) && onCreatePlace
              ? () => onCreatePlace(
                field[1],
                (option) => selectCreatedPlace(field[0], field[1], option)
              )
              : undefined}
            createPlaceDisabled={createPlaceDisabled}
            createPlaceDisabledReason={createPlaceDisabledReason}
          />
        ))}
      </div>
      {definition.id === "places" && tripId && (
        <PlacePicker
          tripId={tripId}
          value={data}
          onChange={(fields) => {
            Object.entries(fields).forEach(([key, value]) => updateField(key, value));
          }}
        />
      )}
      {extraFields.length > 0 && (
        <details
          className="form-more"
          open={extraOpen}
          onToggle={(event) => setExtraOpen(event.currentTarget.open)}
        >
          <summary>补充信息（可选，{extraFields.length} 项）</summary>
          <div className="form-fields">
            {extraFields.map((field) => (
              <Field
                key={field[0]}
                formId={formId}
                resource={definition.id}
                field={field}
                value={data[field[0]]}
                required={requiredField(definition, field[0], data)}
                readOnly={readOnlyFieldNames.has(field[0])}
                options={mergeFieldOptions(
                  options[field[0]],
                  createdPlaceOptions[field[0]]
                )}
                constraint={fieldConstraints[field[0]]}
                onConstraintError={setFormError}
                onChange={(value) => updateField(field[0], value)}
                onCreatePlace={isPlaceAssociationField(field[0]) && onCreatePlace
                  ? () => onCreatePlace(
                    field[1],
                    (option) => selectCreatedPlace(field[0], field[1], option)
                  )
                  : undefined}
                createPlaceDisabled={createPlaceDisabled}
                createPlaceDisabledReason={createPlaceDisabledReason}
              />
            ))}
          </div>
        </details>
      )}
      {formError && (
        <p className="form-error" role="alert" tabIndex={-1} ref={errorRef}>
          {formError}
        </p>
      )}
      <p className="sr-only" role="status" aria-live="polite">
        {placeCreatedMessage}
      </p>
      <div className="form-actions">
        <button className="secondary-button" type="button" onClick={onCancel}>取消</button>
        <button className="primary-button" type="submit" disabled={busy}>
          {busy ? "保存中…" : submitLabel}
        </button>
      </div>
    </form>
  );
}

type FieldProps = {
  formId: string;
  resource: ResourceDefinition["id"];
  field: ResourceDefinition["fields"][number];
  value: unknown;
  required: boolean;
  readOnly: boolean;
  options?: FieldOption[];
  constraint?: FieldConstraint;
  onConstraintError: (message: string) => void;
  onChange: (value: unknown) => void;
  onCreatePlace?: () => void;
  createPlaceDisabled: boolean;
  createPlaceDisabledReason?: string;
};

function Field({
  formId,
  resource,
  field: [fieldKey, label, type],
  value,
  required,
  readOnly,
  options,
  constraint,
  onConstraintError,
  onChange,
  onCreatePlace,
  createPlaceDisabled,
  createPlaceDisabledReason
}: FieldProps) {
  const id = `${formId}-${fieldKey}`;
  const helperId = helperText[fieldKey] ? `${id}-help` : undefined;
  const constraintHelperId = constraint?.helperText ? `${id}-constraint-help` : undefined;

  if (type === "checkbox") {
    return (
      <label className="checkbox" htmlFor={id}>
        <input
          id={id}
          name={fieldKey}
          type="checkbox"
          disabled={readOnly}
          checked={Boolean(value)}
          onChange={(event) => onChange(event.target.checked)}
        />
        <span>{label}</span>
      </label>
    );
  }
  if (type === "select") {
    const baseOptions = options ?? taxonomyOptions(resource, fieldKey);
    const currentValue = String(value ?? "");
    const visibleOptions = currentValue && !baseOptions.some((option) => option.value === currentValue)
      ? [{ value: currentValue, label: currentValue }, ...baseOptions]
      : baseOptions;
    const select = (
      <select
        id={id}
        name={fieldKey}
        autoComplete="off"
        required={required}
        disabled={readOnly}
        value={currentValue}
        onChange={(event) => onChange(event.target.value)}
      >
        <option value="">未设置</option>
        {visibleOptions.map((option) => (
          <option key={option.value} value={option.value}>{option.label}</option>
        ))}
      </select>
    );
    if (!onCreatePlace) {
      return (
        <label htmlFor={id}>
          <span>{label}{required && <i aria-hidden="true">必填</i>}</span>
          {select}
          {helperId && <small id={helperId}>{helperText[fieldKey]}</small>}
        </label>
      );
    }
    return (
      <div className="record-field place-association-field">
        <label htmlFor={id}>
          <span>{label}{required && <i aria-hidden="true">必填</i>}</span>
        </label>
        <div className="place-association-control">
          {select}
          <button
            className="secondary-button place-create-trigger"
            type="button"
            aria-label={`新增地点并选为${label}`}
            title={createPlaceDisabledReason ?? `新增地点并选为${label}`}
            disabled={readOnly || createPlaceDisabled}
            onClick={onCreatePlace}
          >
            <Plus aria-hidden="true" size={18} weight="bold" />
          </button>
        </div>
        {helperId && <small id={helperId}>{helperText[fieldKey]}</small>}
      </div>
    );
  }
  if (type === "multiselect") {
    const selected = new Set(Array.isArray(value) ? value.map(String) : []);
    const visibleOptions = [...(options ?? [])];
    selected.forEach((selectedValue) => {
      if (!visibleOptions.some((option) => option.value === selectedValue)) {
        visibleOptions.unshift({ value: selectedValue, label: selectedValue });
      }
    });
    return (
      <fieldset className="multi-select">
        <legend>{label}</legend>
        {visibleOptions.length ? visibleOptions.map((option) => (
          <label key={option.value}>
            <input
              name={fieldKey}
              type="checkbox"
              value={option.value}
              disabled={readOnly}
              checked={selected.has(option.value)}
              onChange={(event) => {
                const next = new Set(selected);
                if (event.target.checked) next.add(option.value);
                else next.delete(option.value);
                onChange([...next]);
              }}
            />
            <span>{option.label}</span>
          </label>
        )) : <small>请先创建可关联的记录。</small>}
      </fieldset>
    );
  }
  if (type === "textarea") {
    return (
      <label htmlFor={id}>
        <span>{label}{required && <i aria-hidden="true">必填</i>}</span>
        <textarea
          id={id}
          name={fieldKey}
          rows={3}
          required={required}
          readOnly={readOnly}
          value={String(value ?? "")}
          onChange={(event) => onChange(event.target.value)}
        />
      </label>
    );
  }

  const inputType = type === "money"
    ? "number"
    : type === "tags"
      ? "text"
      : type === "url-list"
        ? "text"
        : type;
  const disableSpellCheck = inputType === "url" || type === "url-list" || /Id$|adcode/i.test(fieldKey);
  const rawDateTimeValue = type === "datetime-local" ? String(value ?? "").trim() : "";
  const preservedDateTimeValue = rawDateTimeValue && !isExactLocalDateTime(rawDateTimeValue)
    ? rawDateTimeValue
    : "";
  const preservedDateTimeId = preservedDateTimeValue ? `${id}-current-value` : undefined;
  const describedBy = [helperId, constraintHelperId, preservedDateTimeId]
    .filter(Boolean)
    .join(" ") || undefined;
  const displayedValue = type === "datetime-local"
    ? localDateTimeValue(value)
    : String(value ?? "");

  if (type === "date" || type === "datetime-local" || type === "time") {
    return (
      <div className="record-field temporal-field">
        <label htmlFor={id}>
          <span>{label}{required && <i aria-hidden="true">必填</i>}</span>
        </label>
        <TemporalInput
          id={id}
          name={fieldKey}
          type={type}
          autoComplete="off"
          aria-describedby={describedBy}
          min={constraint?.min}
          max={constraint?.max}
          required={required}
          readOnly={readOnly}
          value={displayedValue}
          onInvalid={(event) => {
            if (!event.currentTarget.validity.rangeUnderflow
              && !event.currentTarget.validity.rangeOverflow) return;
            onConstraintError(
              constraint?.errorMessage
                ?? `${label}必须在允许范围内`
            );
          }}
          onChange={(event) => onChange(event.target.value)}
        />
        {helperId && <small id={helperId}>{helperText[fieldKey]}</small>}
        {constraintHelperId && (
          <small id={constraintHelperId} className="field-constraint-note">
            {constraint?.helperText}
          </small>
        )}
        {preservedDateTimeId && (
          <small id={preservedDateTimeId} className="field-value-note">
            当前记录为“{preservedDateTimeValue}”，无法完整放入单一日期时间选择器。
            未修改时间时会原样保留，选择新时间后才会替换。
          </small>
        )}
      </div>
    );
  }

  return (
    <label htmlFor={id}>
      <span>{label}{required && <i aria-hidden="true">必填</i>}</span>
      <input
        id={id}
        name={fieldKey}
        type={inputType}
        autoComplete="off"
        aria-describedby={describedBy}
        inputMode={type === "number" || type === "money"
          ? "decimal"
          : type === "url" || type === "url-list" ? "url" : undefined}
        step={type === "money" ? "0.01" : type === "number" ? "any" : undefined}
        min={type === "money" ? "0.01" : undefined}
        spellCheck={disableSpellCheck ? false : undefined}
        required={required}
        readOnly={readOnly}
        value={displayedValue}
        onChange={(event) => onChange(
          type === "number"
            ? event.target.value === "" ? "" : Number(event.target.value)
            : event.target.value
        )}
      />
      {helperId && <small id={helperId}>{helperText[fieldKey]}</small>}
      {preservedDateTimeId && (
        <small id={preservedDateTimeId} className="field-value-note">
          当前记录为“{preservedDateTimeValue}”，无法完整放入单一日期时间选择器。
          未修改时间时会原样保留，选择新时间后才会替换。
        </small>
      )}
    </label>
  );
}

function localDateTimeValue(value: unknown): string {
  const raw = String(value ?? "").trim();
  const match = raw.match(/^(\d{4}-\d{2}-\d{2})[ T](\d{2}:\d{2})/);
  return match ? `${match[1]}T${match[2]}` : "";
}

function isExactLocalDateTime(value: string): boolean {
  return /^\d{4}-\d{2}-\d{2}[ T]\d{2}:\d{2}$/.test(value);
}

function validateFieldConstraints(
  definition: ResourceDefinition,
  data: Record<string, unknown>,
  constraints: Record<string, FieldConstraint>
): string {
  for (const [key, label] of definition.fields) {
    const constraint = constraints[key];
    if (!constraint) continue;
    const value = String(data[key] ?? "").trim();
    if (!value) continue;
    if (
      (constraint.min && value < constraint.min)
      || (constraint.max && value > constraint.max)
    ) {
      return constraint.errorMessage ?? `${label}必须在允许范围内`;
    }
  }
  return "";
}

function defaultValue(key: string): unknown {
  if (key === "mode") return "drive";
  if (key === "splitMode") return "all";
  if (key === "occurredAt") {
    const now = new Date();
    return new Date(now.getTime() - now.getTimezoneOffset() * 60_000).toISOString().slice(0, 16);
  }
  if (key === "packed") return false;
  return "";
}

function moneyDraftValue(value: unknown): string {
  if (value === "" || value === null || value === undefined) return "";
  return String(Number(value) / 100);
}

function isPlaceAssociationField(key: string): boolean {
  return ["placeId", "fromPlaceId", "toPlaceId"].includes(key);
}

function mergeFieldOptions(
  primary?: FieldOption[],
  extra?: FieldOption[]
): FieldOption[] | undefined {
  if (!primary && !extra) return undefined;
  const merged = new Map((primary ?? []).map((option) => [option.value, option]));
  extra?.forEach((option) => merged.set(option.value, option));
  return [...merged.values()];
}

function requiredField(
  definition: ResourceDefinition,
  key: string,
  data: Record<string, unknown>
): boolean {
  if (key === definition.fields[0]?.[0]) return true;
  if (definition.id === "days" && key === "date") return true;
  if (definition.id === "places" && key === "type") return true;
  if (definition.id === "bookings") {
    return ["item", "type", "bookerMemberId"].includes(key);
  }
  if (definition.id === "segments") {
    return ["dayId", "fromPlaceId", "toPlaceId", "mode"].includes(key);
  }
  if (definition.id === "research-evidence") {
    return ["title", "content", "platformId", "recordedByMemberId"].includes(key);
  }
  if (definition.id !== "expenses") return false;
  if (["type", "splitMode", "amountMinor", "payerMemberId"].includes(key)) return true;
  return key === "splitMemberIds" && data.splitMode === "selected";
}

function preserveSystemFields(
  definition: ResourceDefinition,
  initial?: Record<string, unknown>
): Record<string, unknown> {
  if (!initial) return {};
  const fields: Partial<Record<ResourceDefinition["id"], string[]>> = {
    days: ["placeEntries"],
    expenses: ["bookingRecordId"]
  };
  const preserved = new Set(fields[definition.id] ?? []);
  return Object.fromEntries(
    Object.entries(initial).filter(([key]) => preserved.has(key))
  );
}

function splitTags(value: unknown): string[] {
  return String(value ?? "")
    .split(/[，,、]/)
    .map((item) => item.trim())
    .filter(Boolean);
}

export function normalizeRecordData(
  definition: ResourceDefinition,
  data: Record<string, unknown>
): Record<string, unknown> {
  const normalized: Record<string, unknown> = {};
  for (const [key, , type] of definition.fields) {
    const value = data[key];
    if (type === "tags" || type === "url-list") {
      normalized[key] = splitTags(value);
      continue;
    }
    if (type === "multiselect") {
      normalized[key] = Array.isArray(value)
        ? value.map(String).filter(Boolean)
        : [];
      continue;
    }
    if (type === "checkbox") {
      normalized[key] = Boolean(value);
      continue;
    }
    if (type === "money") {
      if (value !== "" && value !== null && value !== undefined) {
        normalized[key] = Math.round(Number(value) * 100);
      }
      continue;
    }
    if (type === "number") {
      if (value !== "" && value !== null && value !== undefined) {
        normalized[key] = Number(value);
      }
      continue;
    }
    if (typeof value === "string" && value.trim() === "") continue;
    if (value !== null && value !== undefined) normalized[key] = value;
  }
  return normalized;
}
