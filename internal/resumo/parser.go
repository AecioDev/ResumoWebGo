package resumo

import (
	"bytes"
	"fmt"
	"math"
	"path/filepath"
	"strconv"
	"strings"
	"unicode"

	"github.com/xuri/excelize/v2"

	"resumo-planilhas/internal/config"
)

var MesesNomes = []string{
	"Janeiro", "Fevereiro", "Março", "Abril", "Maio", "Junho",
	"Julho", "Agosto", "Setembro", "Outubro", "Novembro", "Dezembro",
}

type SocioMesLinha struct {
	Socio       string  `json:"socio"`
	Arquivo     string  `json:"arquivo"`
	ValorTotal  float64 `json:"valorTotal"`
	Recebimento float64 `json:"recebimento"`
	Percentual  float64 `json:"percentual"`
	MercPagas   float64 `json:"mercPagas"`
	ValSocio    float64 `json:"valSocio"`
	ValFirma    float64 `json:"valFirma"`
}

type SocioWorkbookData struct {
	Socio       string
	Arquivo     string
	AnoPlanilha int
	Meses       [12]SocioMesLinha
	Anual       SocioMesLinha
}

type AcertoLinha struct {
	Socio          string  `json:"socio"`
	Arquivo        string  `json:"arquivo"`
	Cobranca       float64 `json:"cobranca"`
	QtdNotas       float64 `json:"qtdNotas"`
	Recebimento    float64 `json:"recebimento"`
	Percentual     float64 `json:"percentual"`
	Combustivel    float64 `json:"combustivel"`
	Refeicao       float64 `json:"refeicao"`
	Hotel          float64 `json:"hotel"`
	LavaJato       float64 `json:"lavaJato"`
	Oficina        float64 `json:"oficina"`
	Fichas         float64 `json:"fichas"`
	Vale           float64 `json:"vale"`
	ValeAjudante   float64 `json:"valeAjudante"`
	TotalDespesas  float64 `json:"totalDespesas"`
	TotalAPagar    float64 `json:"totalAPagar"`
	Pix            float64 `json:"pix"`
	Deposito       float64 `json:"deposito"`
	Dinheiro       float64 `json:"dinheiro"`
	Cheque         float64 `json:"cheque"`
	TotalPagamento float64 `json:"totalPagamento"`
	FaltaSobra     float64 `json:"faltaSobra"`
}

type AcertoWorkbookData struct {
	Socio    string
	Arquivo  string
	Semanas  [4]AcertoLinha
	TotalMes AcertoLinha
}

func (a *AcertoLinha) Recalcular() {
	if a.Cobranca > 0 {
		a.Percentual = a.Recebimento / a.Cobranca
	} else {
		a.Percentual = 0
	}
	a.TotalDespesas = a.Combustivel + a.Refeicao + a.Hotel + a.LavaJato +
		a.Oficina + a.Fichas + a.Vale + a.ValeAjudante
	a.TotalAPagar = a.Recebimento - a.TotalDespesas
	a.TotalPagamento = a.Pix + a.Deposito + a.Dinheiro + a.Cheque
	a.FaltaSobra = a.TotalPagamento - a.TotalAPagar
}

