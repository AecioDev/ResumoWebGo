package resumo

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"resumo-planilhas/internal/config"
	"resumo-planilhas/internal/gdrive"
)

type DriveSource interface {
	ListFolders(ctx context.Context, parentID string, sharedWithMe bool) ([]gdrive.FolderItem, error)
	ListSpreadsheets(ctx context.Context, folderID string) ([]gdrive.DriveFile, error)
	DownloadSpreadsheet(ctx context.Context, file gdrive.DriveFile, forceRefresh bool) ([]byte, bool, error)
}

type ResumoStats struct {
	TotalArquivos int    `json:"totalArquivos"`
	DoCache       int    `json:"doCache"`
	Baixados      int    `json:"baixados"`
	TempoMs       int64  `json:"tempoMs"`
	AtualizadoEm  string `json:"atualizadoEm"`
}

type MesSociosResumo struct {
	Numero int             `json:"numero"` // 1..12 ou 13 para Resumo Anual
	Nome   string          `json:"nome"`
	Linhas []SocioMesLinha `json:"linhas"`
	Total  SocioMesLinha   `json:"total"`
}

type ResumoSociosResponse struct {
	EmpresaID   string            `json:"empresaId"`
	EmpresaNome string            `json:"empresaNome"`
	Ano         int               `json:"ano"`
	Abas        []MesSociosResumo `json:"abas"`
	Stats       ResumoStats       `json:"stats"`
	Avisos      []string          `json:"avisos,omitempty"`
}

type SemanaAcertoResumo struct {
	Numero int           `json:"numero"` // 1..4 ou 5 para TOTAL DO MÊS
	Nome   string        `json:"nome"`
	Linhas []AcertoLinha `json:"linhas"`
	Total  AcertoLinha   `json:"total"`
}

type ResumoAcertoResponse struct {
	EmpresaID   string               `json:"empresaId"`
	EmpresaNome string               `json:"empresaNome"`
	Ano         int                  `json:"ano"`
	MesNumero   int                  `json:"mesNumero"`
	MesNome     string               `json:"mesNome"`
	PastaMes    string               `json:"pastaMes"`
	Abas        []SemanaAcertoResumo `json:"abas"`
	Stats       ResumoStats          `json:"stats"`
	Avisos      []string             `json:"avisos,omitempty"`
}

type MesAcertoDisponivel struct {
	Numero   int    `json:"numero"`
	Nome     string `json:"nome"`
	FolderID string `json:"folderId"`
	Pasta    string `json:"pasta"`
}

type Service struct {
	drive DriveSource
}

func NewService(drive DriveSource) *Service {
	return &Service{drive: drive}
}

