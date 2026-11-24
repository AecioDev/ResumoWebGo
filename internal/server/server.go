package server

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"resumo-planilhas/internal/auth"
	"resumo-planilhas/internal/config"
	"resumo-planilhas/internal/gdrive"
	"resumo-planilhas/internal/resumo"
)

//go:embed web/*
var webFiles embed.FS

type Options struct {
	BaseURL    string
	ServerMode bool
	AuthStore  *auth.Store
	ShutdownCh chan struct{}
}

type Server struct {
	store      *config.Store
	drive      *gdrive.Client
	resumoSvc  *resumo.Service
	authStore  *auth.Store
	baseURL    string
	serverMode bool
	shutdownCh chan struct{}
}

func New(store *config.Store, drive *gdrive.Client, resumoSvc *resumo.Service, opts Options) *Server {
	return &Server{
		store:      store,
		drive:      drive,
		resumoSvc:  resumoSvc,
		authStore:  opts.AuthStore,
		baseURL:    strings.TrimRight(opts.BaseURL, "/"),
		serverMode: opts.ServerMode,
		shutdownCh: opts.ShutdownCh,
	}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /api/health", s.handleHealth)
	mux.HandleFunc("GET /api/status", s.handleStatus)

	mux.HandleFunc("POST /api/login", s.handleLogin)
	mux.HandleFunc("POST /api/logout", s.handleLogout)
	mux.HandleFunc("GET /acesso", s.handleTokenAccess)

	mux.HandleFunc("POST /api/credentials", s.requireAdmin(s.handleSaveCredentials))
	mux.HandleFunc("GET /auth/login", s.requireAdmin(s.handleAuthLogin))
	mux.HandleFunc("GET /auth/callback", s.requireAdmin(s.handleAuthCallback))
	mux.HandleFunc("POST /api/auth/logout", s.requireAdmin(s.handleAuthLogout))

	mux.HandleFunc("GET /api/empresas", s.requireAuth(s.handleListEmpresas))
	mux.HandleFunc("POST /api/empresas", s.requireAdmin(s.handleSaveEmpresa))
	mux.HandleFunc("DELETE /api/empresas/{id}", s.requireAdmin(s.handleDeleteEmpresa))

	mux.HandleFunc("GET /api/users", s.requireAdminOrGestor(s.handleListUsers))
	mux.HandleFunc("POST /api/users", s.requireAdminOrGestor(s.handleSaveUser))
	mux.HandleFunc("DELETE /api/users/{id}", s.requireAdminOrGestor(s.handleDeleteUser))
	mux.HandleFunc("POST /api/users/{id}/token", s.requireAdminOrGestor(s.handleGenerateUserToken))
	mux.HandleFunc("DELETE /api/users/{id}/token", s.requireAdminOrGestor(s.handleRevokeUserToken))
	mux.HandleFunc("GET /api/users/{id}/atalho", s.requireAdminOrGestor(s.handleDownloadShortcut))
	mux.HandleFunc("GET /api/socios-nomes", s.requireAdminOrGestor(s.handleListarNomesSocios))

	mux.HandleFunc("GET /api/drive/folders", s.requireAdmin(s.handleListFolders))
	mux.HandleFunc("GET /api/drive/folder-info", s.requireAdmin(s.handleFolderInfo))

	mux.HandleFunc("GET /api/resumo/socios", s.requireAuth(s.handleResumoSocios))
	mux.HandleFunc("GET /api/resumo/acerto/meses", s.requireAuth(s.handleAcertoMeses))
	mux.HandleFunc("GET /api/resumo/acerto", s.requireAuth(s.handleResumoAcerto))

	mux.HandleFunc("GET /api/export/socios", s.requireAuth(s.handleExportSocios))
	mux.HandleFunc("GET /api/export/acerto", s.requireAuth(s.handleExportAcerto))

	mux.HandleFunc("POST /api/cache/clear", s.requireAdmin(s.handleClearCache))
	mux.HandleFunc("POST /api/shutdown", s.handleShutdown)

	subFS, err := fs.Sub(webFiles, "web")
	if err != nil {
		panic(err)
	}
	fileServer := http.FileServer(http.FS(subFS))
	mux.Handle("/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-cache")
		fileServer.ServeHTTP(w, r)
	}))

	return mux
}

