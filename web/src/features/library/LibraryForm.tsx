// 基础资料编辑表单独立管理字段状态、校验与提交负载。
import { useEffect, useMemo, useState, type FormEvent } from "react";
import type { LibraryRecord, LibraryResource } from "../../lib/types";

export function LibraryForm({
  resource,
  initial,
  selfMode,
  pending,
  onDirtyChange,
  onCancel,
  onSubmit
}: {
  resource: LibraryResource;
  initial?: LibraryRecord;
  selfMode: boolean;
  pending: boolean;
  onDirtyChange: (dirty: boolean) => void;
  onCancel: () => void;
  onSubmit: (
    data: Record<string, unknown>,
    password: string | undefined,
    profile: {
      displayName: string;
      mobile: string;
      alternateContact: string;
      emergencyContact: string;
      currentPassword: string;
      newPassword: string;
    }
  ) => Promise<void>;
}) {
  const defaults = useMemo(() => ({
    name: String(initial?.data.name ?? ""),
    displayName: String(initial?.data.displayName ?? ""),
    status: String(initial?.data.status ?? "active"),
    note: String(initial?.data.note ?? ""),
    color: String(initial?.data.color ?? "#477566"),
    category: String(initial?.data.category ?? "general"),
    mobile: String(initial?.data.mobile ?? ""),
    alternateContact: String(initial?.data.alternateContact ?? ""),
    emergencyContact: String(initial?.data.emergencyContact ?? ""),
    admin: Boolean(initial?.data.admin),
    password: "",
    currentPassword: ""
  }), [initial]);
  const [form, setForm] = useState(defaults);
  const [submitting, setSubmitting] = useState(false);
  const dirty = JSON.stringify(form) !== JSON.stringify(defaults);
  const mobileChanged = form.mobile.trim() !== String(initial?.data.mobile ?? "");

  useEffect(() => {
    onDirtyChange(dirty);
    return () => onDirtyChange(false);
  }, [dirty, onDirtyChange]);

  const set = (key: keyof typeof form, value: string | boolean) => {
    setForm((current) => ({ ...current, [key]: value }));
  };
  const submit = async (event: FormEvent) => {
    event.preventDefault();
    setSubmitting(true);
    try {
      const data: Record<string, unknown> = resource === "members"
        ? {
          displayName: form.displayName.trim(),
          mobile: form.mobile.trim(),
          alternateContact: form.alternateContact.trim(),
          emergencyContact: form.emergencyContact.trim(),
          admin: form.admin,
          status: form.status,
          ...(form.note.trim() ? { note: form.note.trim() } : {})
        }
        : {
          name: form.name.trim(),
          color: form.color,
          category: form.category,
          status: form.status
        };
      await onSubmit(
        data,
        resource === "members" && form.password ? form.password : undefined,
        {
          displayName: form.displayName.trim(),
          mobile: form.mobile.trim(),
          alternateContact: form.alternateContact.trim(),
          emergencyContact: form.emergencyContact.trim(),
          currentPassword: form.currentPassword,
          newPassword: form.password
        }
      );
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <form className="library-form" onSubmit={submit} aria-busy={submitting || pending}>
      {resource === "members" ? (
        <>
          <label>
            <span>姓名<i aria-hidden="true">必填</i></span>
            <input
              name="displayName"
              required
              autoComplete="off"
              value={form.displayName}
              onChange={(event) => set("displayName", event.target.value)}
            />
          </label>
          <label>
            <span>手机号{selfMode && <i aria-hidden="true">必填</i>}</span>
            <input
              name="mobile"
              type="tel"
              inputMode="numeric"
              autoComplete="tel"
              pattern="[0-9]{11}"
              minLength={11}
              maxLength={11}
              required={selfMode || Boolean(form.password)}
              placeholder={selfMode ? "用于登录" : "11 位手机号，可留空"}
              value={form.mobile}
              onChange={(event) => set(
                "mobile",
                event.target.value.replace(/\D/g, "").slice(0, 11)
              )}
            />
          </label>
          <label>
            <span>备用联系方式</span>
            <input
              name="alternateContact"
              autoComplete="off"
              value={form.alternateContact}
              onChange={(event) => set("alternateContact", event.target.value)}
            />
          </label>
          <label>
            <span>紧急联系人联系方式</span>
            <input
              name="emergencyContact"
              autoComplete="off"
              value={form.emergencyContact}
              onChange={(event) => set("emergencyContact", event.target.value)}
            />
          </label>
          {!selfMode && (
            <label>
              <span>备注</span>
              <textarea
                name="note"
                rows={3}
                value={form.note}
                onChange={(event) => set("note", event.target.value)}
              />
            </label>
          )}
          <fieldset>
            <legend>登录安全</legend>
            {selfMode && (
              <label>
                <span>当前密码</span>
                <input
                  name="currentPassword"
                  type="password"
                  minLength={6}
                  required={mobileChanged || Boolean(form.password)}
                  autoComplete="current-password"
                  placeholder="修改手机号或密码时必填"
                  value={form.currentPassword}
                  onChange={(event) => set("currentPassword", event.target.value)}
                />
              </label>
            )}
            <label>
              <span>{selfMode ? "新密码" : "设置或重置密码"}</span>
              <input
                name="password"
                type="password"
                minLength={6}
                autoComplete="new-password"
                placeholder="留空则保持原密码"
                value={form.password}
                onChange={(event) => set("password", event.target.value)}
              />
              <small>
                {selfMode
                  ? "修改成功后当前登录状态保持不变。"
                  : "手机号和密码同时存在时，该成员可以登录。"}
              </small>
            </label>
            {!selfMode && (
              <label className="checkbox">
                <input
                  name="admin"
                  type="checkbox"
                  checked={form.admin}
                  onChange={(event) => set("admin", event.target.checked)}
                />
                <span>管理员权限</span>
              </label>
            )}
          </fieldset>
        </>
      ) : (
        <>
          <label>
            <span>标签名称<i aria-hidden="true">必填</i></span>
            <input
              name="name"
              required
              autoComplete="off"
              value={form.name}
              onChange={(event) => set("name", event.target.value)}
            />
          </label>
          <label>
            <span>标签分类</span>
            <select
              name="category"
              value={form.category}
              onChange={(event) => set("category", event.target.value)}
            >
              <option value="general">通用</option>
              <option value="place-type">地点类型</option>
              <option value="expense-type">费用类型</option>
              <option value="platform-type">平台类型</option>
            </select>
          </label>
          <label>
            <span>标识颜色</span>
            <input
              name="color"
              type="color"
              value={form.color}
              onChange={(event) => set("color", event.target.value)}
            />
          </label>
        </>
      )}
      {!selfMode && (
        <label>
          <span>状态</span>
          <select
            name="status"
            value={form.status}
            onChange={(event) => set("status", event.target.value)}
          >
            <option value="active">启用</option>
            <option value="inactive">停用</option>
          </select>
        </label>
      )}
      <div className="form-actions">
        <button className="secondary-button" type="button" onClick={onCancel}>取消</button>
        <button className="primary-button" type="submit" disabled={submitting || pending}>
          {submitting || pending ? "保存中…" : "保存"}
        </button>
      </div>
    </form>
  );
}
