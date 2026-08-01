// 认证处理器负责 bootstrap、会话 Cookie 和 CSRF。
package httpapi

import (
	"crypto/subtle"
	"errors"
	"net/http"

	"my-travel/server/internal/model"
	"my-travel/server/internal/repository"
)

func (s *Server) bootstrap(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Token       string `json:"token"`
		DisplayName string `json:"displayName"`
		Mobile      string `json:"mobile"`
		Password    string `json:"password"`
	}
	if !decode(w, r, &input) {
		return
	}
	user, err := s.service.Bootstrap(
		r.Context(),
		input.Token,
		input.DisplayName,
		input.Mobile,
		input.Password,
	)
	if err != nil {
		problem(w, http.StatusForbidden, "bootstrap-rejected", "初始化未获授权，或已经完成。", nil)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"user": user})
}

func (s *Server) provisionUser(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Token       string `json:"token"`
		DisplayName string `json:"displayName"`
		Mobile      string `json:"mobile"`
		Password    string `json:"password"`
	}
	if !decode(w, r, &input) {
		return
	}
	user, err := s.service.ProvisionUser(
		r.Context(),
		input.Token,
		input.DisplayName,
		input.Mobile,
		input.Password,
	)
	if err != nil {
		problem(w, http.StatusForbidden, "user-provisioning-rejected", "登录成员创建未获授权，或成员信息不符合要求。", nil)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"user": user})
}

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Mobile   string `json:"mobile"`
		Password string `json:"password"`
	}
	if !decode(w, r, &input) {
		return
	}
	user, session, err := s.service.Login(r.Context(), input.Mobile, input.Password)
	if err != nil {
		problem(w, http.StatusUnauthorized, "invalid-credentials", "手机号或密码不正确。", nil)
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     s.config.CookieName,
		Value:    session.ID,
		Path:     "/",
		HttpOnly: true,
		Secure:   s.config.CookieSecure,
		SameSite: http.SameSiteStrictMode,
		Expires:  session.ExpiresAt,
	})
	writeJSON(w, http.StatusOK, map[string]any{
		"user": user, "csrfToken": session.CSRFToken, "expiresAt": session.ExpiresAt,
	})
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	_, session, ok := s.requireSession(w, r, true)
	if !ok {
		return
	}
	_ = s.repo.DeleteSession(r.Context(), session.ID)
	s.clearSessionCookie(w)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) clearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     s.config.CookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   s.config.CookieSecure,
		SameSite: http.SameSiteStrictMode,
	})
}

func (s *Server) session(w http.ResponseWriter, r *http.Request) {
	user, session, ok := s.requireSession(w, r, false)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"user": user, "csrfToken": session.CSRFToken, "expiresAt": session.ExpiresAt,
	})
}

func (s *Server) requireSession(
	w http.ResponseWriter,
	r *http.Request,
	write bool,
) (model.MemberIdentity, model.Session, bool) {
	cookie, err := r.Cookie(s.config.CookieName)
	if err != nil {
		problem(w, http.StatusUnauthorized, "authentication-required", "请先登录。", nil)
		return model.MemberIdentity{}, model.Session{}, false
	}
	session, err := s.repo.GetSession(r.Context(), cookie.Value)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			s.clearSessionCookie(w)
			problem(w, http.StatusUnauthorized, "session-expired", "登录已过期，请重新登录。", nil)
			return model.MemberIdentity{}, model.Session{}, false
		}
		internal(w, err)
		return model.MemberIdentity{}, model.Session{}, false
	}
	member, err := s.repo.GetMemberIdentity(r.Context(), session.MemberID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			_ = s.repo.DeleteSession(r.Context(), session.ID)
			s.clearSessionCookie(w)
			problem(w, http.StatusUnauthorized, "session-expired", "登录已过期，请重新登录。", nil)
			return model.MemberIdentity{}, model.Session{}, false
		}
		internal(w, err)
		return model.MemberIdentity{}, model.Session{}, false
	}
	if write && subtle.ConstantTimeCompare([]byte(r.Header.Get("X-CSRF-Token")), []byte(session.CSRFToken)) != 1 {
		problem(w, http.StatusForbidden, "csrf-rejected", "安全校验失败，请刷新页面后重试。", nil)
		return model.MemberIdentity{}, model.Session{}, false
	}
	return member, session, true
}
