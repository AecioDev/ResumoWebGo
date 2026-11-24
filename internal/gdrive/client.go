package gdrive

import (
	"context"
	"crypto/rand"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/drive/v3"
	"google.golang.org/api/option"

	"resumo-planilhas/internal/config"
)

const (
	MimeFolder      = "application/vnd.google-apps.folder"
	MimeGoogleSheet = "application/vnd.google-apps.spreadsheet"
	MimeXLSX        = "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"
)

type FolderItem struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	ParentID string `json:"parentId,omitempty"`
}

type DriveFile struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	MimeType     string `json:"mimeType"`
	ModifiedTime string `json:"modifiedTime"`
	Size         int64  `json:"size"`
}

type AuthStatus struct {
	HasCredentials bool   `json:"hasCredentials"`
	AuthMode       string `json:"authMode,omitempty"` // "service_account" ou "oauth"
	Authenticated  bool   `json:"authenticated"`
	UserEmail      string `json:"userEmail,omitempty"`
	UserName       string `json:"userName,omitempty"`
	Error          string `json:"error,omitempty"`
}

func (c *Client) detectCredentialsMode() (string, []byte, error) {
	raw, err := os.ReadFile(c.store.CredentialsPath())
	if err != nil {
		return "", nil, err
	}
	var probe struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(raw, &probe); err != nil {
		return "", nil, err
	}
	if probe.Type == "service_account" {
		return "service_account", raw, nil
	}
	return "oauth", raw, nil
}

type Client struct {
	store *config.Store

	mu              sync.Mutex
	pendingState    string
	cachedStatus    *AuthStatus
	cachedStatusExp time.Time
}

func (c *Client) InvalidateAuthCache() {
	c.mu.Lock()
	c.cachedStatus = nil
	c.cachedStatusExp = time.Time{}
	c.mu.Unlock()
}

func NewClient(store *config.Store) *Client {
	return &Client{store: store}
}

func (c *Client) loadOAuthConfig(redirectURI string) (*oauth2.Config, error) {
	raw, err := os.ReadFile(c.store.CredentialsPath())
	if err != nil {
		return nil, fmt.Errorf("arquivo credentials.json não encontrado em %s: %w", c.store.CredentialsPath(), err)
	}
	oauthCfg, err := google.ConfigFromJSON(raw, drive.DriveReadonlyScope)
	if err != nil {
		return nil, fmt.Errorf("erro ao ler credentials.json: %w", err)
	}
	if redirectURI != "" {
		oauthCfg.RedirectURL = redirectURI
	}
	return oauthCfg, nil
}

func (c *Client) StartAuthURL(redirectURI string) (string, error) {
	oauthCfg, err := c.loadOAuthConfig(redirectURI)
	if err != nil {
		return "", err
	}

	stateBytes := make([]byte, 16)
	if _, err := rand.Read(stateBytes); err != nil {
		return "", err
	}
	state := hex.EncodeToString(stateBytes)

	c.mu.Lock()
	c.pendingState = state
	c.mu.Unlock()

	authURL := oauthCfg.AuthCodeURL(
		state,
		oauth2.AccessTypeOffline,
		oauth2.SetAuthURLParam("prompt", "consent"),
	)
	return authURL, nil
}

func (c *Client) ExchangeCode(ctx context.Context, redirectURI, state, code string) error {
	c.mu.Lock()
	expected := c.pendingState
	c.pendingState = ""
	c.mu.Unlock()

	if expected == "" || state != expected {
		return errors.New("estado OAuth inválido ou expirado; tente conectar novamente")
	}
	if strings.TrimSpace(code) == "" {
		return errors.New("código de autorização vazio")
	}

	oauthCfg, err := c.loadOAuthConfig(redirectURI)
	if err != nil {
		return err
	}

	tok, err := oauthCfg.Exchange(ctx, code)
	if err != nil {
		return fmt.Errorf("falha ao obter token do Google: %w", err)
	}
	return c.saveToken(tok)
}