// ===== Autenticação e Autorização =====

func (s *Server) currentActor(r *http.Request) (auth.User, bool) {
	if !s.serverMode || s.authStore == nil {
		return auth.User{
			ID:       "local-admin",
			Username: "local",
			Nome:     "Local",
			Role:     auth.RoleAdmin,
			Ativo:    true,
		}, true
	}
	return s.authStore.UserFromRequest(r)
}

func (s *Server) requireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if _, ok := s.currentActor(r); !ok {
			writeError(w, http.StatusUnauthorized, "Faça login para continuar")
			return
		}
		next(w, r)
	}
}

func (s *Server) requireAdmin(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor, ok := s.currentActor(r)
		if !ok {
			writeError(w, http.StatusUnauthorized, "Faça login para continuar")
			return
		}
		if actor.Role != auth.RoleAdmin {
			writeError(w, http.StatusForbidden, "Acesso restrito ao administrador")
			return
		}
		next(w, r)
	}
}

func (s *Server) requireAdminOrGestor(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor, ok := s.currentActor(r)
		if !ok {
			writeError(w, http.StatusUnauthorized, "Faça login para continuar")
			return
		}
		if actor.Role != auth.RoleAdmin && actor.Role != auth.RoleGestor {
			writeError(w, http.StatusForbidden, "Sem permissão para gerenciar acessos")
			return
		}
		next(w, r)
	}
}