func ParseSociosWorkbook(data []byte, fileName string, cel config.CelulasSocios) (*SocioWorkbookData, error) {
	f, err := excelize.OpenReader(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("erro ao abrir planilha %s: %w", fileName, err)
	}
	defer f.Close()

	cel = cel.Normalized()
	socioNome := socioNameFromFile(fileName)

	result := &SocioWorkbookData{
		Socio:   socioNome,
		Arquivo: fileName,
		Anual: SocioMesLinha{
			Socio:   socioNome,
			Arquivo: fileName,
		},
	}

	sheetIndex := buildNormalizedSheetMap(f.GetSheetList())

	for i, mesNome := range MesesNomes {
		linha := SocioMesLinha{
			Socio:   socioNome,
			Arquivo: fileName,
		}
		if actualSheet, ok := sheetIndex[normalizeKey(mesNome)]; ok {
			if result.AnoPlanilha == 0 {
				if y := int(math.Round(readFloatCell(f, actualSheet, "J2"))); y >= 2000 && y <= 2100 {
					result.AnoPlanilha = y
				}
			}
			linha.ValorTotal = readFloatCell(f, actualSheet, cel.ValorTotal)
			linha.Recebimento = readFloatCell(f, actualSheet, cel.Recebimento)
			if linha.ValorTotal > 0 {
				linha.Percentual = linha.Recebimento / linha.ValorTotal
			}
			linha.MercPagas = readFloatCell(f, actualSheet, cel.MercPagas)
			linha.ValSocio = readFloatCell(f, actualSheet, cel.ValSocio)
			linha.ValFirma = readFloatCell(f, actualSheet, cel.ValFirma)
		}

		result.Meses[i] = linha
		result.Anual.ValorTotal += linha.ValorTotal
		result.Anual.Recebimento += linha.Recebimento
		result.Anual.MercPagas += linha.MercPagas
		result.Anual.ValSocio += linha.ValSocio
		result.Anual.ValFirma += linha.ValFirma
	}

	if result.Anual.ValorTotal > 0 {
		result.Anual.Percentual = result.Anual.Recebimento / result.Anual.ValorTotal
	}

	return result, nil
}

func ParseAcertoWorkbook(data []byte, fileName string) (*AcertoWorkbookData, error) {
	f, err := excelize.OpenReader(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("erro ao abrir planilha de acerto %s: %w", fileName, err)
	}
	defer f.Close()

	socioNome := socioNameFromFile(fileName)
	result := &AcertoWorkbookData{
		Socio:   socioNome,
		Arquivo: fileName,
		TotalMes: AcertoLinha{
			Socio:   socioNome,
			Arquivo: fileName,
		},
	}

	sheetIndex := buildNormalizedSheetMap(f.GetSheetList())

	for s := 0; s < 4; s++ {
		linha := AcertoLinha{
			Socio:   socioNome,
			Arquivo: fileName,
		}
		candidates := []string{
			fmt.Sprintf("SEMANA 0%d", s+1),
			fmt.Sprintf("SEMANA %d", s+1),
			fmt.Sprintf("SEMANA0%d", s+1),
		}
		actualSheet := ""
		for _, cand := range candidates {
			if found, ok := sheetIndex[normalizeKey(cand)]; ok {
				actualSheet = found
				break
			}
		}

		if actualSheet != "" {
			linha.Cobranca = readFloatCell(f, actualSheet, "C4")
			linha.QtdNotas = readFloatCell(f, actualSheet, "E4")
			linha.Recebimento = readFloatCell(f, actualSheet, "C6")
			linha.Combustivel = readFloatCell(f, actualSheet, "C9")
			linha.Refeicao = readFloatCell(f, actualSheet, "C10")
			linha.Hotel = readFloatCell(f, actualSheet, "C11")
			linha.LavaJato = readFloatCell(f, actualSheet, "C12")
			linha.Oficina = readFloatCell(f, actualSheet, "C13")
			linha.Fichas = readFloatCell(f, actualSheet, "C14")
			linha.ValeAjudante = readFloatCell(f, actualSheet, "C15")
			linha.Vale = readFloatCell(f, actualSheet, "C16")
			linha.Pix = readFloatCell(f, actualSheet, "C21")
			linha.Deposito = readFloatCell(f, actualSheet, "C22")
			linha.Dinheiro = readFloatCell(f, actualSheet, "C23")
			linha.Cheque = readFloatCell(f, actualSheet, "C24")
			linha.Recalcular()
		}

		result.Semanas[s] = linha

		result.TotalMes.Cobranca += linha.Cobranca
		result.TotalMes.QtdNotas += linha.QtdNotas
		result.TotalMes.Recebimento += linha.Recebimento
		result.TotalMes.Combustivel += linha.Combustivel
		result.TotalMes.Refeicao += linha.Refeicao
		result.TotalMes.Hotel += linha.Hotel
		result.TotalMes.LavaJato += linha.LavaJato
		result.TotalMes.Oficina += linha.Oficina
		result.TotalMes.Fichas += linha.Fichas
		result.TotalMes.Vale += linha.Vale
		result.TotalMes.ValeAjudante += linha.ValeAjudante
		result.TotalMes.Pix += linha.Pix
		result.TotalMes.Deposito += linha.Deposito
		result.TotalMes.Dinheiro += linha.Dinheiro
		result.TotalMes.Cheque += linha.Cheque
	}

	result.TotalMes.Recalcular()
	return result, nil
}