func (s *Service) GerarResumoSocios(ctx context.Context, emp config.EmpresaConfig, ano int, forceRefresh bool) (*ResumoSociosResponse, error) {
	if !emp.UsaSocios() {
		return nil, fmt.Errorf("a empresa %s não está configurada para planilhas de Sócios", emp.Nome)
	}
	folderID, _ := emp.GetPastaSocios(ano)
	if strings.TrimSpace(folderID) == "" {
		return nil, fmt.Errorf("nenhuma pasta do Google Drive configurada para Sócios da empresa %s no ano %d", emp.Nome, ano)
	}

	inicio := time.Now()
	files, err := s.drive.ListSpreadsheets(ctx, folderID)
	if err != nil {
		return nil, err
	}
	if len(files) == 0 {
		return nil, errors.New("nenhuma planilha de sócio (.xlsx ou Google Sheets) encontrada nesta pasta do Google Drive")
	}

	type parseResult struct {
		data      *SocioWorkbookData
		fromCache bool
		aviso     string
	}

	results := make([]parseResult, len(files))
	sem := make(chan struct{}, 6)
	var wg sync.WaitGroup

	for i, file := range files {
		wg.Add(1)
		go func(idx int, f gdrive.DriveFile) {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
			case <-ctx.Done():
				results[idx] = parseResult{aviso: fmt.Sprintf("%s: operação cancelada", f.Name)}
				return
			}

			raw, cached, dlErr := s.drive.DownloadSpreadsheet(ctx, f, forceRefresh)
			if dlErr != nil {
				results[idx] = parseResult{aviso: fmt.Sprintf("%s: %v", f.Name, dlErr)}
				return
			}
			parsed, pErr := ParseSociosWorkbook(raw, f.Name, emp.Celulas)
			if pErr != nil {
				results[idx] = parseResult{fromCache: cached, aviso: fmt.Sprintf("%s: %v", f.Name, pErr)}
				return
			}
			results[idx] = parseResult{data: parsed, fromCache: cached}
		}(i, file)
	}
	wg.Wait()

	var socios []*SocioWorkbookData
	var avisos []string
	doCache, baixados := 0, 0
	for _, r := range results {
		if r.fromCache {
			doCache++
		} else if r.data != nil {
			baixados++
		}
		if r.aviso != "" {
			avisos = append(avisos, r.aviso)
		}
		if r.data != nil {
			socios = append(socios, r.data)
		}
	}

	sort.Slice(socios, func(i, j int) bool {
		return socios[i].Socio < socios[j].Socio
	})

	// Verifica se as planilhas lidas possuem ano interno (J2) divergente do ano filtrado
	anosEncontrados := map[int]bool{}
	for _, sc := range socios {
		if sc.AnoPlanilha > 0 && sc.AnoPlanilha != ano {
			anosEncontrados[sc.AnoPlanilha] = true
		}
	}
	if len(anosEncontrados) > 0 {
		var outros []string
		for y := range anosEncontrados {
			outros = append(outros, fmt.Sprintf("%d", y))
		}
		sort.Strings(outros)
		avisos = append(avisos, fmt.Sprintf("Atenção: há planilha(s) nesta pasta marcadas com ano %s na célula J2 (você selecionou %d). Verifique a pasta configurada para %d em 'Empresas e Drive'.", strings.Join(outros, "/"), ano, ano))
	}

	abas := make([]MesSociosResumo, 13)
	for m := 0; m < 12; m++ {
		aba := MesSociosResumo{
			Numero: m + 1,
			Nome:   MesesNomes[m],
			Linhas: make([]SocioMesLinha, 0, len(socios)),
			Total:  SocioMesLinha{Socio: "TOTAIS"},
		}
		for _, sc := range socios {
			l := sc.Meses[m]
			aba.Linhas = append(aba.Linhas, l)
			aba.Total.ValorTotal += l.ValorTotal
			aba.Total.Recebimento += l.Recebimento
			aba.Total.MercPagas += l.MercPagas
			aba.Total.ValSocio += l.ValSocio
			aba.Total.ValFirma += l.ValFirma
		}
		if aba.Total.ValorTotal > 0 {
			aba.Total.Percentual = aba.Total.Recebimento / aba.Total.ValorTotal
		}
		abas[m] = aba
	}

	anual := MesSociosResumo{
		Numero: 13,
		Nome:   "Resumo Anual",
		Linhas: make([]SocioMesLinha, 0, len(socios)),
		Total:  SocioMesLinha{Socio: "TOTAIS"},
	}
	for _, sc := range socios {
		l := sc.Anual
		anual.Linhas = append(anual.Linhas, l)
		anual.Total.ValorTotal += l.ValorTotal
		anual.Total.Recebimento += l.Recebimento
		anual.Total.MercPagas += l.MercPagas
		anual.Total.ValSocio += l.ValSocio
		anual.Total.ValFirma += l.ValFirma
	}
	if anual.Total.ValorTotal > 0 {
		anual.Total.Percentual = anual.Total.Recebimento / anual.Total.ValorTotal
	}
	abas[12] = anual

	return &ResumoSociosResponse{
		EmpresaID:   emp.ID,
		EmpresaNome: emp.Nome,
		Ano:         ano,
		Abas:        abas,
		Stats: ResumoStats{
			TotalArquivos: len(files),
			DoCache:       doCache,
			Baixados:      baixados,
			TempoMs:       time.Since(inicio).Milliseconds(),
			AtualizadoEm:  time.Now().Format("02/01/2006 15:04:05"),
		},
		Avisos: avisos,
	}, nil
}

