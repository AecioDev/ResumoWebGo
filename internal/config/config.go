package config

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	ModoSocios = "S"
	ModoAcerto = "A"
	ModoAmbos  = "SA"

	DefaultPort = 8745
)

type CelulasSocios struct {
	ValorTotal  string `json:"valorTotal"`
	Recebimento string `json:"recebimento"`
	MercPagas   string `json:"mercPagas"`
	ValSocio    string `json:"valSocio"`
	ValFirma    string `json:"valFirma"`
}

func DefaultCelulasSocios() CelulasSocios {
	return CelulasSocios{
		ValorTotal:  "B12",
		Recebimento: "D11",
		MercPagas:   "F6",
		ValSocio:    "E20",
		ValFirma:    "E19",
	}
}

func CleanCellRef(ref, fallback string) string {
	cleaned := strings.ToUpper(strings.TrimSpace(strings.ReplaceAll(ref, "$", "")))
	if cleaned == "" {
		return fallback
	}
	return cleaned
}

func (c CelulasSocios) Normalized() CelulasSocios {
	def := DefaultCelulasSocios()
	return CelulasSocios{
		ValorTotal:  CleanCellRef(c.ValorTotal, def.ValorTotal),
		Recebimento: CleanCellRef(c.Recebimento, def.Recebimento),
		MercPagas:   CleanCellRef(c.MercPagas, def.MercPagas),
		ValSocio:    CleanCellRef(c.ValSocio, def.ValSocio),
		ValFirma:    CleanCellRef(c.ValFirma, def.ValFirma),
	}
}

type PastaAnoRef struct {
	SociosID   string `json:"sociosId,omitempty"`
	SociosNome string `json:"sociosNome,omitempty"`
	AcertoID   string `json:"acertoId,omitempty"`
	AcertoNome string `json:"acertoNome,omitempty"`
}

type EmpresaConfig struct {
	ID              string                 `json:"id"`
	Nome            string                 `json:"nome"`
	Modo            string                 `json:"modo"` // "S", "A" ou "SA"
	PastaSociosID   string                 `json:"pastaSociosId,omitempty"`
	PastaSociosNome string                 `json:"pastaSociosNome,omitempty"`
	PastaAcertoID   string                 `json:"pastaAcertoId,omitempty"`
	PastaAcertoNome string                 `json:"pastaAcertoNome,omitempty"`
	PastasPorAno    map[string]PastaAnoRef `json:"pastasPorAno,omitempty"`
	Celulas         CelulasSocios          `json:"celulas"`
}

func (e EmpresaConfig) UsaSocios() bool {
	return e.Modo == ModoSocios || e.Modo == ModoAmbos
}

func (e EmpresaConfig) UsaAcerto() bool {
	return e.Modo == ModoAcerto || e.Modo == ModoAmbos
}

// GetPastaSocios retorna o ID da pasta de Sócios para o ano informado (com fallback para o campo legado).
func (e EmpresaConfig) GetPastaSocios(ano int) (string, string) {
	if e.PastasPorAno != nil {
		if ref, ok := e.PastasPorAno[fmt.Sprintf("%d", ano)]; ok && strings.TrimSpace(ref.SociosID) != "" {
			return ref.SociosID, ref.SociosNome
		}
	}
	return e.PastaSociosID, e.PastaSociosNome
}

// GetPastaAcerto retorna o ID da pasta de Acerto para o ano informado (com fallback para o campo legado).
func (e EmpresaConfig) GetPastaAcerto(ano int) (string, string) {
	if e.PastasPorAno != nil {
		if ref, ok := e.PastasPorAno[fmt.Sprintf("%d", ano)]; ok && strings.TrimSpace(ref.AcertoID) != "" {
			return ref.AcertoID, ref.AcertoNome
		}
	}
	return e.PastaAcertoID, e.PastaAcertoNome
}

type AppConfig struct {
	Porta    int             `json:"porta"`
	Empresas []EmpresaConfig `json:"empresas"`
}

type Store struct {
	mu      sync.RWMutex
	baseDir string
	cfg     AppConfig
}

