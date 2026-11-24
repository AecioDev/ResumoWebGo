package resumo

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/xuri/excelize/v2"

	"resumo-planilhas/internal/config"
	"resumo-planilhas/internal/gdrive"
)

type fakeDrive struct {
	folders map[string][]gdrive.FolderItem
	files   map[string][]gdrive.DriveFile
	content map[string][]byte
}

func (f *fakeDrive) ListFolders(_ context.Context, parentID string, _ bool) ([]gdrive.FolderItem, error) {
	return f.folders[parentID], nil
}

func (f *fakeDrive) ListSpreadsheets(_ context.Context, folderID string) ([]gdrive.DriveFile, error) {
	return f.files[folderID], nil
}

func (f *fakeDrive) DownloadSpreadsheet(_ context.Context, file gdrive.DriveFile, _ bool) ([]byte, bool, error) {
	return f.content[file.ID], false, nil
}

func makeSocioXLSX(t *testing.T, janValTot, janRec, janMerc, janSocio, janFirma float64) []byte {
	t.Helper()
	f := excelize.NewFile()
	defer f.Close()
	_ = f.SetSheetName("Sheet1", "Janeiro")
	_ = f.SetCellValue("Janeiro", "B12", janValTot)
	_ = f.SetCellValue("Janeiro", "D11", janRec)
	_ = f.SetCellValue("Janeiro", "F6", janMerc)
	_ = f.SetCellValue("Janeiro", "E20", janSocio)
	_ = f.SetCellValue("Janeiro", "E19", janFirma)

	_, _ = f.NewSheet("Março")
	_ = f.SetCellValue("Março", "B12", 1000.0)
	_ = f.SetCellValue("Março", "D11", 500.0)
	_ = f.SetCellValue("Março", "F6", 200.0)
	_ = f.SetCellValue("Março", "E20", 150.0)
	_ = f.SetCellValue("Março", "E19", 150.0)

	var buf bytes.Buffer
	if err := f.Write(&buf); err != nil {
		t.Fatalf("erro criando xlsx de teste: %v", err)
	}
	return buf.Bytes()
}

func makeAcertoXLSX(t *testing.T) []byte {
	t.Helper()
	f := excelize.NewFile()
	defer f.Close()
	_ = f.SetSheetName("Sheet1", "SEMANA 01")
	_ = f.SetCellValue("SEMANA 01", "C4", 2000.0)  // Cobrança
	_ = f.SetCellValue("SEMANA 01", "E4", 15)      // Qtd Notas
	_ = f.SetCellValue("SEMANA 01", "C6", 1500.0)  // Recebimento
	_ = f.SetCellValue("SEMANA 01", "C9", 100.0)   // Combustível
	_ = f.SetCellValue("SEMANA 01", "C10", 50.0)   // Refeição
	_ = f.SetCellValue("SEMANA 01", "C15", 80.0)   // Vale Ajudante
	_ = f.SetCellValue("SEMANA 01", "C16", 120.0)  // Vale
	_ = f.SetCellValue("SEMANA 01", "C21", 600.0)  // PIX
	_ = f.SetCellValue("SEMANA 01", "C23", 500.0)  // Dinheiro

	var buf bytes.Buffer
	if err := f.Write(&buf); err != nil {
		t.Fatalf("erro criando xlsx de acerto: %v", err)
	}
	return buf.Bytes()
}