func (s *Service) ListarMesesAcerto(ctx context.Context, emp config.EmpresaConfig, ano int) ([]MesAcertoDisponivel, error) {
	if !emp.UsaAcerto() {
		return nil, fmt.Errorf("a empresa %s não está configurada para Acerto Semanal", emp.Nome)
	}
	folderID, _ := emp.GetPastaAcerto(ano)
	if strings.TrimSpace(folderID) == "" {
		return nil, fmt.Errorf("nenhuma pasta do Google Drive configurada para Acerto Semanal da empresa %s no ano %d", emp.Nome, ano)
	}

	folders, err := s.drive.ListFolders(ctx, folderID, false)
	if err != nil {
		return nil, err
	}

	var disponiveis []MesAcertoDisponivel
	for m := 1; m <= 12; m++ {
		if found, ok := matchMonthFolder(folders, m); ok {
			disponiveis = append(disponiveis, MesAcertoDisponivel{
				Numero:   m,
				Nome:     MesesNomes[m-1],
				FolderID: found.ID,
				Pasta:    found.Name,
			})
		}
	}
	return disponiveis, nil
}

func (s *Service) GerarResumoAcerto(ctx context.Context, emp config.EmpresaConfig, ano int, mes int, forceRefresh bool) (*ResumoAcertoResponse, error) {
	if mes < 1 || mes > 12 {
		return nil, errors.New("mês inválido (informe de 1 a 12)")
	}
	if !emp.UsaAcerto() {
		return nil, fmt.Errorf("a empresa %s não está configurada para Acerto Semanal", emp.Nome)
	}
	folderID, _ := emp.GetPastaAcerto(ano)
	if strings.TrimSpace(folderID) == "" {
		return nil, fmt.Errorf("nenhuma pasta do Google Drive configurada para Acerto Semanal da empresa %s no ano %d", emp.Nome, ano)
	}

	inicio := time.Now()
	folders, err := s.drive.ListFolders(ctx, folderID, false)
	if err != nil {
		return nil, err
	}

	monthFolder, ok := matchMonthFolder(folders, mes)
	if !ok {
		return nil, fmt.Errorf("pasta do mês %02d - %s não encontrada no Google Drive", mes, strings.ToUpper(MesesNomes[mes-1]))
	}

	files, err := s.drive.ListSpreadsheets(ctx, monthFolder.ID)
	if err != nil {
		return nil, err
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("nenhuma planilha de acerto encontrada na pasta %s", monthFolder.Name)
	}

	type parseResult struct {
		data      *AcertoWorkbookData
		fromCache bool
		aviso     string
	}

	results := make([]parseResult, len(files))
	sem := make(chan struct{}, 6)
	var wg sync.WaitGroup

	for i, file := range files {
		wg.Add(1)
		go func(idx int, f gdrive.DriveFile) {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
			case <-ctx.Done():
				results[idx] = parseResult{aviso: fmt.Sprintf("%s: operação cancelada", f.Name)}
				return
			}

			raw, cached, dlErr := s.drive.DownloadSpreadsheet(ctx, f, forceRefresh)
			if dlErr != nil {
				results[idx] = parseResult{aviso: fmt.Sprintf("%s: %v", f.Name, dlErr)}
				return
			}
			parsed, pErr := ParseAcertoWorkbook(raw, f.Name)
			if pErr != nil {
				results[idx] = parseResult{fromCache: cached, aviso: fmt.Sprintf("%s: %v", f.Name, pErr)}
				return
			}
			results[idx] = parseResult{data: parsed, fromCache: cached}
		}(i, file)
	}
	wg.Wait()

	var socios []*AcertoWorkbookData
	var avisos []string
	doCache, baixados := 0, 0
	for _, r := range results {
		if r.fromCache {
			doCache++
		} else if r.data != nil {
			baixados++
		}
		if r.aviso != "" {
			avisos = append(avisos, r.aviso)
		}
		if r.data != nil {
			socios = append(socios, r.data)
		}
	}

	sort.Slice(socios, func(i, j int) bool {
		return socios[i].Socio < socios[j].Socio
	})

	abas := make([]SemanaAcertoResumo, 5)
	for sIdx := 0; sIdx < 4; sIdx++ {
		aba := SemanaAcertoResumo{
			Numero: sIdx + 1,
			Nome:   fmt.Sprintf("SEMANA 0%d", sIdx+1),
			Linhas: make([]AcertoLinha, 0, len(socios)),
			Total:  AcertoLinha{Socio: "TOTAIS"},
		}
		for _, sc := range socios {
			l := sc.Semanas[sIdx]
			aba.Linhas = append(aba.Linhas, l)
			acumularAcerto(&aba.Total, l)
		}
		aba.Total.Recalcular()
		abas[sIdx] = aba
	}

	totalMes := SemanaAcertoResumo{
		Numero: 5,
		Nome:   "TOTAL DO MÊS",
		Linhas: make([]AcertoLinha, 0, len(socios)),
		Total:  AcertoLinha{Socio: "TOTAIS"},
	}
	for _, sc := range socios {
		l := sc.TotalMes
		totalMes.Linhas = append(totalMes.Linhas, l)
		acumularAcerto(&totalMes.Total, l)
	}
	totalMes.Total.Recalcular()
	abas[4] = totalMes

	return &ResumoAcertoResponse{
		EmpresaID:   emp.ID,
		EmpresaNome: emp.Nome,
		Ano:         ano,
		MesNumero:   mes,
		MesNome:     MesesNomes[mes-1],
		PastaMes:    monthFolder.Name,
		Abas:        abas,
		Stats: ResumoStats{
			TotalArquivos: len(files),
			DoCache:       doCache,
			Baixados:      baixados,
			TempoMs:       time.Since(inicio).Milliseconds(),
			AtualizadoEm:  time.Now().Format("02/01/2006 15:04:05"),
		},
		Avisos: avisos,
	}, nil
}