func socioNameFromFile(fileName string) string {
	base := filepath.Base(fileName)
	ext := filepath.Ext(base)
	name := strings.TrimSuffix(base, ext)
	return strings.ToUpper(strings.TrimSpace(name))
}

func buildNormalizedSheetMap(sheets []string) map[string]string {
	m := make(map[string]string, len(sheets))
	for _, s := range sheets {
		m[normalizeKey(s)] = s
	}
	return m
}

func normalizeKey(s string) string {
	s = strings.TrimSpace(strings.ToLower(s))
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch r {
		case 'á', 'à', 'ã', 'â', 'ä':
			b.WriteRune('a')
		case 'é', 'è', 'ê', 'ë':
			b.WriteRune('e')
		case 'í', 'ì', 'î', 'ï':
			b.WriteRune('i')
		case 'ó', 'ò', 'õ', 'ô', 'ö':
			b.WriteRune('o')
		case 'ú', 'ù', 'û', 'ü':
			b.WriteRune('u')
		case 'ç':
			b.WriteRune('c')
		default:
			if !unicode.IsSpace(r) {
				b.WriteRune(r)
			}
		}
	}
	return b.String()
}

func readFloatCell(f *excelize.File, sheet, cell string) float64 {
	raw, err := f.GetCellValue(sheet, cell, excelize.Options{RawCellValue: true})
	if err == nil {
		if val, ok := parseNumber(raw); ok {
			return val
		}
	}

	// Fallback: tenta calcular a fórmula caso a planilha tenha sido salva sem cache de valor
	if calc, err := f.CalcCellValue(sheet, cell); err == nil {
		if val, ok := parseNumber(calc); ok {
			return val
		}
	}

	// Último fallback: valor formatado
	formatted, err := f.GetCellValue(sheet, cell)
	if err == nil {
		if val, ok := parseNumber(formatted); ok {
			return val
		}
	}
	return 0
}

func parseNumber(s string) (float64, bool) {
	s = strings.TrimSpace(s)
	if s == "" || s == "-" || strings.HasPrefix(s, "#") {
		return 0, false
	}

	// Tenta parse direto (padrão raw do XML: "1234.56")
	if v, err := strconv.ParseFloat(s, 64); err == nil && !math.IsNaN(v) && !math.IsInf(v, 0) {
		return v, true
	}

	// Limpa símbolos comuns de moeda/espaços (ex.: "R$ 1.234,56" ou "(1.234,56)")
	neg := false
	if strings.HasPrefix(s, "(") && strings.HasSuffix(s, ")") {
		neg = true
		s = s[1 : len(s)-1]
	}
	s = strings.ReplaceAll(s, "R$", "")
	s = strings.ReplaceAll(s, "%", "")
	s = strings.ReplaceAll(s, " ", "")
	s = strings.TrimSpace(s)
	if s == "" || s == "-" {
		return 0, false
	}

	hasComma := strings.Contains(s, ",")
	hasDot := strings.Contains(s, ".")
	if hasComma && hasDot {
		if strings.LastIndex(s, ",") > strings.LastIndex(s, ".") {
			// Formato BR: 1.234,56
			s = strings.ReplaceAll(s, ".", "")
			s = strings.ReplaceAll(s, ",", ".")
		} else {
			// Formato US: 1,234.56
			s = strings.ReplaceAll(s, ",", "")
		}
	} else if hasComma {
		s = strings.ReplaceAll(s, ",", ".")
	}

	v, err := strconv.ParseFloat(s, 64)
	if err != nil || math.IsNaN(v) || math.IsInf(v, 0) {
		return 0, false
	}
	if neg {
		v = -v
	}
	return v, true
}
