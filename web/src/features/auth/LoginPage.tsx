// 登录页收集凭据并建立会话，错误内容由 API 客户端统一转译。
import { useMutation } from "@tanstack/react-query";
import { useState } from "react";
import { ThemeToggle } from "../../components/ThemeToggle";
import { api } from "../../lib/api";
import type { Session } from "../../lib/types";

export function LoginPage({ onSession }: { onSession: (session: Session) => void }) {
  const [mobile, setMobile] = useState("");
  const [password, setPassword] = useState("");
  const [showPassword, setShowPassword] = useState(false);
  const [error, setError] = useState("");
  const mutation = useMutation({
    mutationFn: () => api.login(mobile, password),
    onSuccess: (session) => onSession(session),
    onError: (reason) => setError(reason instanceof Error ? reason.message : "登录失败，请重试")
  });

  return (
    <main className="login-page">
      <section className="login-context">
        <div className="login-brand">
          <span className="brand-mark">行</span>
        </div>
        <div className="login-product">
          <h1>山河行记</h1>
        </div>
      </section>
      <section className="login-panel">
        <div className="login-theme"><ThemeToggle /></div>
        <div className="login-form-wrap">
          <div className="mobile-brand">
            <span className="brand-mark">行</span>
            <span>山河行记</span>
          </div>
          <h2>欢迎回来</h2>
          <form
            onSubmit={(event) => {
              event.preventDefault();
              setError("");
              mutation.mutate();
            }}
          >
            <label htmlFor="mobile"><span>手机号</span></label>
            <input
              id="mobile"
              name="mobile"
              autoComplete="tel"
              inputMode="numeric"
              pattern="[0-9]{11}"
              minLength={11}
              maxLength={11}
              spellCheck={false}
              value={mobile}
              onChange={(event) => setMobile(
                event.target.value.replace(/\D/g, "").slice(0, 11)
              )}
              required
            />
            <label htmlFor="password"><span>密码</span></label>
            <div className="password-field">
              <input
                id="password"
                name="password"
                autoComplete="current-password"
                spellCheck={false}
                type={showPassword ? "text" : "password"}
                value={password}
                onChange={(event) => setPassword(event.target.value)}
                required
              />
              <button
                type="button"
                aria-label={showPassword ? "隐藏密码" : "显示密码"}
                onClick={() => setShowPassword((value) => !value)}
              >
                {showPassword ? "隐藏" : "显示"}
              </button>
            </div>
            {error && <p className="form-error" role="alert">{error}</p>}
            <button className="primary-button" type="submit" disabled={mutation.isPending}>
              {mutation.isPending ? "登录中…" : "登录"}
            </button>
          </form>
        </div>
      </section>
    </main>
  );
}
