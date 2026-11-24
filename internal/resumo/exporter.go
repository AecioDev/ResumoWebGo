package resumo

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/xuri/excelize/v2"
)

const (
	fmtCurrencyBR = `_-R$ * #,##0.00_-;-R$ * #,##0.00_-;_-R$ * "-"??_-;_-@`
	fmtPercent    = `0.00%`
)

// ExportSociosXLSX gera o arquivo Excel consolidado de Sócios sem vínculos externos.
func ExportSociosXLSX(res *ResumoSociosResponse) ([]byte, error) {
	f := excelize.NewFile()
	defer f.Close()

	styleTitle, _ := f.NewStyle(&excelize.Style{
		Font:      &excelize.Font{Bold: true, Size: 22},
		Alignment: &excelize.Alignment{Horizontal: "center", Vertical: "center"},
	})
	styleSubtitle, _ := f.NewStyle(&excelize.Style{
		Font:      &excelize.Font{Bold: true, Size: 15},
		Alignment: &excelize.Alignment{Horizontal: "center", Vertical: "center"},
	})
	styleMeta, _ := f.NewStyle(&excelize.Style{
		Font:      &excelize.Font{Bold: true, Size: 14},
		Alignment: &excelize.Alignment{Vertical: "center"},
	})
	styleMetaRight, _ := f.NewStyle(&excelize.Style{
		Font:      &excelize.Font{Bold: true, Size: 14},
		Alignment: &excelize.Alignment{Horizontal: "right", Vertical: "center"},
	})
	styleHeader, _ := f.NewStyle(&excelize.Style{
		Font:      &excelize.Font{Bold: true, Size: 13, Color: "#FFFFFF"},
		Fill:      excelize.Fill{Type: "pattern", Color: []string{"#1E3A5F"}, Pattern: 1},
		Alignment: &excelize.Alignment{Horizontal: "center", Vertical: "center"},
		Border:    allBorders(2),
	})
	styleName, _ := f.NewStyle(&excelize.Style{
		Font:      &excelize.Font{Size: 12},
		Alignment: &excelize.Alignment{Vertical: "center"},
		Border:    allBorders(1),
	})
	styleCurrency, _ := f.NewStyle(&excelize.Style{
		Font:         &excelize.Font{Size: 12},
		Alignment:    &excelize.Alignment{Vertical: "center"},
		Border:       allBorders(1),
		CustomNumFmt: strPtr(fmtCurrencyBR),
	})
	stylePercent, _ := f.NewStyle(&excelize.Style{
		Font:         &excelize.Font{Size: 12},
		Alignment:    &excelize.Alignment{Horizontal: "center", Vertical: "center"},
		Border:       allBorders(1),
		CustomNumFmt: strPtr(fmtPercent),
	})
	styleTotalLabel, _ := f.NewStyle(&excelize.Style{
		Font:      &excelize.Font{Bold: true, Size: 13, Color: "#0B4F9C"},
		Fill:      excelize.Fill{Type: "pattern", Color: []string{"#EAF2FF"}, Pattern: 1},
		Alignment: &excelize.Alignment{Vertical: "center"},
		Border:    allBorders(2),
	})
	styleTotalCurrency, _ := f.NewStyle(&excelize.Style{
		Font:         &excelize.Font{Bold: true, Size: 13, Color: "#0B4F9C"},
		Fill:         excelize.Fill{Type: "pattern", Color: []string{"#EAF2FF"}, Pattern: 1},
		Alignment:    &excelize.Alignment{Vertical: "center"},
		Border:       allBorders(2),
		CustomNumFmt: strPtr(fmtCurrencyBR),
	})
	styleTotalPercent, _ := f.NewStyle(&excelize.Style{
		Font:         &excelize.Font{Bold: true, Size: 13, Color: "#0B4F9C"},
		Fill:         excelize.Fill{Type: "pattern", Color: []string{"#EAF2FF"}, Pattern: 1},
		Alignment:    &excelize.Alignment{Horizontal: "center", Vertical: "center"},
		Border:       allBorders(2),
		CustomNumFmt: strPtr(fmtPercent),
	})

	headers := []string{"Sócio", "Valor Total", "Recebimento", "Percentual", "Mercadorias Pagas", "Sócio Ganhou", "Firma Ganhou"}
	widths := []float64{30, 22, 22, 16, 22, 22, 22}

	for idx, aba := range res.Abas {
		sheet := aba.Nome
		if idx == 0 {
			_ = f.SetSheetName("Sheet1", sheet)
		} else {
			_, _ = f.NewSheet(sheet)
		}

		_ = f.MergeCell(sheet, "A1", "G1")
		_ = f.SetCellValue(sheet, "A1", res.EmpresaNome)
		_ = f.SetCellStyle(sheet, "A1", "G1", styleTitle)
		_ = f.SetRowHeight(sheet, 1, 34)

		_ = f.MergeCell(sheet, "A2", "G2")
		_ = f.SetCellValue(sheet, "A2", "RESULTADO DE GANHOS POR SÓCIO")
		_ = f.SetCellStyle(sheet, "A2", "G2", styleSubtitle)
		_ = f.SetRowHeight(sheet, 2, 24)

		_ = f.SetCellValue(sheet, "A3", strings.ToUpper(aba.Nome))
		_ = f.SetCellStyle(sheet, "A3", "E3", styleMeta)
		_ = f.SetCellValue(sheet, "F3", "ANO:")
		_ = f.SetCellStyle(sheet, "F3", "F3", styleMetaRight)
		_ = f.SetCellValue(sheet, "G3", res.Ano)
		_ = f.SetCellStyle(sheet, "G3", "G3", styleMeta)
		_ = f.SetRowHeight(sheet, 3, 24)

		_ = f.SetRowHeight(sheet, 4, 28)
		for colIdx, h := range headers {
			colName, _ := excelize.ColumnNumberToName(colIdx + 1)
			_ = f.SetCellValue(sheet, fmt.Sprintf("%s4", colName), h)
			_ = f.SetColWidth(sheet, colName, colName, widths[colIdx])
		}
		_ = f.SetCellStyle(sheet, "A4", "G4", styleHeader)

		const linhaInicial = 5
		linha := linhaInicial
		isAnual := aba.Numero == 13

		for _, item := range aba.Linhas {
			_ = f.SetRowHeight(sheet, linha, 24)
			_ = f.SetCellValue(sheet, fmt.Sprintf("A%d", linha), item.Socio)
			_ = f.SetCellStyle(sheet, fmt.Sprintf("A%d", linha), fmt.Sprintf("A%d", linha), styleName)

			if !isAnual {
				_ = f.SetCellValue(sheet, fmt.Sprintf("B%d", linha), item.ValorTotal)
				_ = f.SetCellValue(sheet, fmt.Sprintf("C%d", linha), item.Recebimento)
				_ = f.SetCellValue(sheet, fmt.Sprintf("E%d", linha), item.MercPagas)
				_ = f.SetCellValue(sheet, fmt.Sprintf("F%d", linha), item.ValSocio)
				_ = f.SetCellValue(sheet, fmt.Sprintf("G%d", linha), item.ValFirma)
			} else {
				_ = f.SetCellFormula(sheet, fmt.Sprintf("B%d", linha), somaMesesFormula("B", linha))
				_ = f.SetCellFormula(sheet, fmt.Sprintf("C%d", linha), somaMesesFormula("C", linha))
				_ = f.SetCellFormula(sheet, fmt.Sprintf("E%d", linha), somaMesesFormula("E", linha))
				_ = f.SetCellFormula(sheet, fmt.Sprintf("F%d", linha), somaMesesFormula("F", linha))
				_ = f.SetCellFormula(sheet, fmt.Sprintf("G%d", linha), somaMesesFormula("G", linha))
			}

			_ = f.SetCellFormula(sheet, fmt.Sprintf("D%d", linha), fmt.Sprintf("IF(B%d>0,C%d/B%d,0)", linha, linha, linha))

			_ = f.SetCellStyle(sheet, fmt.Sprintf("B%d", linha), fmt.Sprintf("C%d", linha), styleCurrency)
			_ = f.SetCellStyle(sheet, fmt.Sprintf("D%d", linha), fmt.Sprintf("D%d", linha), stylePercent)
			_ = f.SetCellStyle(sheet, fmt.Sprintf("E%d", linha), fmt.Sprintf("G%d", linha), styleCurrency)
			linha++
		}

		linhaFinal := linha - 1
		linhaTotal := linha
		_ = f.SetRowHeight(sheet, linhaTotal, 28)
		_ = f.SetCellValue(sheet, fmt.Sprintf("A%d", linhaTotal), "TOTAIS")
		_ = f.SetCellStyle(sheet, fmt.Sprintf("A%d", linhaTotal), fmt.Sprintf("A%d", linhaTotal), styleTotalLabel)

		if linhaFinal >= linhaInicial {
			for _, col := range []string{"B", "C", "E", "F", "G"} {
				_ = f.SetCellFormula(sheet, fmt.Sprintf("%s%d", col, linhaTotal), fmt.Sprintf("SUM(%s%d:%s%d)", col, linhaInicial, col, linhaFinal))
			}
			_ = f.SetCellFormula(sheet, fmt.Sprintf("D%d", linhaTotal), fmt.Sprintf("IF(B%d>0,C%d/B%d,0)", linhaTotal, linhaTotal, linhaTotal))
		}

		_ = f.SetCellStyle(sheet, fmt.Sprintf("B%d", linhaTotal), fmt.Sprintf("C%d", linhaTotal), styleTotalCurrency)
		_ = f.SetCellStyle(sheet, fmt.Sprintf("D%d", linhaTotal), fmt.Sprintf("D%d", linhaTotal), styleTotalPercent)
		_ = f.SetCellStyle(sheet, fmt.Sprintf("E%d", linhaTotal), fmt.Sprintf("G%d", linhaTotal), styleTotalCurrency)
	}

	var buf bytes.Buffer
	if err := f.Write(&buf); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// ExportAcertoXLSX gera o arquivo Excel consolidado de Acerto Semanal sem vínculos externos.
func ExportAcertoXLSX(res *ResumoAcertoResponse) ([]byte, error) {
	f := excelize.NewFile()
	defer f.Close()

	styleTitle, _ := f.NewStyle(&excelize.Style{Font: &excelize.Font{Bold: true, Size: 20}})
	styleSub, _ := f.NewStyle(&excelize.Style{Font: &excelize.Font{Bold: true, Size: 14}})
	styleMeta, _ := f.NewStyle(&excelize.Style{Font: &excelize.Font{Bold: true, Size: 15}})

	makeGroupStyle := func(bg string) int {
		st, _ := f.NewStyle(&excelize.Style{
			Font:      &excelize.Font{Bold: true, Size: 14, Color: "#FFFFFF"},
			Fill:      excelize.Fill{Type: "pattern", Color: []string{bg}, Pattern: 1},
			Alignment: &excelize.Alignment{Horizontal: "center", Vertical: "center"},
			Border:    allBorders(2),
		})
		return st
	}
	styleGrpResumo := makeGroupStyle("#64748B")
	styleGrpDesp := makeGroupStyle("#8B0000")
	styleGrpRec := makeGroupStyle("#006400")

	styleColHeader, _ := f.NewStyle(&excelize.Style{
		Font:      &excelize.Font{Bold: true, Size: 10},
		Fill:      excelize.Fill{Type: "pattern", Color: []string{"#F1F5F9"}, Pattern: 1},
		Alignment: &excelize.Alignment{Horizontal: "center", Vertical: "center", WrapText: true},
		Border:    allBorders(2),
	})
	styleName, _ := f.NewStyle(&excelize.Style{
		Font:   &excelize.Font{Bold: true, Size: 10},
		Border: allBorders(1),
	})
	styleInt, _ := f.NewStyle(&excelize.Style{
		Font:      &excelize.Font{Size: 10},
		Alignment: &excelize.Alignment{Horizontal: "center"},
		Border:    allBorders(1),
	})
	styleCurrency, _ := f.NewStyle(&excelize.Style{
		Font:         &excelize.Font{Size: 10},
		Border:       allBorders(1),
		CustomNumFmt: strPtr(fmtCurrencyBR),
	})
	styleCurrencyBold, _ := f.NewStyle(&excelize.Style{
		Font:         &excelize.Font{Bold: true, Size: 10},
		Border:       allBorders(1),
		CustomNumFmt: strPtr(fmtCurrencyBR),
	})
	stylePercent, _ := f.NewStyle(&excelize.Style{
		Font:         &excelize.Font{Bold: true, Size: 10},
		Alignment:    &excelize.Alignment{Horizontal: "center"},
		Border:       allBorders(1),
		CustomNumFmt: strPtr(fmtPercent),
	})

	makeTotalStyle := func(bg string, numFmt string, center bool) int {
		align := &excelize.Alignment{Vertical: "center"}
		if center {
			align.Horizontal = "center"
		}
		st := &excelize.Style{
			Font:      &excelize.Font{Bold: true, Size: 10, Color: "#FFFF00"},
			Fill:      excelize.Fill{Type: "pattern", Color: []string{bg}, Pattern: 1},
			Alignment: align,
			Border:    allBorders(2),
		}
		if numFmt != "" {
			st.CustomNumFmt = strPtr(numFmt)
		}
		id, _ := f.NewStyle(st)
		return id
	}

	totResumoText := makeTotalStyle("#64748B", "", false)
	totResumoCur := makeTotalStyle("#64748B", fmtCurrencyBR, false)
	totResumoInt := makeTotalStyle("#64748B", "", true)
	totResumoPct := makeTotalStyle("#64748B", fmtPercent, true)
	totDespCur := makeTotalStyle("#8B0000", fmtCurrencyBR, false)
	totRecCur := makeTotalStyle("#006400", fmtCurrencyBR, false)

	headers := []string{
		"NOME", "COBRANÇA", "QTD NOTAS", "RECEBIMENTO", "%",
		"COMBUSTÍVEL", "REFEIÇÃO", "HOTEL", "LAVA JATO", "OFICINA", "FICHAS", "VALE", "VALE AJUDANTE", "TOTAL DESPESAS",
		"TOTAL A PAGAR", "PIX", "DEPÓSITO", "DINHEIRO", "CHEQUE", "TOTAL PAGAMENTO", "FALTA/SOBRA",
	}
	widths := []float64{
		22, 15, 11, 15, 9,
		14, 13, 13, 13, 13, 13, 13, 15, 16,
		16, 13, 13, 13, 13, 18, 15,
	}

	for idx, aba := range res.Abas {
		sheet := aba.Nome
		if idx == 0 {
			_ = f.SetSheetName("Sheet1", sheet)
		} else {
			_, _ = f.NewSheet(sheet)
		}

		_ = f.MergeCell(sheet, "A1", "E1")
		_ = f.SetCellValue(sheet, "A1", res.EmpresaNome)
		_ = f.SetCellStyle(sheet, "A1", "E1", styleTitle)

		_ = f.MergeCell(sheet, "A2", "E2")
		_ = f.SetCellValue(sheet, "A2", "RESUMO DE ACERTO SEMANAL - "+sheet)
		_ = f.SetCellStyle(sheet, "A2", "E2", styleSub)

		_ = f.SetCellValue(sheet, "A4", strings.ToUpper(res.MesNome))
		_ = f.SetCellValue(sheet, "B4", res.Ano)
		_ = f.SetCellStyle(sheet, "A4", "B4", styleMeta)

		_ = f.SetRowHeight(sheet, 5, 28)
		_ = f.MergeCell(sheet, "A5", "E5")
		_ = f.SetCellValue(sheet, "A5", "Resumo")
		_ = f.SetCellStyle(sheet, "A5", "E5", styleGrpResumo)

		_ = f.MergeCell(sheet, "F5", "N5")
		_ = f.SetCellValue(sheet, "F5", "Despesas")
		_ = f.SetCellStyle(sheet, "F5", "N5", styleGrpDesp)

		_ = f.MergeCell(sheet, "O5", "U5")
		_ = f.SetCellValue(sheet, "O5", "Recebimentos")
		_ = f.SetCellStyle(sheet, "O5", "U5", styleGrpRec)

		_ = f.SetRowHeight(sheet, 6, 26)
		for colIdx, h := range headers {
			colName, _ := excelize.ColumnNumberToName(colIdx + 1)
			_ = f.SetCellValue(sheet, fmt.Sprintf("%s6", colName), h)
			_ = f.SetColWidth(sheet, colName, colName, widths[colIdx])
		}
		_ = f.SetCellStyle(sheet, "A6", "U6", styleColHeader)

		const linhaInicial = 7
		linha := linhaInicial
		isTotalMes := aba.Numero == 5

		for _, item := range aba.Linhas {
			_ = f.SetCellValue(sheet, fmt.Sprintf("A%d", linha), item.Socio)
			_ = f.SetCellStyle(sheet, fmt.Sprintf("A%d", linha), fmt.Sprintf("A%d", linha), styleName)

			if !isTotalMes {
				_ = f.SetCellValue(sheet, fmt.Sprintf("B%d", linha), item.Cobranca)
				_ = f.SetCellValue(sheet, fmt.Sprintf("C%d", linha), item.QtdNotas)
				_ = f.SetCellValue(sheet, fmt.Sprintf("D%d", linha), item.Recebimento)
				_ = f.SetCellValue(sheet, fmt.Sprintf("F%d", linha), item.Combustivel)
				_ = f.SetCellValue(sheet, fmt.Sprintf("G%d", linha), item.Refeicao)
				_ = f.SetCellValue(sheet, fmt.Sprintf("H%d", linha), item.Hotel)
				_ = f.SetCellValue(sheet, fmt.Sprintf("I%d", linha), item.LavaJato)
				_ = f.SetCellValue(sheet, fmt.Sprintf("J%d", linha), item.Oficina)
				_ = f.SetCellValue(sheet, fmt.Sprintf("K%d", linha), item.Fichas)
				_ = f.SetCellValue(sheet, fmt.Sprintf("L%d", linha), item.Vale)
				_ = f.SetCellValue(sheet, fmt.Sprintf("M%d", linha), item.ValeAjudante)
				_ = f.SetCellValue(sheet, fmt.Sprintf("P%d", linha), item.Pix)
				_ = f.SetCellValue(sheet, fmt.Sprintf("Q%d", linha), item.Deposito)
				_ = f.SetCellValue(sheet, fmt.Sprintf("R%d", linha), item.Dinheiro)
				_ = f.SetCellValue(sheet, fmt.Sprintf("S%d", linha), item.Cheque)
			} else {
				for _, col := range []string{"B", "C", "D", "F", "G", "H", "I", "J", "K", "L", "M", "P", "Q", "R", "S"} {
					_ = f.SetCellFormula(sheet, fmt.Sprintf("%s%d", col, linha), somaSemanasFormula(col, linha))
				}
			}

			_ = f.SetCellFormula(sheet, fmt.Sprintf("E%d", linha), fmt.Sprintf("IF(B%d>0,D%d/B%d,0)", linha, linha, linha))
			_ = f.SetCellFormula(sheet, fmt.Sprintf("N%d", linha), fmt.Sprintf("SUM(F%d:M%d)", linha, linha))
			_ = f.SetCellFormula(sheet, fmt.Sprintf("O%d", linha), fmt.Sprintf("D%d-N%d", linha, linha))
			_ = f.SetCellFormula(sheet, fmt.Sprintf("T%d", linha), fmt.Sprintf("SUM(P%d:S%d)", linha, linha))
			_ = f.SetCellFormula(sheet, fmt.Sprintf("U%d", linha), fmt.Sprintf("T%d-O%d", linha, linha))

			_ = f.SetCellStyle(sheet, fmt.Sprintf("B%d", linha), fmt.Sprintf("B%d", linha), styleCurrency)
			_ = f.SetCellStyle(sheet, fmt.Sprintf("C%d", linha), fmt.Sprintf("C%d", linha), styleInt)
			_ = f.SetCellStyle(sheet, fmt.Sprintf("D%d", linha), fmt.Sprintf("D%d", linha), styleCurrency)
			_ = f.SetCellStyle(sheet, fmt.Sprintf("E%d", linha), fmt.Sprintf("E%d", linha), stylePercent)
			_ = f.SetCellStyle(sheet, fmt.Sprintf("F%d", linha), fmt.Sprintf("M%d", linha), styleCurrency)
			_ = f.SetCellStyle(sheet, fmt.Sprintf("N%d", linha), fmt.Sprintf("O%d", linha), styleCurrencyBold)
			_ = f.SetCellStyle(sheet, fmt.Sprintf("P%d", linha), fmt.Sprintf("S%d", linha), styleCurrency)
			_ = f.SetCellStyle(sheet, fmt.Sprintf("T%d", linha), fmt.Sprintf("U%d", linha), styleCurrencyBold)

			linha++
		}

		linhaFinal := linha - 1
		linhaTotal := linha
		_ = f.SetCellValue(sheet, fmt.Sprintf("A%d", linhaTotal), "TOTAIS")
		if linhaFinal >= linhaInicial {
			for colIdx := 2; colIdx <= 21; colIdx++ {
				col, _ := excelize.ColumnNumberToName(colIdx)
				switch colIdx {
				case 5:
					_ = f.SetCellFormula(sheet, fmt.Sprintf("E%d", linhaTotal), fmt.Sprintf("IF(B%d>0,D%d/B%d,0)", linhaTotal, linhaTotal, linhaTotal))
				case 15:
					_ = f.SetCellFormula(sheet, fmt.Sprintf("O%d", linhaTotal), fmt.Sprintf("D%d-N%d", linhaTotal, linhaTotal))
				case 21:
					_ = f.SetCellFormula(sheet, fmt.Sprintf("U%d", linhaTotal), fmt.Sprintf("T%d-O%d", linhaTotal, linhaTotal))
				default:
					_ = f.SetCellFormula(sheet, fmt.Sprintf("%s%d", col, linhaTotal), fmt.Sprintf("SUM(%s%d:%s%d)", col, linhaInicial, col, linhaFinal))
				}
			}
		}

		_ = f.SetCellStyle(sheet, fmt.Sprintf("A%d", linhaTotal), fmt.Sprintf("A%d", linhaTotal), totResumoText)
		_ = f.SetCellStyle(sheet, fmt.Sprintf("B%d", linhaTotal), fmt.Sprintf("B%d", linhaTotal), totResumoCur)
		_ = f.SetCellStyle(sheet, fmt.Sprintf("C%d", linhaTotal), fmt.Sprintf("C%d", linhaTotal), totResumoInt)
		_ = f.SetCellStyle(sheet, fmt.Sprintf("D%d", linhaTotal), fmt.Sprintf("D%d", linhaTotal), totResumoCur)
		_ = f.SetCellStyle(sheet, fmt.Sprintf("E%d", linhaTotal), fmt.Sprintf("E%d", linhaTotal), totResumoPct)
		_ = f.SetCellStyle(sheet, fmt.Sprintf("F%d", linhaTotal), fmt.Sprintf("N%d", linhaTotal), totDespCur)
		_ = f.SetCellStyle(sheet, fmt.Sprintf("O%d", linhaTotal), fmt.Sprintf("U%d", linhaTotal), totRecCur)
	}

	var buf bytes.Buffer
	if err := f.Write(&buf); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func somaMesesFormula(col string, linha int) string {
	parts := make([]string, len(MesesNomes))
	for i, m := range MesesNomes {
		parts[i] = fmt.Sprintf("'%s'!%s%d", m, col, linha)
	}
	return strings.Join(parts, "+")
}

func somaSemanasFormula(col string, linha int) string {
	return fmt.Sprintf("'SEMANA 01'!%s%d+'SEMANA 02'!%s%d+'SEMANA 03'!%s%d+'SEMANA 04'!%s%d",
		col, linha, col, linha, col, linha, col, linha)
}

func allBorders(style int) []excelize.Border {
	return []excelize.Border{
		{Type: "left", Color: "#334155", Style: style},
		{Type: "right", Color: "#334155", Style: style},
		{Type: "top", Color: "#334155", Style: style},
		{Type: "bottom", Color: "#334155", Style: style},
	}
}

func strPtr(s string) *string {
	return &s
}