func (s *Server) visibleEmpresas(actor auth.User) []config.EmpresaConfig {
	all := s.store.GetConfig().Empresas
	out := make([]config.EmpresaConfig, 0, len(all))
	for _, emp := range all {
		if !actor.HasEmpresa(emp.ID) {
			continue
		}
		if actor.Role != auth.RoleAdmin {
			// Não expõe IDs internos do Google Drive para gestores/sócios, mantendo apenas a lista de anos disponíveis
			sanitized := config.EmpresaConfig{
				ID:           emp.ID,
				Nome:         emp.Nome,
				Modo:         emp.Modo,
				PastasPorAno: map[string]config.PastaAnoRef{},
			}
			for y := range emp.PastasPorAno {
				sanitized.PastasPorAno[y] = config.PastaAnoRef{}
			}
			out = append(out, sanitized)
		} else {
			out = append(out, emp)
		}
	}
	return out
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":         true,
		"app":        "ResumoPlanilhas",
		"serverMode": s.serverMode,
	})
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	if s.authStore == nil {
		writeError(w, http.StatusBadRequest, "Autenticação não está habilitada neste modo")
		return
	}
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "Dados de login inválidos")
		return
	}
	user, err := s.authStore.Authenticate(req.Username, req.Password)
	if err != nil {
		writeError(w, http.StatusUnauthorized, err.Error())
		return
	}
	s.authStore.SetSessionCookie(w, r, user)
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":   true,
		"user": user,
	})
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if s.authStore != nil {
		s.authStore.ClearSessionCookie(w, r)
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// handleTokenAccess autentica via link de atalho (/acesso?token=...&empresaId=...&ano=...&tipo=...)
// grava o cookie de sessão HttpOnly e redireciona para / removendo o token da barra de endereços.
func (s *Server) handleTokenAccess(w http.ResponseWriter, r *http.Request) {
	if s.serverMode && s.authStore != nil {
		token := strings.TrimSpace(r.URL.Query().Get("token"))
		user, err := s.authStore.AuthenticateByToken(token)
		if err != nil {
			http.Redirect(w, r, "/?authError="+url.QueryEscape(err.Error()), http.StatusFound)
			return
		}
		s.authStore.SetSessionCookie(w, r, user)

		q := url.Values{}
		if empID := strings.TrimSpace(r.URL.Query().Get("empresaId")); empID != "" && user.HasEmpresa(empID) {
			q.Set("empresaId", empID)
		}
		if ano := strings.TrimSpace(r.URL.Query().Get("ano")); ano != "" {
			if y, err := strconv.Atoi(ano); err == nil && y >= 2000 && y <= 2100 {
				q.Set("ano", ano)
			}
		}
		if tipo := strings.ToUpper(strings.TrimSpace(r.URL.Query().Get("tipo"))); tipo == "S" || tipo == "A" {
			q.Set("tipo", tipo)
		}
		if mes := strings.TrimSpace(r.URL.Query().Get("mes")); mes != "" {
			if m, err := strconv.Atoi(mes); err == nil && m >= 1 && m <= 12 {
				q.Set("mes", mes)
			}
		}

		dest := "/"
		if encoded := q.Encode(); encoded != "" {
			dest = "/?" + encoded
		}
		http.Redirect(w, r, dest, http.StatusFound)
		return
	}

	http.Redirect(w, r, "/", http.StatusFound)
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	actor, loggedIn := s.currentActor(r)
	if !loggedIn {
		writeJSON(w, http.StatusOK, map[string]any{
			"serverMode": true,
			"loggedIn":   false,
			"anoAtual":   time.Now().Year(),
			"mesAtual":   int(time.Now().Month()),
		})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	driveStatus := s.drive.GetAuthStatus(ctx)
	if actor.Role != auth.RoleAdmin {
		// Não expõe e-mail interno da Service Account para usuários não-admin
		driveStatus = gdrive.AuthStatus{
			HasCredentials: driveStatus.HasCredentials,
			AuthMode:       driveStatus.AuthMode,
			Authenticated:  driveStatus.Authenticated,
		}
	}

	resp := map[string]any{
		"serverMode":  s.serverMode,
		"loggedIn":    true,
		"user":        actor,
		"auth":        driveStatus,
		"empresas":    s.visibleEmpresas(actor),
		"redirectUri": s.callbackURL(r),
		"anoAtual":    time.Now().Year(),
		"mesAtual":    int(time.Now().Month()),
	}
	if actor.Role == auth.RoleAdmin {
		resp["dataDir"] = s.store.BaseDir()
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleSaveCredentials(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		writeError(w, http.StatusBadRequest, "Erro ao ler corpo da requisição")
		return
	}
	if err := s.store.SaveCredentialsJSON(body); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	s.drive.InvalidateAuthCache()
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":      true,
		"message": "Credenciais salvas com sucesso!",
	})
}

func (s *Server) handleAuthLogin(w http.ResponseWriter, r *http.Request) {
	authURL, err := s.drive.StartAuthURL(s.callbackURL(r))
	if err != nil {
		http.Redirect(w, r, "/?authError="+url.QueryEscape(err.Error()), http.StatusFound)
		return
	}
	http.Redirect(w, r, authURL, http.StatusFound)
}

func (s *Server) handleAuthCallback(w http.ResponseWriter, r *http.Request) {
	if errMsg := r.URL.Query().Get("error"); errMsg != "" {
		http.Redirect(w, r, "/?authError="+url.QueryEscape("Autorização cancelada: "+errMsg), http.StatusFound)
		return
	}

	code := r.URL.Query().Get("code")
	state := r.URL.Query().Get("state")

	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()

	if err := s.drive.ExchangeCode(ctx, s.callbackURL(r), state, code); err != nil {
		http.Redirect(w, r, "/?authError="+url.QueryEscape(err.Error()), http.StatusFound)
		return
	}
	s.drive.InvalidateAuthCache()
	http.Redirect(w, r, "/?authOk=1", http.StatusFound)
}

func (s *Server) handleAuthLogout(w http.ResponseWriter, r *http.Request) {
	if err := s.drive.Disconnect(); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleListEmpresas(w http.ResponseWriter, r *http.Request) {
	actor, _ := s.currentActor(r)
	writeJSON(w, http.StatusOK, s.visibleEmpresas(actor))
}

func (s *Server) handleSaveEmpresa(w http.ResponseWriter, r *http.Request) {
	var emp config.EmpresaConfig
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&emp); err != nil {
		writeError(w, http.StatusBadRequest, "Dados da empresa inválidos")
		return
	}

	saved, err := s.store.UpsertEmpresa(emp)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, saved)
}

