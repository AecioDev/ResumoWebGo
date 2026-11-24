# Resumo de Planilhas (Go + Google Drive API)

Aplicativo em Go que lê diretamente planilhas no **Google Drive** (somente leitura), consolida **Sócios** e **Acerto Semanal** em paralelo e disponibiliza um painel web com impressão e exportação Excel **sem vínculos externos quebrados**.

## Perfis de Acesso (Modo Servidor / Docker)
- **Admin:** configura credenciais do Google Drive, empresas, pastas por ano (`2026`, `2027`...) e cria gestores/usuários.
- **Gestor:** vê apenas as empresas vinculadas a ele e gerencia os acessos dos próprios sócios em **👥 Acessos dos Sócios**.
- **Sócio:** visualiza exclusivamente os próprios dados (extrato mês a mês de Janeiro a Dezembro + Total).

---

## Deploy na Oracle Cloud (ARM64 `aarch64` / AMD64)

### 1. Clonar e configurar `.env` na VPS (em pasta separada)
```bash
cd ~
git clone <URL_DO_SEU_REPOSITORIO> resumo-planilhas
cd resumo-planilhas
cp .env.example .env
nano .env
```
Preencha `ADMIN_PASSWORD` e `SESSION_SECRET` (dica para gerar uma chave segura: `openssl rand -hex 32`).

### 2. Subir o container (não interfere em nenhum outro projeto)
```bash
docker compose up -d --build
docker logs -f resumo-planilhas
```

### 3. Liberar a porta `8745` na Oracle Cloud
1. **Painel da Oracle Cloud:** *Networking → Virtual Cloud Networks → sua VCN → Subnet → Security Lists → Default Security List → Add Ingress Rules*:
   - Source CIDR: `0.0.0.0/0`
   - IP Protocol: `TCP`
   - Destination Port Range: `8745`
2. **Firewall interno do Ubuntu (se necessário):**
   - Se usar `ufw`: `sudo ufw allow 8745/tcp`
   - Se usar `iptables` padrão da Oracle:
     ```bash
     sudo iptables -I INPUT 6 -m state --state NEW -p tcp --dport 8745 -j ACCEPT
     sudo netfilter-persistent save
     ```

### 4. Primeiro acesso e Conta de Serviço
1. Acesse `http://SEU_IP_ORACLE:8745` e entre com o `ADMIN_USER` e `ADMIN_PASSWORD` do `.env`.
2. Envie o JSON da **Conta de Serviço (Service Account)** direto pela tela inicial (os dados ficam persistidos em `./data/`).
3. Cadastre empresas, pastas por ano e usuários.

### Atualizações futuras via Git
```bash
cd ~/resumo-planilhas
git pull
docker compose up -d --build
```
*(Os dados em `./data/` — `config.json`, `users.json`, `credentials.json` e `cache/` — são preservados entre atualizações.)*

---

## Build Desktop Windows (opcional)
```powershell
.\build-and-install.ps1
```
