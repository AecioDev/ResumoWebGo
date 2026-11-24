package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"
)

const (
	RoleAdmin  = "admin"
	RoleGestor = "gestor"
	RoleSocio  = "socio"

	SessionCookieName = "rp_session"
	SessionTTL        = 7 * 24 * time.Hour
)

type User struct {
	ID           string   `json:"id"`
	Username     string   `json:"username"`
	Nome         string   `json:"nome"`
	PasswordHash string   `json:"passwordHash,omitempty"`
	AccessToken  string   `json:"accessToken,omitempty"`
	Role         string   `json:"role"` // "admin", "gestor", "socio"
	EmpresaIDs   []string `json:"empresaIds"`
	SocioNome    string   `json:"socioNome,omitempty"` // Obrigatório quando Role == "socio"
	Ativo        bool     `json:"ativo"`
	CreatedBy    string   `json:"createdBy,omitempty"`
}

func (u User) Public() User {
	cp := u
	cp.PasswordHash = ""
	cp.AccessToken = ""
	if cp.EmpresaIDs == nil {
		cp.EmpresaIDs = []string{}
	}
	return cp
}

// ManageView oculta o hash de senha mas mantém o AccessToken para quem gerencia o usuário.
func (u User) ManageView() User {
	cp := u
	cp.PasswordHash = ""
	if cp.EmpresaIDs == nil {
		cp.EmpresaIDs = []string{}
	}
	return cp
}

func (u User) HasEmpresa(empresaID string) bool {
	if u.Role == RoleAdmin {
		return true
	}
	for _, id := range u.EmpresaIDs {
		if id == empresaID {
			return true
		}
	}
	return false
}

type UserInput struct {
	ID         string   `json:"id"`
	Username   string   `json:"username"`
	Nome       string   `json:"nome"`
	Password   string   `json:"password"`
	Role       string   `json:"role"`
	EmpresaIDs []string `json:"empresaIds"`
	SocioNome  string   `json:"socioNome"`
	Ativo      *bool    `json:"ativo"`
}

type Store struct {
	mu        sync.RWMutex
	usersPath string
	secret    []byte
	users     []User
}

func NewStore(baseDir string, sessionSecret string) (*Store, error) {
	if err := os.MkdirAll(baseDir, 0o755); err != nil {
		return nil, err
	}

	secret, err := loadOrCreateSecret(filepath.Join(baseDir, "secret.key"), sessionSecret)
	if err != nil {
		return nil, err
	}

	s := &Store{
		usersPath: filepath.Join(baseDir, "users.json"),
		secret:    secret,
		users:     []User{},
	}
	if err := s.Load(); err != nil {
		return nil, err
	}
	return s, nil
}

func loadOrCreateSecret(path, envSecret string) ([]byte, error) {
	if strings.TrimSpace(envSecret) != "" {
		sum := sha256.Sum256([]byte(envSecret))
		return sum[:], nil
	}
	if data, err := os.ReadFile(path); err == nil && len(data) >= 32 {
		return data, nil
	}
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return nil, err
	}
	_ = os.WriteFile(path, b, 0o600)
	return b, nil
}

func (s *Store) Load() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := os.ReadFile(s.usersPath)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	var users []User
	if err := json.Unmarshal(data, &users); err != nil {
		return err
	}
	s.users = users
	return nil
}

func (s *Store) saveLocked() error {
	data, err := json.MarshalIndent(s.users, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.usersPath, data, 0o600)
}

// EnsureAdmin garante que exista ao menos um usuário admin (usado no modo servidor).
// Retorna a senha gerada caso tenha criado um admin novo sem senha pré-definida.
func (s *Store) EnsureAdmin(username, password string) (created bool, generatedPassword string, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, u := range s.users {
		if u.Role == RoleAdmin && u.Ativo {
			return false, "", nil
		}
	}

	user := strings.ToLower(strings.TrimSpace(username))
	if user == "" {
		user = "admin"
	}
	pass := strings.TrimSpace(password)
	if pass == "" {
		b := make([]byte, 9)
		_, _ = rand.Read(b)
		pass = hex.EncodeToString(b)
		generatedPassword = pass
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(pass), bcrypt.DefaultCost)
	if err != nil {
		return false, "", err
	}

	s.users = append(s.users, User{
		ID:           newID(),
		Username:     user,
		Nome:         "Administrador",
		PasswordHash: string(hash),
		Role:         RoleAdmin,
		EmpresaIDs:   []string{},
		Ativo:        true,
	})
	if err := s.saveLocked(); err != nil {
		return false, "", err
	}
	return true, generatedPassword, nil
}