func acumularAcerto(dest *AcertoLinha, src AcertoLinha) {
	dest.Cobranca += src.Cobranca
	dest.QtdNotas += src.QtdNotas
	dest.Recebimento += src.Recebimento
	dest.Combustivel += src.Combustivel
	dest.Refeicao += src.Refeicao
	dest.Hotel += src.Hotel
	dest.LavaJato += src.LavaJato
	dest.Oficina += src.Oficina
	dest.Fichas += src.Fichas
	dest.Vale += src.Vale
	dest.ValeAjudante += src.ValeAjudante
	dest.Pix += src.Pix
	dest.Deposito += src.Deposito
	dest.Dinheiro += src.Dinheiro
	dest.Cheque += src.Cheque
}

func matchMonthFolder(folders []gdrive.FolderItem, mes int) (gdrive.FolderItem, bool) {
	prefix2 := fmt.Sprintf("%02d", mes)
	monthNorm := normalizeKey(MesesNomes[mes-1])

	// 1) Correspondência forte: começa com "01" e contém o nome do mês
	for _, f := range folders {
		norm := normalizeKey(f.Name)
		if strings.HasPrefix(norm, prefix2) && strings.Contains(norm, monthNorm) {
			return f, true
		}
	}
	// 2) Contém o nome do mês
	for _, f := range folders {
		if strings.Contains(normalizeKey(f.Name), monthNorm) {
			return f, true
		}
	}
	// 3) Começa com "01-" ou "01 "
	for _, f := range folders {
		trimmed := strings.TrimSpace(f.Name)
		if strings.HasPrefix(trimmed, prefix2+" ") || strings.HasPrefix(trimmed, prefix2+"-") {
			return f, true
		}
	}
	return gdrive.FolderItem{}, false
}