func (s *Server) handleDeleteEmpresa(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if strings.TrimSpace(id) == "" {
		writeError(w, http.StatusBadRequest, "ID da empresa não informado")
		return
	}
	if err := s.store.DeleteEmpresa(id); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// ===== Gestão de Usuários (Admin e Gestor) =====

func (s *Server) handleListUsers(w http.ResponseWriter, r *http.Request) {
	if s.authStore == nil {
		writeJSON(w, http.StatusOK, []auth.User{})
		return
	}
	actor, _ := s.currentActor(r)
	writeJSON(w, http.StatusOK, s.authStore.ListForActor(actor))
}

func (s *Server) handleSaveUser(w http.ResponseWriter, r *http.Request) {
	if s.authStore == nil {
		writeError(w, http.StatusBadRequest, "Gestão de usuários disponível apenas no modo servidor")
		return
	}
	actor, _ := s.currentActor(r)
	var in auth.UserInput
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, "Dados do usuário inválidos")
		return
	}
	saved, err := s.authStore.UpsertForActor(actor, in)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, saved)
}

func (s *Server) handleDeleteUser(w http.ResponseWriter, r *http.Request) {
	if s.authStore == nil {
		writeError(w, http.StatusBadRequest, "Gestão de usuários disponível apenas no modo servidor")
		return
	}
	actor, _ := s.currentActor(r)
	id := r.PathValue("id")
	if err := s.authStore.DeleteForActor(actor, id); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleGenerateUserToken(w http.ResponseWriter, r *http.Request) {
	if s.authStore == nil {
		writeError(w, http.StatusBadRequest, "Disponível apenas no modo servidor")
		return
	}
	actor, _ := s.currentActor(r)
	id := r.PathValue("id")
	updated, err := s.authStore.GenerateAccessToken(actor, id)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, updated)
}

func (s *Server) handleRevokeUserToken(w http.ResponseWriter, r *http.Request) {
	if s.authStore == nil {
		writeError(w, http.StatusBadRequest, "Disponível apenas no modo servidor")
		return
	}
	actor, _ := s.currentActor(r)
	id := r.PathValue("id")
	updated, err := s.authStore.RevokeAccessToken(actor, id)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, updated)
}

func (s *Server) handleDownloadShortcut(w http.ResponseWriter, r *http.Request) {
	if s.authStore == nil {
		writeError(w, http.StatusBadRequest, "Disponível apenas no modo servidor")
		return
	}
	actor, _ := s.currentActor(r)
	id := r.PathValue("id")
	targetUser, err := s.authStore.GetManagedByID(actor, id)
	if err != nil {
		writeError(w, http.StatusForbidden, err.Error())
		return
	}
	if strings.TrimSpace(targetUser.AccessToken) == "" {
		writeError(w, http.StatusBadRequest, "Gere primeiro o token de acesso deste usuário")
		return
	}

	empID := strings.TrimSpace(r.URL.Query().Get("empresaId"))
	ano := strings.TrimSpace(r.URL.Query().Get("ano"))
	tipo := strings.ToUpper(strings.TrimSpace(r.URL.Query().Get("tipo")))

	q := url.Values{}
	q.Set("token", targetUser.AccessToken)

	empNome := ""
	if empID != "" {
		if emp, ok := s.store.GetEmpresa(empID); ok && targetUser.HasEmpresa(empID) {
			q.Set("empresaId", emp.ID)
			empNome = emp.Nome
		}
	}
	if ano != "" {
		q.Set("ano", ano)
	}
	if tipo == "S" || tipo == "A" {
		q.Set("tipo", tipo)
	}

	origin := s.publicOrigin(r)
	accessURL := origin + "/acesso?" + q.Encode()

	title := "Resumo de Planilhas"
	if empNome != "" {
		title = "Resumo — " + empNome
		if ano != "" {
			title += " (" + ano + ")"
		}
	}

	jsURL, _ := json.Marshal(accessURL)
	escURL := html.EscapeString(accessURL)
	escTitle := html.EscapeString(title)

	content := fmt.Sprintf(`<!DOCTYPE html>
<html lang="pt-BR">
<head>
  <meta charset="UTF-8" />
  <meta http-equiv="refresh" content="0; url=%s" />
  <title>%s</title>
  <style>
    body{margin:0;min-height:100vh;display:flex;align-items:center;justify-content:center;font-family:Segoe UI,Arial,sans-serif;background:#0f172a;color:#f8fafc}
    .box{background:#1e293b;padding:28px 34px;border-radius:12px;text-align:center;box-shadow:0 12px 32px rgba(0,0,0,.35)}
    a{display:inline-block;margin-top:14px;padding:10px 18px;border-radius:8px;background:#2563eb;color:#fff;text-decoration:none;font-weight:600}
  </style>
</head>
<body>
  <div class="box">
    <h2>%s</h2>
    <p>Abrindo painel de resumo...</p>
    <a href="%s">Abrir agora</a>
  </div>
  <script>window.location.replace(%s);</script>
</body>
</html>
`, escURL, escTitle, escTitle, escURL, string(jsURL))

	fileName := "#_RESUMO"
	if empNome != "" {
		fileName += "_" + sanitizeFilename(empNome)
	}
	if targetUser.Role == auth.RoleSocio && targetUser.SocioNome != "" {
		fileName += "_" + sanitizeFilename(targetUser.SocioNome)
	}
	if ano != "" {
		fileName += "_" + sanitizeFilename(ano)
	}
	fileName += ".html"

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, fileName))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(content))
}