func NewStore(customBaseDir string) (*Store, error) {
	baseDir := customBaseDir
	if baseDir == "" {
		userCfgDir, err := os.UserConfigDir()
		if err != nil {
			return nil, fmt.Errorf("não foi possível localizar AppData: %w", err)
		}
		baseDir = filepath.Join(userCfgDir, "ResumoPlanilhas")
	}

	if err := os.MkdirAll(baseDir, 0o755); err != nil {
		return nil, fmt.Errorf("erro ao criar diretório de configuração: %w", err)
	}
	if err := os.MkdirAll(filepath.Join(baseDir, "cache"), 0o755); err != nil {
		return nil, fmt.Errorf("erro ao criar diretório de cache: %w", err)
	}

	s := &Store{
		baseDir: baseDir,
		cfg: AppConfig{
			Porta:    DefaultPort,
			Empresas: []EmpresaConfig{},
		},
	}

	if err := s.Load(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Store) BaseDir() string {
	return s.baseDir
}

func (s *Store) ConfigPath() string {
	return filepath.Join(s.baseDir, "config.json")
}

func (s *Store) CredentialsPath() string {
	return filepath.Join(s.baseDir, "credentials.json")
}

func (s *Store) TokenPath() string {
	return filepath.Join(s.baseDir, "token.json")
}

func (s *Store) CacheDir() string {
	return filepath.Join(s.baseDir, "cache")
}

func (s *Store) HasCredentials() bool {
	info, err := os.Stat(s.CredentialsPath())
	return err == nil && !info.IsDir() && info.Size() > 0
}

func (s *Store) SaveCredentialsJSON(raw []byte) error {
	var parsed struct {
		Type        string `json:"type"`
		ClientEmail string `json:"client_email"`
		PrivateKey  string `json:"private_key"`
		Installed   *struct {
			ClientID     string `json:"client_id"`
			ClientSecret string `json:"client_secret"`
		} `json:"installed"`
		Web *struct {
			ClientID     string `json:"client_id"`
			ClientSecret string `json:"client_secret"`
		} `json:"web"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return fmt.Errorf("JSON inválido: %w", err)
	}
	isServiceAccount := parsed.Type == "service_account" && parsed.ClientEmail != "" && parsed.PrivateKey != ""
	isOAuth := (parsed.Installed != nil && parsed.Installed.ClientID != "" && parsed.Installed.ClientSecret != "") ||
		(parsed.Web != nil && parsed.Web.ClientID != "" && parsed.Web.ClientSecret != "")
	if !isServiceAccount && !isOAuth {
		return errors.New("arquivo credentials.json inválido: envie uma chave de Conta de Serviço (service_account) ou OAuth Desktop")
	}
	return os.WriteFile(s.CredentialsPath(), raw, 0o600)
}

func (s *Store) Load() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := os.ReadFile(s.ConfigPath())
	if errors.Is(err, os.ErrNotExist) {
		return s.saveLocked()
	}
	if err != nil {
		return fmt.Errorf("erro ao ler config.json: %w", err)
	}

	var cfg AppConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return fmt.Errorf("erro ao interpretar config.json: %w", err)
	}
	if cfg.Porta <= 0 {
		cfg.Porta = DefaultPort
	}
	if cfg.Empresas == nil {
		cfg.Empresas = []EmpresaConfig{}
	}
	for i := range cfg.Empresas {
		cfg.Empresas[i] = normalizeEmpresa(cfg.Empresas[i])
	}
	s.cfg = cfg
	return nil
}

func (s *Store) saveLocked() error {
	data, err := json.MarshalIndent(s.cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.ConfigPath(), data, 0o644)
}

func (s *Store) GetConfig() AppConfig {
	s.mu.RLock()
	defer s.mu.RUnlock()

	out := AppConfig{
		Porta:    s.cfg.Porta,
		Empresas: make([]EmpresaConfig, len(s.cfg.Empresas)),
	}
	copy(out.Empresas, s.cfg.Empresas)
	return out
}

func (s *Store) GetEmpresa(id string) (EmpresaConfig, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	for _, emp := range s.cfg.Empresas {
		if emp.ID == id {
			return emp, true
		}
	}
	return EmpresaConfig{}, false
}

func (s *Store) UpsertEmpresa(emp EmpresaConfig) (EmpresaConfig, error) {
	emp.Nome = strings.TrimSpace(strings.ToUpper(emp.Nome))
	if emp.Nome == "" {
		return EmpresaConfig{}, errors.New("informe o nome da empresa")
	}

	emp = normalizeEmpresa(emp)

	s.mu.Lock()
	defer s.mu.Unlock()

	if emp.ID == "" {
		emp.ID = newID()
		s.cfg.Empresas = append(s.cfg.Empresas, emp)
	} else {
		updated := false
		for i := range s.cfg.Empresas {
			if s.cfg.Empresas[i].ID == emp.ID {
				s.cfg.Empresas[i] = emp
				updated = true
				break
			}
		}
		if !updated {
			s.cfg.Empresas = append(s.cfg.Empresas, emp)
		}
	}

	if err := s.saveLocked(); err != nil {
		return EmpresaConfig{}, err
	}
	return emp, nil
}

func (s *Store) DeleteEmpresa(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	filtered := make([]EmpresaConfig, 0, len(s.cfg.Empresas))
	for _, emp := range s.cfg.Empresas {
		if emp.ID != id {
			filtered = append(filtered, emp)
		}
	}
	s.cfg.Empresas = filtered
	return s.saveLocked()
}

func (s *Store) ClearCache() (int, error) {
	entries, err := os.ReadDir(s.CacheDir())
	if err != nil {
		return 0, err
	}
	removed := 0
	for _, entry := range entries {
		if !entry.IsDir() {
			if err := os.Remove(filepath.Join(s.CacheDir(), entry.Name())); err == nil {
				removed++
			}
		}
	}
	return removed, nil
}

func NormalizeModo(modo string) string {
	switch strings.ToUpper(strings.TrimSpace(modo)) {
	case ModoSocios:
		return ModoSocios
	case ModoAcerto:
		return ModoAcerto
	case ModoAmbos:
		return ModoAmbos
	default:
		return ModoSocios
	}
}

var driveFolderRegex = regexp.MustCompile(`/folders/([a-zA-Z0-9_-]+)`)

// ExtractDriveFolderID aceita tanto o ID puro quanto uma URL completa do Google Drive.
func ExtractDriveFolderID(input string) string {
	raw := strings.TrimSpace(input)
	if raw == "" {
		return ""
	}
	if m := driveFolderRegex.FindStringSubmatch(raw); len(m) == 2 {
		return m[1]
	}
	if u, err := url.Parse(raw); err == nil && u.Scheme != "" {
		if id := u.Query().Get("id"); id != "" {
			return id
		}
	}
	return raw
}

func normalizeEmpresa(emp EmpresaConfig) EmpresaConfig {
	emp.Modo = NormalizeModo(emp.Modo)
	emp.PastaSociosID = ExtractDriveFolderID(emp.PastaSociosID)
	emp.PastaAcertoID = ExtractDriveFolderID(emp.PastaAcertoID)
	emp.Celulas = emp.Celulas.Normalized()

	cleanMap := make(map[string]PastaAnoRef)
	for yearKey, ref := range emp.PastasPorAno {
		y := strings.TrimSpace(yearKey)
		if _, err := strconv.Atoi(y); err != nil || len(y) != 4 {
			continue
		}
		ref.SociosID = ExtractDriveFolderID(ref.SociosID)
		ref.AcertoID = ExtractDriveFolderID(ref.AcertoID)
		ref.SociosNome = strings.TrimSpace(ref.SociosNome)
		ref.AcertoNome = strings.TrimSpace(ref.AcertoNome)
		if ref.SociosID != "" || ref.AcertoID != "" {
			cleanMap[y] = ref
		}
	}

	// Migração transparente: se existiam campos legados sem mapa por ano, registra no ano atual
	if len(cleanMap) == 0 && (emp.PastaSociosID != "" || emp.PastaAcertoID != "") {
		yearStr := strconv.Itoa(time.Now().Year())
		cleanMap[yearStr] = PastaAnoRef{
			SociosID:   emp.PastaSociosID,
			SociosNome: emp.PastaSociosNome,
			AcertoID:   emp.PastaAcertoID,
			AcertoNome: emp.PastaAcertoNome,
		}
	}

	// Mantém campos legados sincronizados com o ano mais recente configurado
	if len(cleanMap) > 0 {
		years := make([]string, 0, len(cleanMap))
		for y := range cleanMap {
			years = append(years, y)
		}
		sort.Strings(years)
		latest := cleanMap[years[len(years)-1]]
		if emp.PastaSociosID == "" {
			emp.PastaSociosID = latest.SociosID
			emp.PastaSociosNome = latest.SociosNome
		}
		if emp.PastaAcertoID == "" {
			emp.PastaAcertoID = latest.AcertoID
			emp.PastaAcertoNome = latest.AcertoNome
		}
	}

	emp.PastasPorAno = cleanMap
	return emp
}

func newID() string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("emp_%d", os.Getpid())
	}
	return hex.EncodeToString(b)
}