// ListarNomesSocios retorna os nomes únicos dos sócios encontrados na pasta da empresa (útil ao criar acesso de sócio).
func (s *Service) ListarNomesSocios(ctx context.Context, emp config.EmpresaConfig, ano int) ([]string, error) {
	seen := map[string]bool{}
	var nomes []string

	addFromFolder := func(folderID string) {
		if strings.TrimSpace(folderID) == "" {
			return
		}
		files, err := s.drive.ListSpreadsheets(ctx, folderID)
		if err != nil {
			return
		}
		for _, f := range files {
			nome := socioNameFromFile(f.Name)
			if nome != "" && !seen[nome] {
				seen[nome] = true
				nomes = append(nomes, nome)
			}
		}
	}

	if emp.UsaSocios() {
		folderID, _ := emp.GetPastaSocios(ano)
		addFromFolder(folderID)
	}
	if emp.UsaAcerto() && len(nomes) == 0 {
		meses, err := s.ListarMesesAcerto(ctx, emp, ano)
		if err == nil && len(meses) > 0 {
			addFromFolder(meses[0].FolderID)
		}
	}

	sort.Strings(nomes)
	return nomes, nil
}

// FilterSociosForSocio remove todos os outros sócios e recalcula totais apenas com os dados do sócio informado.
func FilterSociosForSocio(res *ResumoSociosResponse, socioNome string) *ResumoSociosResponse {
	target := normalizeKey(socioNome)
	filtered := *res
	filtered.Abas = make([]MesSociosResumo, len(res.Abas))
	matchedCount := 0

	for i, aba := range res.Abas {
		novaAba := MesSociosResumo{
			Numero: aba.Numero,
			Nome:   aba.Nome,
			Linhas: make([]SocioMesLinha, 0, 1),
			Total:  SocioMesLinha{Socio: "TOTAL"},
		}
		for _, l := range aba.Linhas {
			if normalizeKey(l.Socio) == target {
				novaAba.Linhas = append(novaAba.Linhas, l)
				novaAba.Total.ValorTotal += l.ValorTotal
				novaAba.Total.Recebimento += l.Recebimento
				novaAba.Total.MercPagas += l.MercPagas
				novaAba.Total.ValSocio += l.ValSocio
				novaAba.Total.ValFirma += l.ValFirma
			}
		}
		if novaAba.Total.ValorTotal > 0 {
			novaAba.Total.Percentual = novaAba.Total.Recebimento / novaAba.Total.ValorTotal
		}
		if i == 0 {
			matchedCount = len(novaAba.Linhas)
		}
		filtered.Abas[i] = novaAba
	}

	filtered.Stats.TotalArquivos = matchedCount
	filtered.Stats.Baixados = 0
	filtered.Stats.DoCache = matchedCount
	filtered.Avisos = nil
	return &filtered
}

// FilterAcertoForSocio remove todos os outros sócios e recalcula totais apenas com os dados do sócio informado.
func FilterAcertoForSocio(res *ResumoAcertoResponse, socioNome string) *ResumoAcertoResponse {
	target := normalizeKey(socioNome)
	filtered := *res
	filtered.Abas = make([]SemanaAcertoResumo, len(res.Abas))
	matchedCount := 0

	for i, aba := range res.Abas {
		novaAba := SemanaAcertoResumo{
			Numero: aba.Numero,
			Nome:   aba.Nome,
			Linhas: make([]AcertoLinha, 0, 1),
			Total:  AcertoLinha{Socio: "TOTAL"},
		}
		for _, l := range aba.Linhas {
			if normalizeKey(l.Socio) == target {
				novaAba.Linhas = append(novaAba.Linhas, l)
				acumularAcerto(&novaAba.Total, l)
			}
		}
		novaAba.Total.Recalcular()
		if i == 0 {
			matchedCount = len(novaAba.Linhas)
		}
		filtered.Abas[i] = novaAba
	}

	filtered.Stats.TotalArquivos = matchedCount
	filtered.Stats.Baixados = 0
	filtered.Stats.DoCache = matchedCount
	filtered.Avisos = nil
	return &filtered
}