func TestResumoSociosAndExport(t *testing.T) {
	drive := &fakeDrive{
		files: map[string][]gdrive.DriveFile{
			"folder_socios": {
				{ID: "f1", Name: "JOAO.xlsx", MimeType: gdrive.MimeXLSX, ModifiedTime: "2026-11-23T10:00:00Z"},
				{ID: "f2", Name: "MARIA.xlsx", MimeType: gdrive.MimeXLSX, ModifiedTime: "2026-11-23T10:00:00Z"},
			},
		},
		content: map[string][]byte{
			"f1": makeSocioXLSX(t, 2000, 1000, 300, 350, 350),
			"f2": makeSocioXLSX(t, 4000, 3000, 500, 1250, 1250),
		},
	}

	svc := NewService(drive)
	emp := config.EmpresaConfig{
		ID:            "emp1",
		Nome:          "JS DISTRIBUIDORA",
		Modo:          config.ModoSocios,
		PastaSociosID: "folder_socios",
		Celulas:       config.DefaultCelulasSocios(),
	}

	res, err := svc.GerarResumoSocios(context.Background(), emp, 2027, false)
	if err != nil {
		t.Fatalf("GerarResumoSocios falhou: %v", err)
	}
	if len(res.Abas) != 13 {
		t.Fatalf("esperava 13 abas, veio %d", len(res.Abas))
	}

	// Janeiro: 2000 + 4000 = 6000
	if res.Abas[0].Total.ValorTotal != 6000 {
		t.Fatalf("esperava ValorTotal Janeiro=6000, veio %v", res.Abas[0].Total.ValorTotal)
	}
	// Anual: Janeiro (6000) + Março (1000+1000) = 8000
	if res.Abas[12].Total.ValorTotal != 8000 {
		t.Fatalf("esperava ValorTotal Anual=8000, veio %v", res.Abas[12].Total.ValorTotal)
	}

	// Exportação sem vínculos externos
	xlsxBytes, err := ExportSociosXLSX(res)
	if err != nil {
		t.Fatalf("ExportSociosXLSX falhou: %v", err)
	}
	outFile, err := excelize.OpenReader(bytes.NewReader(xlsxBytes))
	if err != nil {
		t.Fatalf("erro ao reabrir xlsx exportado: %v", err)
	}
	defer outFile.Close()

	formulaB5, _ := outFile.GetCellFormula("Janeiro", "B5")
	if strings.Contains(formulaB5, "[") {
		t.Fatalf("não deveria conter vínculo externo em Janeiro!B5: %s", formulaB5)
	}
	valB5, _ := outFile.GetCellValue("Janeiro", "B5", excelize.Options{RawCellValue: true})
	if valB5 != "2000" {
		t.Fatalf("esperava Janeiro!B5=2000, veio %q", valB5)
	}

	// Empresa só Sócios deve bloquear Acerto
	if _, err := svc.GerarResumoAcerto(context.Background(), emp, 2027, 1, false); err == nil {
		t.Fatalf("esperava erro ao pedir Acerto de empresa configurada apenas como Sócios")
	}

	// Pastas separadas por ano devem ler a pasta correspondente ao ano selecionado
	driveMulti := &fakeDrive{
		files: map[string][]gdrive.DriveFile{
			"folder_2026": {{ID: "f26", Name: "ANA.xlsx", MimeType: gdrive.MimeXLSX, ModifiedTime: "2026-01-01T00:00:00Z"}},
			"folder_2027": {{ID: "f27", Name: "ANA.xlsx", MimeType: gdrive.MimeXLSX, ModifiedTime: "2027-01-01T00:00:00Z"}},
		},
		content: map[string][]byte{
			"f26": makeSocioXLSX(t, 1000, 500, 100, 200, 200),
			"f27": makeSocioXLSX(t, 9000, 4500, 900, 1800, 1800),
		},
	}
	svcMulti := NewService(driveMulti)
	empMulti := config.EmpresaConfig{
		ID:   "emp_multi",
		Nome: "MULTI ANO",
		Modo: config.ModoSocios,
		PastasPorAno: map[string]config.PastaAnoRef{
			"2026": {SociosID: "folder_2026"},
			"2027": {SociosID: "folder_2027"},
		},
		Celulas: config.DefaultCelulasSocios(),
	}
	res26, err := svcMulti.GerarResumoSocios(context.Background(), empMulti, 2026, false)
	if err != nil || res26.Abas[0].Total.ValorTotal != 1000 {
		t.Fatalf("ano 2026 esperado=1000, veio=%v err=%v", res26, err)
	}
	res27, err := svcMulti.GerarResumoSocios(context.Background(), empMulti, 2027, false)
	if err != nil || res27.Abas[0].Total.ValorTotal != 9000 {
		t.Fatalf("ano 2027 esperado=9000, veio=%v err=%v", res27, err)
	}
}

func TestResumoAcertoAndExport(t *testing.T) {
	drive := &fakeDrive{
		folders: map[string][]gdrive.FolderItem{
			"folder_acerto": {
				{ID: "m01", Name: "01 - JANEIRO"},
			},
		},
		files: map[string][]gdrive.DriveFile{
			"m01": {
				{ID: "a1", Name: "CARLOS.xlsx", MimeType: gdrive.MimeXLSX, ModifiedTime: "2026-11-23T10:00:00Z"},
			},
		},
		content: map[string][]byte{
			"a1": makeAcertoXLSX(t),
		},
	}

	svc := NewService(drive)
	emp := config.EmpresaConfig{
		ID:            "emp2",
		Nome:          "RF MAGAZINE",
		Modo:          config.ModoAmbos,
		PastaAcertoID: "folder_acerto",
	}

	res, err := svc.GerarResumoAcerto(context.Background(), emp, 2027, 1, false)
	if err != nil {
		t.Fatalf("GerarResumoAcerto falhou: %v", err)
	}
	if len(res.Abas) != 5 {
		t.Fatalf("esperava 5 abas, veio %d", len(res.Abas))
	}

	sem1 := res.Abas[0].Linhas[0]
	// Despesas: 100 + 50 + 80 (Vale Ajud) + 120 (Vale) = 350
	if sem1.TotalDespesas != 350 {
		t.Fatalf("esperava TotalDespesas=350, veio %v", sem1.TotalDespesas)
	}
	if sem1.Vale != 120 || sem1.ValeAjudante != 80 {
		t.Fatalf("mapeamento Vale(C16)/ValeAjudante(C15) incorreto: vale=%v ajud=%v", sem1.Vale, sem1.ValeAjudante)
	}
	// Total a pagar: 1500 - 350 = 1150; Pagamento: 600 + 500 = 1100; Falta/Sobra: 1100 - 1150 = -50
	if sem1.TotalAPagar != 1150 || sem1.TotalPagamento != 1100 || sem1.FaltaSobra != -50 {
		t.Fatalf("totais incorretos: pagar=%v pgto=%v falta=%v", sem1.TotalAPagar, sem1.TotalPagamento, sem1.FaltaSobra)
	}

	xlsxBytes, err := ExportAcertoXLSX(res)
	if err != nil {
		t.Fatalf("ExportAcertoXLSX falhou: %v", err)
	}
	outFile, err := excelize.OpenReader(bytes.NewReader(xlsxBytes))
	if err != nil {
		t.Fatalf("erro ao reabrir xlsx de acerto exportado: %v", err)
	}
	defer outFile.Close()

	if len(outFile.GetSheetList()) != 5 {
		t.Fatalf("esperava 5 abas no Excel de acerto, veio %d", len(outFile.GetSheetList()))
	}
}