func (s *Store) Authenticate(username, password string) (User, error) {
	uname := strings.ToLower(strings.TrimSpace(username))
	if uname == "" || password == "" {
		return User{}, errors.New("informe usuário e senha")
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	for _, u := range s.users {
		if strings.ToLower(u.Username) == uname {
			if !u.Ativo {
				return User{}, errors.New("usuário desativado")
			}
			if err := bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(password)); err != nil {
				return User{}, errors.New("usuário ou senha inválidos")
			}
			return u.Public(), nil
		}
	}
	return User{}, errors.New("usuário ou senha inválidos")
}

func (s *Store) AuthenticateByToken(token string) (User, error) {
	tok := strings.TrimSpace(token)
	if len(tok) < 16 {
		return User{}, errors.New("link de acesso inválido")
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	for _, u := range s.users {
		if u.AccessToken == "" {
			continue
		}
		if len(u.AccessToken) == len(tok) && subtle.ConstantTimeCompare([]byte(u.AccessToken), []byte(tok)) == 1 {
			if !u.Ativo {
				return User{}, errors.New("usuário desativado")
			}
			return u.Public(), nil
		}
	}
	return User{}, errors.New("link de acesso inválido ou revogado")
}

func (s *Store) GetManagedByID(actor User, targetID string) (User, error) {
	if actor.Role != RoleAdmin && actor.Role != RoleGestor {
		return User{}, errors.New("sem permissão")
	}
	s.mu.RLock()
	defer s.mu.RUnlock()

	for _, u := range s.users {
		if u.ID == targetID {
			if actor.Role == RoleGestor && !actorCanManageUser(actor, u) {
				return User{}, errors.New("sem permissão para acessar este usuário")
			}
			return u.ManageView(), nil
		}
	}
	return User{}, errors.New("usuário não encontrado")
}

func (s *Store) GenerateAccessToken(actor User, targetID string) (User, error) {
	if actor.Role != RoleAdmin && actor.Role != RoleGestor {
		return User{}, errors.New("sem permissão para gerar link de acesso")
	}

	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		return User{}, err
	}
	token := base64.RawURLEncoding.EncodeToString(b)

	s.mu.Lock()
	defer s.mu.Unlock()

	for i := range s.users {
		if s.users[i].ID == targetID {
			if actor.Role == RoleGestor && !actorCanManageUser(actor, s.users[i]) {
				return User{}, errors.New("você não tem permissão para gerenciar este usuário")
			}
			s.users[i].AccessToken = token
			if err := s.saveLocked(); err != nil {
				return User{}, err
			}
			return s.users[i].ManageView(), nil
		}
	}
	return User{}, errors.New("usuário não encontrado")
}

