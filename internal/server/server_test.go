package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"testing"

	"github.com/xuri/excelize/v2"

	"resumo-planilhas/internal/auth"
	"resumo-planilhas/internal/config"
	"resumo-planilhas/internal/gdrive"
	"resumo-planilhas/internal/resumo"
)

type stubDrive struct {
	files   map[string][]gdrive.DriveFile
	content map[string][]byte
}

func (s *stubDrive) ListFolders(_ context.Context, _ string, _ bool) ([]gdrive.FolderItem, error) {
	return nil, nil
}
func (s *stubDrive) ListSpreadsheets(_ context.Context, folderID string) ([]gdrive.DriveFile, error) {
	return s.files[folderID], nil
}
func (s *stubDrive) DownloadSpreadsheet(_ context.Context, file gdrive.DriveFile, _ bool) ([]byte, bool, error) {
	return s.content[file.ID], false, nil
}

func buildTestSocioXLSX(t *testing.T, valorTotal, recebimento, valSocio float64) []byte {
	t.Helper()
	f := excelize.NewFile()
	defer f.Close()
	_ = f.SetSheetName("Sheet1", "Janeiro")
	_ = f.SetCellValue("Janeiro", "B12", valorTotal)
	_ = f.SetCellValue("Janeiro", "D11", recebimento)
	_ = f.SetCellValue("Janeiro", "E20", valSocio)
	var buf bytes.Buffer
	if err := f.Write(&buf); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestServerRoutesAndEmpresaCRUD(t *testing.T) {
	store, err := config.NewStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewStore falhou: %v", err)
	}
	driveClient := gdrive.NewClient(store)
	resumoSvc := resumo.NewService(driveClient)
	shutdownCh := make(chan struct{}, 1)

	srv := New(store, driveClient, resumoSvc, Options{
		BaseURL:    "http://127.0.0.1:8745",
		ServerMode: false,
		ShutdownCh: shutdownCh,
	})
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	for _, path := range []string{"/api/health", "/api/status", "/", "/styles.css", "/app.js"} {
		resp, err := http.Get(ts.URL + path)
		if err != nil {
			t.Fatalf("GET %s falhou: %v", path, err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("GET %s retornou status %d", path, resp.StatusCode)
		}
	}

	payload := map[string]any{
		"nome":          "JS Distribuidora",
		"modo":          "SA",
		"pastaSociosId": "https://drive.google.com/drive/folders/1AbCdEfGhIjKlMnOpQrStUvWxYz",
		"pastaAcertoId": "1ZzYyXxWwVvUuTtSsRrQqPpOoNn",
	}
	body, _ := json.Marshal(payload)
	resp, err := http.Post(ts.URL+"/api/empresas", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("POST /api/empresas falhou: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("POST /api/empresas retornou status %d", resp.StatusCode)
	}

	var saved config.EmpresaConfig
	if err := json.NewDecoder(resp.Body).Decode(&saved); err != nil {
		t.Fatalf("erro ao decodificar resposta: %v", err)
	}
	if saved.Nome != "JS DISTRIBUIDORA" || saved.Modo != "SA" {
		t.Fatalf("empresa salva inesperada: %+v", saved)
	}
	if saved.PastaSociosID != "1AbCdEfGhIjKlMnOpQrStUvWxYz" {
		t.Fatalf("extração de ID da URL do Drive falhou: %q", saved.PastaSociosID)
	}
}

func TestMultiUserIsolationAdminGestorSocio(t *testing.T) {
	dir := t.TempDir()
	store, err := config.NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	authStore, err := auth.NewStore(dir, "test-secret")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := authStore.EnsureAdmin("admin", "admin123"); err != nil {
		t.Fatal(err)
	}

	empJoci, _ := store.UpsertEmpresa(config.EmpresaConfig{
		Nome: "JS DISTRIBUIDORA",
		Modo: config.ModoSocios,
		PastasPorAno: map[string]config.PastaAnoRef{
			"2026": {SociosID: "folder_joci_2026"},
		},
	})
	empSobrinho, _ := store.UpsertEmpresa(config.EmpresaConfig{
		Nome: "EMPRESA SOBRINHO",
		Modo: config.ModoSocios,
		PastasPorAno: map[string]config.PastaAnoRef{
			"2026": {SociosID: "folder_sob_2026"},
		},
	})

	fake := &stubDrive{
		files: map[string][]gdrive.DriveFile{
			"folder_joci_2026": {
				{ID: "f_acassio", Name: "ACASSIO.xlsx", MimeType: gdrive.MimeXLSX, ModifiedTime: "2026-01-01"},
				{ID: "f_vinicius", Name: "VINICIUS.xlsx", MimeType: gdrive.MimeXLSX, ModifiedTime: "2026-01-01"},
			},
		},
		content: map[string][]byte{
			"f_acassio":  buildTestSocioXLSX(t, 3000, 1500, 500),
			"f_vinicius": buildTestSocioXLSX(t, 7000, 3500, 1000),
		},
	}

	driveClient := gdrive.NewClient(store)
	resumoSvc := resumo.NewService(fake)
	srv := New(store, driveClient, resumoSvc, Options{
		ServerMode: true,
		AuthStore:  authStore,
	})
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	// 1) Sem login -> 401
	resp, _ := http.Get(ts.URL + "/api/empresas")
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("esperava 401 sem login, veio %d", resp.StatusCode)
	}
	resp.Body.Close()

	newClientWithLogin := func(user, pass string) *http.Client {
		jar, _ := cookiejar.New(nil)
		c := &http.Client{Jar: jar}
		b, _ := json.Marshal(map[string]string{"username": user, "password": pass})
		r, err := c.Post(ts.URL+"/api/login", "application/json", bytes.NewReader(b))
		if err != nil || r.StatusCode != http.StatusOK {
			t.Fatalf("login %s falhou: err=%v status=%v", user, err, r)
		}
		r.Body.Close()
		return c
	}

	// 2) Admin cria gestor Joci (só empJoci) e gestor Sobrinho (só empSobrinho)
	adminCli := newClientWithLogin("admin", "admin123")
	createUser := func(c *http.Client, payload map[string]any) *http.Response {
		b, _ := json.Marshal(payload)
		r, err := c.Post(ts.URL+"/api/users", "application/json", bytes.NewReader(b))
		if err != nil {
			t.Fatal(err)
		}
		return r
	}

	r := createUser(adminCli, map[string]any{
		"username": "joci", "nome": "Joci", "password": "joci123", "role": "gestor", "empresaIds": []string{empJoci.ID},
	})
	if r.StatusCode != http.StatusOK {
		t.Fatalf("admin criar gestor joci falhou: %d", r.StatusCode)
	}
	r.Body.Close()

	// 3) Gestor Joci só vê empJoci e leva 403 em empSobrinho
	jociCli := newClientWithLogin("joci", "joci123")
	rEmp, _ := jociCli.Get(ts.URL + "/api/empresas")
	var empsJoci []config.EmpresaConfig
	_ = json.NewDecoder(rEmp.Body).Decode(&empsJoci)
	rEmp.Body.Close()
	if len(empsJoci) != 1 || empsJoci[0].ID != empJoci.ID {
		t.Fatalf("joci deveria ver apenas empJoci, veio: %+v", empsJoci)
	}

	rForb, _ := jociCli.Get(ts.URL + "/api/resumo/socios?empresaId=" + empSobrinho.ID + "&ano=2026")
	if rForb.StatusCode != http.StatusForbidden {
		t.Fatalf("joci deveria receber 403 em empSobrinho, veio %d", rForb.StatusCode)
	}
	rForb.Body.Close()

	// 4) Joci cria acesso para o sócio ACASSIO em empJoci (permitido), mas NÃO pode criar em empSobrinho
	rBadSocio := createUser(jociCli, map[string]any{
		"username": "invasor", "password": "1234", "role": "socio", "socioNome": "X", "empresaIds": []string{empSobrinho.ID},
	})
	if rBadSocio.StatusCode == http.StatusOK {
		t.Fatalf("joci não poderia criar sócio na empresa do sobrinho")
	}
	rBadSocio.Body.Close()

	rAcassio := createUser(jociCli, map[string]any{
		"username": "acassio", "password": "ac1234", "role": "socio", "socioNome": "ACASSIO", "empresaIds": []string{empJoci.ID},
	})
	if rAcassio.StatusCode != http.StatusOK {
		t.Fatalf("joci criar sócio acassio falhou: %d", rAcassio.StatusCode)
	}
	rAcassio.Body.Close()

	// 5) Sócio ACASSIO faz login e recebe EXCLUSIVAMENTE os próprios valores (3000, nunca 10000 nem VINICIUS)
	acassioCli := newClientWithLogin("acassio", "ac1234")
	rRes, _ := acassioCli.Get(ts.URL + "/api/resumo/socios?empresaId=" + empJoci.ID + "&ano=2026")
	if rRes.StatusCode != http.StatusOK {
		t.Fatalf("acassio consultar próprio resumo falhou: %d", rRes.StatusCode)
	}
	var resAcassio resumo.ResumoSociosResponse
	_ = json.NewDecoder(rRes.Body).Decode(&resAcassio)
	rRes.Body.Close()

	jan := resAcassio.Abas[0]
	if len(jan.Linhas) != 1 || jan.Linhas[0].Socio != "ACASSIO" || jan.Total.ValorTotal != 3000 {
		t.Fatalf("isolamento de sócio falhou! linhas=%+v total=%+v", jan.Linhas, jan.Total)
	}

	// Sócio não pode listar usuários nem encerrar servidor
	rUsersForb, _ := acassioCli.Get(ts.URL + "/api/users")
	if rUsersForb.StatusCode != http.StatusForbidden {
		t.Fatalf("sócio deveria receber 403 em /api/users, veio %d", rUsersForb.StatusCode)
	}
	rUsersForb.Body.Close()

	rShutForb, _ := adminCli.Post(ts.URL+"/api/shutdown", "application/json", nil)
	if rShutForb.StatusCode != http.StatusForbidden {
		t.Fatalf("shutdown deveria ser 403 no modo servidor, veio %d", rShutForb.StatusCode)
	}
	rShutForb.Body.Close()

	// 6) Token de acesso direto (/acesso) + download do atalho .url + revogação
	rListUsers, _ := adminCli.Get(ts.URL + "/api/users")
	var allUsers []auth.User
	_ = json.NewDecoder(rListUsers.Body).Decode(&allUsers)
	rListUsers.Body.Close()

	var jociID string
	for _, u := range allUsers {
		if u.Username == "joci" {
			jociID = u.ID
			break
		}
	}
	if jociID == "" {
		t.Fatalf("usuário joci não encontrado na listagem")
	}

	rTok, err := adminCli.Post(ts.URL+"/api/users/"+jociID+"/token", "application/json", nil)
	if err != nil || rTok.StatusCode != http.StatusOK {
		t.Fatalf("gerar token falhou: err=%v status=%v", err, rTok)
	}
	var jociWithTok auth.User
	_ = json.NewDecoder(rTok.Body).Decode(&jociWithTok)
	rTok.Body.Close()
	if jociWithTok.AccessToken == "" {
		t.Fatalf("AccessToken veio vazio")
	}

	// Download do arquivo .url
	rSc, err := adminCli.Get(ts.URL + "/api/users/" + jociID + "/atalho?empresaId=" + empJoci.ID + "&ano=2026&tipo=S")
	if err != nil || rSc.StatusCode != http.StatusOK {
		t.Fatalf("download atalho falhou: err=%v status=%v", err, rSc)
	}
	var scBuf bytes.Buffer
	_, _ = scBuf.ReadFrom(rSc.Body)
	rSc.Body.Close()
	if !bytes.Contains(scBuf.Bytes(), []byte("[InternetShortcut]")) || !bytes.Contains(scBuf.Bytes(), []byte(jociWithTok.AccessToken)) {
		t.Fatalf("conteúdo do atalho .url inválido: %s", scBuf.String())
	}

	// Cliente anônimo acessa /acesso?token=... -> deve redirecionar sem o token e já logar via cookie
	jarAuto, _ := cookiejar.New(nil)
	noRedirectCli := &http.Client{
		Jar: jarAuto,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	rAuto, err := noRedirectCli.Get(ts.URL + "/acesso?token=" + jociWithTok.AccessToken + "&empresaId=" + empJoci.ID + "&ano=2026&tipo=S")
	if err != nil || rAuto.StatusCode != http.StatusFound {
		t.Fatalf("esperava 302 em /acesso, veio err=%v status=%v", err, rAuto)
	}
	loc := rAuto.Header.Get("Location")
	rAuto.Body.Close()
	if bytes.Contains([]byte(loc), []byte("token=")) || !bytes.Contains([]byte(loc), []byte("empresaId="+empJoci.ID)) {
		t.Fatalf("Location de /acesso inesperada (não deve conter token): %s", loc)
	}

	// Cookie recebido no /acesso já deve permitir consultar /api/empresas
	rCheck, _ := noRedirectCli.Get(ts.URL + "/api/empresas")
	if rCheck.StatusCode != http.StatusOK {
		t.Fatalf("cookie emitido por /acesso não autenticou /api/empresas: %d", rCheck.StatusCode)
	}
	rCheck.Body.Close()

	// Revoga o token e verifica que o link antigo para de funcionar
	reqRevoke, _ := http.NewRequest(http.MethodDelete, ts.URL+"/api/users/"+jociID+"/token", nil)
	rRev, err := adminCli.Do(reqRevoke)
	if err != nil || rRev.StatusCode != http.StatusOK {
		t.Fatalf("revogar token falhou: err=%v status=%v", err, rRev)
	}
	rRev.Body.Close()

	rAfterRev, _ := noRedirectCli.Get(ts.URL + "/acesso?token=" + jociWithTok.AccessToken)
	locRev := rAfterRev.Header.Get("Location")
	rAfterRev.Body.Close()
	if !bytes.Contains([]byte(locRev), []byte("authError=")) {
		t.Fatalf("token revogado deveria redirecionar com authError, veio: %s", locRev)
	}
}