func (c *Client) Disconnect() error {
	c.InvalidateAuthCache()
	err := os.Remove(c.store.TokenPath())
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

func (c *Client) loadToken() (*oauth2.Token, error) {
	raw, err := os.ReadFile(c.store.TokenPath())
	if err != nil {
		return nil, err
	}
	var tok oauth2.Token
	if err := json.Unmarshal(raw, &tok); err != nil {
		return nil, err
	}
	return &tok, nil
}

func (c *Client) saveToken(tok *oauth2.Token) error {
	raw, err := json.MarshalIndent(tok, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(c.store.TokenPath(), raw, 0o600)
}

type persistentTokenSource struct {
	mu          sync.Mutex
	src         oauth2.TokenSource
	lastAccess  string
	lastRefresh string
	saveFn      func(*oauth2.Token) error
}

func (p *persistentTokenSource) Token() (*oauth2.Token, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	tok, err := p.src.Token()
	if err != nil {
		return nil, err
	}
	if tok.AccessToken != p.lastAccess || tok.RefreshToken != p.lastRefresh {
		p.lastAccess = tok.AccessToken
		p.lastRefresh = tok.RefreshToken
		_ = p.saveFn(tok)
	}
	return tok, nil
}

func (c *Client) driveService(ctx context.Context) (*drive.Service, error) {
	mode, raw, err := c.detectCredentialsMode()
	if err != nil {
		return nil, fmt.Errorf("arquivo credentials.json não encontrado: %w", err)
	}

	if mode == "service_account" {
		jwtCfg, err := google.JWTConfigFromJSON(raw, drive.DriveReadonlyScope)
		if err != nil {
			return nil, fmt.Errorf("erro ao ler chave da Conta de Serviço: %w", err)
		}
		return drive.NewService(ctx, option.WithTokenSource(jwtCfg.TokenSource(ctx)))
	}

	oauthCfg, err := c.loadOAuthConfig("")
	if err != nil {
		return nil, err
	}
	tok, err := c.loadToken()
	if err != nil {
		return nil, fmt.Errorf("não autenticado no Google Drive: %w", err)
	}

	baseSource := oauthCfg.TokenSource(ctx, tok)
	ts := &persistentTokenSource{
		src:         baseSource,
		lastAccess:  tok.AccessToken,
		lastRefresh: tok.RefreshToken,
		saveFn:      c.saveToken,
	}

	return drive.NewService(ctx, option.WithTokenSource(ts))
}

func (c *Client) GetAuthStatus(ctx context.Context) AuthStatus {
	c.mu.Lock()
	if c.cachedStatus != nil && time.Now().Before(c.cachedStatusExp) {
		cp := *c.cachedStatus
		c.mu.Unlock()
		return cp
	}
	c.mu.Unlock()

	st := AuthStatus{
		HasCredentials: c.store.HasCredentials(),
	}
	if !st.HasCredentials {
		return st
	}

	mode, _, err := c.detectCredentialsMode()
	if err != nil {
		st.Error = "Erro ao ler credentials.json"
		return st
	}
	st.AuthMode = mode

	if mode == "oauth" {
		if _, err := c.loadToken(); err != nil {
			return st
		}
	}

	srv, err := c.driveService(ctx)
	if err != nil {
		st.Error = err.Error()
		return st
	}
	about, err := srv.About.Get().Fields("user(displayName,emailAddress)").Context(ctx).Do()
	if err != nil {
		if mode == "service_account" {
			st.Error = "Falha na Conta de Serviço. Verifique se a Google Drive API está ativada no projeto."
		} else {
			st.Error = "Sessão expirada ou revogada. Reconecte sua conta Google."
		}
		return st
	}

	st.Authenticated = true
	if about.User != nil {
		st.UserEmail = about.User.EmailAddress
		st.UserName = about.User.DisplayName
	}

	c.mu.Lock()
	cp := st
	c.cachedStatus = &cp
	c.cachedStatusExp = time.Now().Add(5 * time.Minute)
	c.mu.Unlock()

	return st
}

func (c *Client) ListFolders(ctx context.Context, parentID string, sharedWithMe bool) ([]FolderItem, error) {
	srv, err := c.driveService(ctx)
	if err != nil {
		return nil, err
	}

	var q string
	mode, _, _ := c.detectCredentialsMode()
	switch {
	case parentID != "":
		q = fmt.Sprintf("'%s' in parents and mimeType = '%s' and trashed = false", escapeQuery(parentID), MimeFolder)
	case sharedWithMe || mode == "service_account":
		q = fmt.Sprintf("mimeType = '%s' and sharedWithMe = true and trashed = false", MimeFolder)
	default:
		q = fmt.Sprintf("'root' in parents and mimeType = '%s' and trashed = false", MimeFolder)
	}

	var folders []FolderItem
	pageToken := ""
	for {
		call := srv.Files.List().
			Q(q).
			Fields("nextPageToken, files(id, name, parents)").
			OrderBy("name").
			PageSize(200).
			SupportsAllDrives(true).
			IncludeItemsFromAllDrives(true).
			Context(ctx)
		if pageToken != "" {
			call = call.PageToken(pageToken)
		}
		res, err := call.Do()
		if err != nil {
			return nil, fmt.Errorf("erro ao listar pastas do Google Drive: %w", err)
		}
		for _, f := range res.Files {
			item := FolderItem{ID: f.Id, Name: f.Name}
			if len(f.Parents) > 0 {
				item.ParentID = f.Parents[0]
			}
			folders = append(folders, item)
		}
		if res.NextPageToken == "" {
			break
		}
		pageToken = res.NextPageToken
	}

	sort.Slice(folders, func(i, j int) bool {
		return strings.ToLower(folders[i].Name) < strings.ToLower(folders[j].Name)
	})
	return folders, nil
}

func (c *Client) GetFolderInfo(ctx context.Context, folderID string) (*FolderItem, error) {
	srv, err := c.driveService(ctx)
	if err != nil {
		return nil, err
	}
	f, err := srv.Files.Get(folderID).
		Fields("id, name, parents").
		SupportsAllDrives(true).
		Context(ctx).
		Do()
	if err != nil {
		return nil, err
	}
	item := &FolderItem{ID: f.Id, Name: f.Name}
	if len(f.Parents) > 0 {
		item.ParentID = f.Parents[0]
	}
	return item, nil
}

func (c *Client) ListSpreadsheets(ctx context.Context, folderID string) ([]DriveFile, error) {
	srv, err := c.driveService(ctx)
	if err != nil {
		return nil, err
	}

	q := fmt.Sprintf(
		"'%s' in parents and trashed = false and (mimeType = '%s' or mimeType = '%s')",
		escapeQuery(folderID),
		MimeXLSX,
		MimeGoogleSheet,
	)

	var files []DriveFile
	pageToken := ""
	for {
		call := srv.Files.List().
			Q(q).
			Fields("nextPageToken, files(id, name, mimeType, modifiedTime, size)").
			OrderBy("name").
			PageSize(500).
			SupportsAllDrives(true).
			IncludeItemsFromAllDrives(true).
			Context(ctx)
		if pageToken != "" {
			call = call.PageToken(pageToken)
		}
		res, err := call.Do()
		if err != nil {
			return nil, fmt.Errorf("erro ao listar planilhas no Google Drive: %w", err)
		}
		for _, f := range res.Files {
			upperName := strings.ToUpper(strings.TrimSpace(f.Name))
			if strings.HasPrefix(upperName, "~") || strings.HasPrefix(upperName, "#_RESUMO") {
				continue
			}
			files = append(files, DriveFile{
				ID:           f.Id,
				Name:         f.Name,
				MimeType:     f.MimeType,
				ModifiedTime: f.ModifiedTime,
				Size:         f.Size,
			})
		}
		if res.NextPageToken == "" {
			break
		}
		pageToken = res.NextPageToken
	}

	sort.Slice(files, func(i, j int) bool {
		return strings.ToLower(files[i].Name) < strings.ToLower(files[j].Name)
	})
	return files, nil
}

func (c *Client) DownloadSpreadsheet(ctx context.Context, file DriveFile, forceRefresh bool) ([]byte, bool, error) {
	cacheFile := c.cacheFilePath(file.ID, file.ModifiedTime)
	if !forceRefresh {
		if data, err := os.ReadFile(cacheFile); err == nil && len(data) > 0 {
			return data, true, nil
		}
	}

	srv, err := c.driveService(ctx)
	if err != nil {
		return nil, false, err
	}

	var respBody io.ReadCloser
	if file.MimeType == MimeGoogleSheet {
		resp, err := srv.Files.Export(file.ID, MimeXLSX).Context(ctx).Download()
		if err != nil {
			return nil, false, fmt.Errorf("erro ao exportar Google Sheet %s: %w", file.Name, err)
		}
		respBody = resp.Body
	} else {
		resp, err := srv.Files.Get(file.ID).SupportsAllDrives(true).Context(ctx).Download()
		if err != nil {
			return nil, false, fmt.Errorf("erro ao baixar planilha %s: %w", file.Name, err)
		}
		respBody = resp.Body
	}
	defer respBody.Close()

	data, err := io.ReadAll(respBody)
	if err != nil {
		return nil, false, fmt.Errorf("erro ao ler conteúdo de %s: %w", file.Name, err)
	}

	c.cleanOldCacheForFile(file.ID, cacheFile)
	_ = os.WriteFile(cacheFile, data, 0o644)
	return data, false, nil
}

func (c *Client) cacheFilePath(fileID, modifiedTime string) string {
	h := sha1.Sum([]byte(modifiedTime))
	modHash := hex.EncodeToString(h[:])[:12]
	safeID := strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			return r
		}
		return '_'
	}, fileID)
	return filepath.Join(c.store.CacheDir(), fmt.Sprintf("%s_%s.xlsx", safeID, modHash))
}

func (c *Client) cleanOldCacheForFile(fileID, keepPath string) {
	prefix := fileID + "_"
	entries, err := os.ReadDir(c.store.CacheDir())
	if err != nil {
		return
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), prefix) {
			full := filepath.Join(c.store.CacheDir(), entry.Name())
			if full != keepPath {
				_ = os.Remove(full)
			}
		}
	}
}

func escapeQuery(s string) string {
	return strings.ReplaceAll(s, "'", "\\'")
}