func (s *Store) RevokeAccessToken(actor User, targetID string) (User, error) {
	if actor.Role != RoleAdmin && actor.Role != RoleGestor {
		return User{}, errors.New("sem permissão para revogar link de acesso")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	for i := range s.users {
		if s.users[i].ID == targetID {
			if actor.Role == RoleGestor && !actorCanManageUser(actor, s.users[i]) {
				return User{}, errors.New("você não tem permissão para gerenciar este usuário")
			}
			s.users[i].AccessToken = ""
			if err := s.saveLocked(); err != nil {
				return User{}, err
			}
			return s.users[i].ManageView(), nil
		}
	}
	return User{}, errors.New("usuário não encontrado")
}

func (s *Store) GetByID(id string) (User, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	for _, u := range s.users {
		if u.ID == id {
			return u.Public(), true
		}
	}
	return User{}, false
}

func (s *Store) ListForActor(actor User) []User {
	s.mu.RLock()
	defer s.mu.RUnlock()

	out := make([]User, 0, len(s.users))
	for _, u := range s.users {
		if actor.Role == RoleAdmin {
			out = append(out, u.ManageView())
			continue
		}
		// Gestor vê apenas usuários "socio" vinculados exclusivamente a empresas do próprio gestor
		if actor.Role == RoleGestor && u.Role == RoleSocio && actorCanManageUser(actor, u) {
			out = append(out, u.ManageView())
		}
	}
	return out
}

func (s *Store) UpsertForActor(actor User, in UserInput) (User, error) {
	if actor.Role != RoleAdmin && actor.Role != RoleGestor {
		return User{}, errors.New("sem permissão para gerenciar usuários")
	}

	uname := strings.ToLower(strings.TrimSpace(in.Username))
	if len(uname) < 3 {
		return User{}, errors.New("o login deve ter pelo menos 3 caracteres")
	}
	if strings.ContainsAny(uname, " \t\r\n") {
		return User{}, errors.New("o login não pode conter espaços")
	}

	role := strings.ToLower(strings.TrimSpace(in.Role))
	if actor.Role == RoleGestor {
		// Gestor só pode criar/editar usuários do tipo "socio"
		role = RoleSocio
	}
	if role != RoleAdmin && role != RoleGestor && role != RoleSocio {
		return User{}, errors.New("perfil inválido (use admin, gestor ou socio)")
	}

	empIDs := uniqueStrings(in.EmpresaIDs)
	if actor.Role == RoleGestor {
		if len(empIDs) == 0 {
			return User{}, errors.New("selecione ao menos uma empresa para o sócio")
		}
		for _, eid := range empIDs {
			if !actor.HasEmpresa(eid) {
				return User{}, errors.New("você só pode vincular empresas da sua própria conta")
			}
		}
	}

	socioNome := strings.ToUpper(strings.TrimSpace(in.SocioNome))
	if role == RoleSocio && socioNome == "" {
		return User{}, errors.New("informe o nome exato do sócio (como aparece na planilha)")
	}
	if role != RoleSocio {
		socioNome = ""
	}

	nome := strings.TrimSpace(in.Nome)
	if nome == "" {
		if socioNome != "" {
			nome = socioNome
		} else {
			nome = uname
		}
	}

	ativo := true
	if in.Ativo != nil {
		ativo = *in.Ativo
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// Verifica duplicidade de username
	for _, existing := range s.users {
		if strings.ToLower(existing.Username) == uname && existing.ID != in.ID {
			return User{}, fmt.Errorf("já existe um usuário com o login '%s'", uname)
		}
	}

	if in.ID == "" {
		if len(strings.TrimSpace(in.Password)) < 4 {
			return User{}, errors.New("informe uma senha com pelo menos 4 caracteres")
		}
		hash, err := bcrypt.GenerateFromPassword([]byte(in.Password), bcrypt.DefaultCost)
		if err != nil {
			return User{}, err
		}
		u := User{
			ID:           newID(),
			Username:     uname,
			Nome:         nome,
			PasswordHash: string(hash),
			Role:         role,
			EmpresaIDs:   empIDs,
			SocioNome:    socioNome,
			Ativo:        ativo,
			CreatedBy:    actor.ID,
		}
		s.users = append(s.users, u)
		if err := s.saveLocked(); err != nil {
			return User{}, err
		}
		return u.ManageView(), nil
	}

	// Atualização
	for i := range s.users {
		if s.users[i].ID == in.ID {
			if actor.Role == RoleGestor && !actorCanManageUser(actor, s.users[i]) {
				return User{}, errors.New("você não tem permissão para editar este usuário")
			}
			// Impede desativar/rebaixar o último admin ativo
			if s.users[i].Role == RoleAdmin && (role != RoleAdmin || !ativo) {
				otherAdmins := 0
				for _, u := range s.users {
					if u.ID != s.users[i].ID && u.Role == RoleAdmin && u.Ativo {
						otherAdmins++
					}
				}
				if otherAdmins == 0 {
					return User{}, errors.New("não é possível desativar ou remover o último administrador")
				}
			}

			s.users[i].Username = uname
			s.users[i].Nome = nome
			s.users[i].Role = role
			s.users[i].EmpresaIDs = empIDs
			s.users[i].SocioNome = socioNome
			s.users[i].Ativo = ativo

			if strings.TrimSpace(in.Password) != "" {
				if len(strings.TrimSpace(in.Password)) < 4 {
					return User{}, errors.New("a nova senha deve ter pelo menos 4 caracteres")
				}
				hash, err := bcrypt.GenerateFromPassword([]byte(in.Password), bcrypt.DefaultCost)
				if err != nil {
					return User{}, err
				}
				s.users[i].PasswordHash = string(hash)
			}

			if err := s.saveLocked(); err != nil {
				return User{}, err
			}
			return s.users[i].ManageView(), nil
		}
	}
	return User{}, errors.New("usuário não encontrado")
}

func (s *Store) DeleteForActor(actor User, targetID string) error {
	if actor.Role != RoleAdmin && actor.Role != RoleGestor {
		return errors.New("sem permissão para excluir usuários")
	}
	if actor.ID == targetID {
		return errors.New("você não pode excluir o próprio usuário logado")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	filtered := make([]User, 0, len(s.users))
	found := false
	for _, u := range s.users {
		if u.ID == targetID {
			if actor.Role == RoleGestor && !actorCanManageUser(actor, u) {
				return errors.New("sem permissão para excluir este usuário")
			}
			if u.Role == RoleAdmin {
				otherAdmins := 0
				for _, x := range s.users {
					if x.ID != u.ID && x.Role == RoleAdmin && x.Ativo {
						otherAdmins++
					}
				}
				if otherAdmins == 0 {
					return errors.New("não é possível excluir o último administrador")
				}
			}
			found = true
			continue
		}
		filtered = append(filtered, u)
	}
	if !found {
		return errors.New("usuário não encontrado")
	}
	s.users = filtered
	return s.saveLocked()
}

// ===== Sessão HMAC em Cookie HttpOnly =====

type sessionPayload struct {
	UID string `json:"uid"`
	Exp int64  `json:"exp"`
}

func (s *Store) SetSessionCookie(w http.ResponseWriter, r *http.Request, user User) {
	payload := sessionPayload{
		UID: user.ID,
		Exp: time.Now().Add(SessionTTL).Unix(),
	}
	raw, _ := json.Marshal(payload)
	b64 := base64.RawURLEncoding.EncodeToString(raw)
	sig := s.sign(b64)

	secure := r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
	http.SetCookie(w, &http.Cookie{
		Name:     SessionCookieName,
		Value:    b64 + "." + sig,
		Path:     "/",
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(SessionTTL.Seconds()),
	})
}

func (s *Store) ClearSessionCookie(w http.ResponseWriter, r *http.Request) {
	secure := r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
	http.SetCookie(w, &http.Cookie{
		Name:     SessionCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	})
}

func (s *Store) UserFromRequest(r *http.Request) (User, bool) {
	cookie, err := r.Cookie(SessionCookieName)
	if err != nil || cookie.Value == "" {
		return User{}, false
	}
	parts := strings.Split(cookie.Value, ".")
	if len(parts) != 2 {
		return User{}, false
	}
	if !hmac.Equal([]byte(s.sign(parts[0])), []byte(parts[1])) {
		return User{}, false
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return User{}, false
	}
	var payload sessionPayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		return User{}, false
	}
	if time.Now().Unix() > payload.Exp {
		return User{}, false
	}

	u, ok := s.GetByID(payload.UID)
	if !ok || !u.Ativo {
		return User{}, false
	}
	return u, true
}

func (s *Store) sign(msg string) string {
	mac := hmac.New(sha256.New, s.secret)
	mac.Write([]byte(msg))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func actorCanManageUser(actor, target User) bool {
	if actor.Role == RoleAdmin {
		return true
	}
	if actor.Role != RoleGestor || target.Role != RoleSocio {
		return false
	}
	if len(target.EmpresaIDs) == 0 {
		return target.CreatedBy == actor.ID
	}
	for _, eid := range target.EmpresaIDs {
		if !actor.HasEmpresa(eid) {
			return false
		}
	}
	return true
}

func uniqueStrings(in []string) []string {
	seen := make(map[string]bool, len(in))
	out := make([]string, 0, len(in))
	for _, v := range in {
		v = strings.TrimSpace(v)
		if v != "" && !seen[v] {
			seen[v] = true
			out = append(out, v)
		}
	}
	return out
}

func newID() string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("usr_%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
}
