package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	"resumo-planilhas/internal/auth"
	"resumo-planilhas/internal/config"
	"resumo-planilhas/internal/gdrive"
	"resumo-planilhas/internal/resumo"
	"resumo-planilhas/internal/server"
)

func main() {
	portFlag := flag.Int("port", 0, "Porta HTTP (padrão: 8745 local ou 8080 no modo servidor)")
	noBrowser := flag.Bool("no-browser", false, "Não abrir o navegador automaticamente ao iniciar")
	dataDirFlag := flag.String("data-dir", "", "Diretório customizado de dados/configuração")
	serverModeFlag := flag.Bool("server", false, "Executa em modo servidor web multi-usuário (Docker/Nuvem)")
	flag.Parse()

	serverMode := *serverModeFlag || parseBoolEnv("SERVER_MODE")

	dataDir := strings.TrimSpace(*dataDirFlag)
	if dataDir == "" {
		dataDir = strings.TrimSpace(os.Getenv("DATA_DIR"))
	}

	store, err := config.NewStore(dataDir)
	if err != nil {
		log.Fatalf("Erro ao inicializar configuração: %v", err)
	}

	// Se GOOGLE_CREDENTIALS_FILE foi informado (ex.: montado no Docker), importa para o dataDir se ainda não existir
	if credFile := strings.TrimSpace(os.Getenv("GOOGLE_CREDENTIALS_FILE")); credFile != "" && !store.HasCredentials() {
		if raw, err := os.ReadFile(credFile); err == nil {
			if err := store.SaveCredentialsJSON(raw); err != nil {
				log.Printf("Aviso: GOOGLE_CREDENTIALS_FILE inválido (%s): %v", credFile, err)
			} else {
				log.Printf("Credenciais carregadas de %s", credFile)
			}
		} else {
			log.Printf("Aviso: não foi possível ler GOOGLE_CREDENTIALS_FILE (%s): %v", credFile, err)
		}
	}

	var authStore *auth.Store
	if serverMode {
		authStore, err = auth.NewStore(store.BaseDir(), os.Getenv("SESSION_SECRET"))
		if err != nil {
			log.Fatalf("Erro ao inicializar autenticação: %v", err)
		}
		adminUser := strings.TrimSpace(os.Getenv("ADMIN_USER"))
		adminPass := strings.TrimSpace(os.Getenv("ADMIN_PASSWORD"))
		created, genPass, err := authStore.EnsureAdmin(adminUser, adminPass)
		if err != nil {
			log.Fatalf("Erro ao criar usuário administrador inicial: %v", err)
		}
		if created {
			if adminUser == "" {
				adminUser = "admin"
			}
			if genPass != "" {
				log.Printf("================================================================")
				log.Printf("Usuário administrador criado! Login: %s | Senha gerada: %s", adminUser, genPass)
				log.Printf("Altere esta senha após o primeiro login ou defina ADMIN_PASSWORD.")
				log.Printf("================================================================")
			} else {
				log.Printf("Usuário administrador inicial '%s' criado com sucesso.", adminUser)
			}
		}
	}

	porta := store.GetConfig().Porta
	if serverMode {
		porta = 8080
	}
	if envPort := strings.TrimSpace(os.Getenv("PORT")); envPort != "" {
		if p, err := strconv.Atoi(envPort); err == nil && p > 0 {
			porta = p
		}
	}
	if *portFlag > 0 {
		porta = *portFlag
	}

	var listener net.Listener
	var baseURL string

	if serverMode {
		addr := fmt.Sprintf("0.0.0.0:%d", porta)
		listener, err = net.Listen("tcp", addr)
		if err != nil {
			log.Fatalf("Erro ao escutar em %s: %v", addr, err)
		}
		baseURL = strings.TrimSpace(os.Getenv("PUBLIC_URL"))
	} else {
		targetURL := fmt.Sprintf("http://127.0.0.1:%d", porta)
		if isAlreadyRunning(targetURL) {
			if !*noBrowser {
				_ = openBrowser(targetURL)
			}
			return
		}
		var actualPort int
		listener, actualPort, err = listenLocal(porta)
		if err != nil {
			log.Fatalf("Erro ao iniciar servidor local: %v", err)
		}
		baseURL = fmt.Sprintf("http://127.0.0.1:%d", actualPort)
	}

	driveClient := gdrive.NewClient(store)
	resumoSvc := resumo.NewService(driveClient)
	shutdownCh := make(chan struct{}, 1)

	srvHandler := server.New(store, driveClient, resumoSvc, server.Options{
		BaseURL:    baseURL,
		ServerMode: serverMode,
		AuthStore:  authStore,
		ShutdownCh: shutdownCh,
	}).Handler()

	httpServer := &http.Server{
		Handler:           srvHandler,
		ReadHeaderTimeout: 10 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		if serverMode {
			log.Printf("ResumoPlanilhas (MODO SERVIDOR) escutando em :%d (dados em %s)", porta, store.BaseDir())
		} else {
			log.Printf("ResumoPlanilhas iniciado em %s (dados em %s)", baseURL, store.BaseDir())
		}
		if err := httpServer.Serve(listener); err != nil && err != http.ErrServerClosed {
			errCh <- err
		}
	}()

	if !serverMode && !*noBrowser {
		go func() {
			time.Sleep(250 * time.Millisecond)
			_ = openBrowser(baseURL)
		}()
	}

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)

	select {
	case <-sigCh:
	case <-shutdownCh:
	case err := <-errCh:
		log.Fatalf("Erro no servidor HTTP: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_ = httpServer.Shutdown(ctx)
}

func parseBoolEnv(key string) bool {
	v := strings.ToLower(strings.TrimSpace(os.Getenv(key)))
	return v == "1" || v == "true" || v == "yes" || v == "sim"
}

func isAlreadyRunning(baseURL string) bool {
	client := &http.Client{Timeout: 400 * time.Millisecond}
	resp, err := client.Get(baseURL + "/api/health")
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return false
	}
	var payload struct {
		OK  bool   `json:"ok"`
		App string `json:"app"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return false
	}
	return payload.OK && payload.App == "ResumoPlanilhas"
}

func listenLocal(preferredPort int) (net.Listener, int, error) {
	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", preferredPort))
	if err == nil {
		return ln, preferredPort, nil
	}
	ln, err = net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, 0, err
	}
	addr := ln.Addr().(*net.TCPAddr)
	return ln, addr.Port, nil
}

func openBrowser(rawURL string) error {
	switch runtime.GOOS {
	case "windows":
		return exec.Command("rundll32", "url.dll,FileProtocolHandler", rawURL).Start()
	case "darwin":
		return exec.Command("open", rawURL).Start()
	default:
		return exec.Command("xdg-open", rawURL).Start()
	}
}