func (s *Server) publicOrigin(r *http.Request) string {
	if s.baseURL != "" {
		return s.baseURL
	}
	scheme := "http"
	if r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https") {
		scheme = "https"
	}
	return scheme + "://" + r.Host
}

func (s *Server) handleListarNomesSocios(w http.ResponseWriter, r *http.Request) {
	actor, _ := s.currentActor(r)
	emp, ano, _, ok := s.parseEmpresaAnoParams(w, r, actor)
	if !ok {
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()

	nomes, err := s.resumoSvc.ListarNomesSocios(ctx, emp, ano)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if nomes == nil {
		nomes = []string{}
	}
	writeJSON(w, http.StatusOK, nomes)
}

// ===== Google Drive & Resumos =====

func (s *Server) handleListFolders(w http.ResponseWriter, r *http.Request) {
	parentID := strings.TrimSpace(r.URL.Query().Get("parentId"))
	shared := r.URL.Query().Get("shared") == "true"

	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()

	folders, err := s.drive.ListFolders(ctx, parentID, shared)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	if folders == nil {
		folders = []gdrive.FolderItem{}
	}
	writeJSON(w, http.StatusOK, folders)
}

func (s *Server) handleFolderInfo(w http.ResponseWriter, r *http.Request) {
	id := config.ExtractDriveFolderID(r.URL.Query().Get("id"))
	if id == "" {
		writeError(w, http.StatusBadRequest, "ID ou URL de pasta não informado")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()

	info, err := s.drive.GetFolderInfo(ctx, id)
	if err != nil {
		writeError(w, http.StatusBadGateway, "Não foi possível localizar esta pasta no Google Drive: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, info)
}

func (s *Server) handleResumoSocios(w http.ResponseWriter, r *http.Request) {
	actor, _ := s.currentActor(r)
	emp, ano, refresh, ok := s.parseEmpresaAnoParams(w, r, actor)
	if !ok {
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
	defer cancel()

	data, err := s.resumoSvc.GerarResumoSocios(ctx, emp, ano, refresh)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if actor.Role == auth.RoleSocio {
		data = resumo.FilterSociosForSocio(data, actor.SocioNome)
	}
	writeJSON(w, http.StatusOK, data)
}

func (s *Server) handleAcertoMeses(w http.ResponseWriter, r *http.Request) {
	actor, _ := s.currentActor(r)
	emp, ano, _, ok := s.parseEmpresaAnoParams(w, r, actor)
	if !ok {
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()

	meses, err := s.resumoSvc.ListarMesesAcerto(ctx, emp, ano)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if meses == nil {
		meses = []resumo.MesAcertoDisponivel{}
	}
	writeJSON(w, http.StatusOK, meses)
}

func (s *Server) handleResumoAcerto(w http.ResponseWriter, r *http.Request) {
	actor, _ := s.currentActor(r)
	emp, ano, refresh, ok := s.parseEmpresaAnoParams(w, r, actor)
	if !ok {
		return
	}
	mes, err := strconv.Atoi(r.URL.Query().Get("mes"))
	if err != nil || mes < 1 || mes > 12 {
		mes = int(time.Now().Month())
	}

	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
	defer cancel()

	data, err := s.resumoSvc.GerarResumoAcerto(ctx, emp, ano, mes, refresh)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if actor.Role == auth.RoleSocio {
		data = resumo.FilterAcertoForSocio(data, actor.SocioNome)
	}
	writeJSON(w, http.StatusOK, data)
}

func (s *Server) handleExportSocios(w http.ResponseWriter, r *http.Request) {
	actor, _ := s.currentActor(r)
	emp, ano, refresh, ok := s.parseEmpresaAnoParams(w, r, actor)
	if !ok {
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
	defer cancel()

	data, err := s.resumoSvc.GerarResumoSocios(ctx, emp, ano, refresh)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if actor.Role == auth.RoleSocio {
		data = resumo.FilterSociosForSocio(data, actor.SocioNome)
	}
	xlsxBytes, err := resumo.ExportSociosXLSX(data)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Erro ao gerar Excel: "+err.Error())
		return
	}

	suffix := emp.Nome
	if actor.Role == auth.RoleSocio {
		suffix = emp.Nome + "_" + actor.SocioNome
	}
	fileName := fmt.Sprintf("#_RESUMO_%s_%d.xlsx", sanitizeFilename(suffix), ano)
	w.Header().Set("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, fileName))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(xlsxBytes)
}

func (s *Server) handleExportAcerto(w http.ResponseWriter, r *http.Request) {
	actor, _ := s.currentActor(r)
	emp, ano, refresh, ok := s.parseEmpresaAnoParams(w, r, actor)
	if !ok {
		return
	}
	mes, err := strconv.Atoi(r.URL.Query().Get("mes"))
	if err != nil || mes < 1 || mes > 12 {
		mes = int(time.Now().Month())
	}

	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
	defer cancel()

	data, err := s.resumoSvc.GerarResumoAcerto(ctx, emp, ano, mes, refresh)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if actor.Role == auth.RoleSocio {
		data = resumo.FilterAcertoForSocio(data, actor.SocioNome)
	}
	xlsxBytes, err := resumo.ExportAcertoXLSX(data)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Erro ao gerar Excel: "+err.Error())
		return
	}

	fileName := fmt.Sprintf("#_RESUMO_ACERTO_SEMANAL_%s_%d.xlsx", sanitizeFilename(strings.ToUpper(data.MesNome)), ano)
	w.Header().Set("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, fileName))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(xlsxBytes)
}

func (s *Server) handleClearCache(w http.ResponseWriter, r *http.Request) {
	removed, err := s.store.ClearCache()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":      true,
		"removed": removed,
	})
}

func (s *Server) handleShutdown(w http.ResponseWriter, r *http.Request) {
	if s.serverMode {
		writeError(w, http.StatusForbidden, "Encerramento remoto desabilitado no modo servidor")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	if s.shutdownCh != nil {
		go func() {
			time.Sleep(250 * time.Millisecond)
			select {
			case s.shutdownCh <- struct{}{}:
			default:
			}
		}()
	}
}

func (s *Server) parseEmpresaAnoParams(w http.ResponseWriter, r *http.Request, actor auth.User) (config.EmpresaConfig, int, bool, bool) {
	empID := strings.TrimSpace(r.URL.Query().Get("empresaId"))
	if !actor.HasEmpresa(empID) {
		writeError(w, http.StatusForbidden, "Você não tem acesso a esta empresa")
		return config.EmpresaConfig{}, 0, false, false
	}
	emp, ok := s.store.GetEmpresa(empID)
	if !ok {
		writeError(w, http.StatusNotFound, "Empresa não encontrada")
		return config.EmpresaConfig{}, 0, false, false
	}

	ano, err := strconv.Atoi(r.URL.Query().Get("ano"))
	if err != nil || ano < 2000 || ano > 2100 {
		ano = time.Now().Year()
	}
	refresh := r.URL.Query().Get("refresh") == "true"
	return emp, ano, refresh, true
}

func (s *Server) callbackURL(r *http.Request) string {
	if s.baseURL != "" {
		return s.baseURL + "/auth/callback"
	}
	scheme := "http"
	if r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https") {
		scheme = "https"
	}
	return scheme + "://" + r.Host + "/auth/callback"
}

func sanitizeFilename(name string) string {
	replacer := strings.NewReplacer("/", "_", "\\", "_", ":", "_", "*", "_", "?", "_", "\"", "_", "<", "_", ">", "_", "|", "_")
	return strings.TrimSpace(replacer.Replace(name))
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]any{
		"error": msg,
	})
}
