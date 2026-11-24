package main

import (
	"bytes"
	"encoding/binary"
	"flag"
	"fmt"
	"os"
)

type icoEntry struct {
	Width      byte
	Height     byte
	ColorCount byte
	Reserved   byte
	Planes     uint16
	BitCount   uint16
	BytesInRes uint32
	ImageOff   uint32
	Data       []byte
}

func main() {
	inPath := flag.String("in", "icone.ico", "Caminho do arquivo .ico de entrada")
	outSyso := flag.String("out", "cmd/resumoplanilhas/rsrc_windows_amd64.syso", "Caminho do arquivo .syso de saída")
	outFavicon := flag.String("favicon", "", "Opcional: caminho para copiar como favicon.ico")
	flag.Parse()

	raw, err := os.ReadFile(*inPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "erro ao ler %s: %v\n", *inPath, err)
		os.Exit(1)
	}

	entries, err := parseICO(raw)
	if err != nil {
		fmt.Fprintf(os.Stderr, "arquivo .ico inválido (%s): %v\n", *inPath, err)
		os.Exit(1)
	}

	syso, err := buildRsrcCOFF(entries)
	if err != nil {
		fmt.Fprintf(os.Stderr, "erro ao gerar .syso: %v\n", err)
		os.Exit(1)
	}

	if err := os.WriteFile(*outSyso, syso, 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "erro ao gravar %s: %v\n", *outSyso, err)
		os.Exit(1)
	}

	if *outFavicon != "" {
		if err := os.WriteFile(*outFavicon, raw, 0o644); err != nil {
			fmt.Fprintf(os.Stderr, "erro ao gravar favicon %s: %v\n", *outFavicon, err)
			os.Exit(1)
		}
	}
}

func parseICO(raw []byte) ([]icoEntry, error) {
	if len(raw) < 6 {
		return nil, fmt.Errorf("arquivo muito curto")
	}
	reserved := binary.LittleEndian.Uint16(raw[0:2])
	typ := binary.LittleEndian.Uint16(raw[2:4])
	count := int(binary.LittleEndian.Uint16(raw[4:6]))
	if reserved != 0 || typ != 1 || count <= 0 {
		return nil, fmt.Errorf("cabeçalho ICO inválido (reserved=%d type=%d count=%d)", reserved, typ, count)
	}
	if len(raw) < 6+count*16 {
		return nil, fmt.Errorf("tabela de ícones truncada")
	}

	entries := make([]icoEntry, 0, count)
	for i := 0; i < count; i++ {
		off := 6 + i*16
		e := icoEntry{
			Width:      raw[off],
			Height:     raw[off+1],
			ColorCount: raw[off+2],
			Reserved:   raw[off+3],
			Planes:     binary.LittleEndian.Uint16(raw[off+4 : off+6]),
			BitCount:   binary.LittleEndian.Uint16(raw[off+6 : off+8]),
			BytesInRes: binary.LittleEndian.Uint32(raw[off+8 : off+12]),
			ImageOff:   binary.LittleEndian.Uint32(raw[off+12 : off+16]),
		}
		end := uint64(e.ImageOff) + uint64(e.BytesInRes)
		if end > uint64(len(raw)) || e.BytesInRes == 0 {
			return nil, fmt.Errorf("imagem #%d fora dos limites do arquivo", i+1)
		}
		e.Data = raw[e.ImageOff:end]
		entries = append(entries, e)
	}
	return entries, nil
}

// buildRsrcCOFF cria um objeto COFF x86_64 (.syso) contendo RT_ICON e RT_GROUP_ICON.
func buildRsrcCOFF(entries []icoEntry) ([]byte, error) {
	const (
		rtIcon      = 3
		rtGroupIcon = 14
		langENUS    = 0x0409
		dirFlag     = 0x80000000
	)

	n := len(entries)

	// Monta payload do RT_GROUP_ICON
	var grpBuf bytes.Buffer
	_ = binary.Write(&grpBuf, binary.LittleEndian, uint16(0)) // Reserved
	_ = binary.Write(&grpBuf, binary.LittleEndian, uint16(1)) // Type = 1 (ICO)
	_ = binary.Write(&grpBuf, binary.LittleEndian, uint16(n)) // Count
	for i, e := range entries {
		grpBuf.WriteByte(e.Width)
		grpBuf.WriteByte(e.Height)
		grpBuf.WriteByte(e.ColorCount)
		grpBuf.WriteByte(0)
		_ = binary.Write(&grpBuf, binary.LittleEndian, e.Planes)
		_ = binary.Write(&grpBuf, binary.LittleEndian, e.BitCount)
		_ = binary.Write(&grpBuf, binary.LittleEndian, e.BytesInRes)
		_ = binary.Write(&grpBuf, binary.LittleEndian, uint16(i+1)) // Icon ID
	}
	grpData := grpBuf.Bytes()

	// Calcula offsets da árvore .rsrc
	// Layout:
	// [0] Root Dir (16 + 2*8 = 32 bytes)
	// [1] Type=RT_ICON Dir (16 + n*8 bytes)
	// [2] Type=RT_GROUP_ICON Dir (16 + 1*8 = 24 bytes)
	// [3..3+n-1] Lang Dir para cada RT_ICON (24 bytes cada)
	// [3+n] Lang Dir para RT_GROUP_ICON (24 bytes)
	// [dataEntries] (n + 1) * 16 bytes
	// [payloads] cada payload alinhado em 4 bytes
	offRoot := uint32(0)
	offTypeIcon := offRoot + 16 + 2*8
	offTypeGrp := offTypeIcon + 16 + uint32(n)*8
	offLangIconStart := offTypeGrp + 16 + 8
	offLangGrp := offLangIconStart + uint32(n)*24
	offDataEntries := offLangGrp + 24
	offPayloads := offDataEntries + uint32(n+1)*16

	// Calcula offsets dos payloads
	payloadOffsets := make([]uint32, n+1)
	cur := offPayloads
	for i, e := range entries {
		payloadOffsets[i] = cur
		cur += align4(uint32(len(e.Data)))
	}
	payloadOffsets[n] = cur
	cur += align4(uint32(len(grpData)))
	rsrcSize := cur

	rsrc := make([]byte, rsrcSize)

	writeDirHeader := func(off uint32, idEntries uint16) {
		binary.LittleEndian.PutUint16(rsrc[off+12:off+14], 0)         // NumberOfNamedEntries
		binary.LittleEndian.PutUint16(rsrc[off+14:off+16], idEntries) // NumberOfIdEntries
	}
	writeDirEntry := func(off uint32, id uint32, target uint32) {
		binary.LittleEndian.PutUint32(rsrc[off:off+4], id)
		binary.LittleEndian.PutUint32(rsrc[off+4:off+8], target)
	}

	// Root directory
	writeDirHeader(offRoot, 2)
	writeDirEntry(offRoot+16, rtIcon, dirFlag|offTypeIcon)
	writeDirEntry(offRoot+24, rtGroupIcon, dirFlag|offTypeGrp)

	// RT_ICON directory
	writeDirHeader(offTypeIcon, uint16(n))
	for i := 0; i < n; i++ {
		langOff := offLangIconStart + uint32(i)*24
		writeDirEntry(offTypeIcon+16+uint32(i)*8, uint32(i+1), dirFlag|langOff)

		dataEntryOff := offDataEntries + uint32(i)*16
		writeDirHeader(langOff, 1)
		writeDirEntry(langOff+16, langENUS, dataEntryOff)

		binary.LittleEndian.PutUint32(rsrc[dataEntryOff:dataEntryOff+4], payloadOffsets[i])
		binary.LittleEndian.PutUint32(rsrc[dataEntryOff+4:dataEntryOff+8], uint32(len(entries[i].Data)))
		copy(rsrc[payloadOffsets[i]:], entries[i].Data)
	}

	// RT_GROUP_ICON directory
	writeDirHeader(offTypeGrp, 1)
	writeDirEntry(offTypeGrp+16, 1, dirFlag|offLangGrp)

	grpDataEntryOff := offDataEntries + uint32(n)*16
	writeDirHeader(offLangGrp, 1)
	writeDirEntry(offLangGrp+16, langENUS, grpDataEntryOff)
	binary.LittleEndian.PutUint32(rsrc[grpDataEntryOff:grpDataEntryOff+4], payloadOffsets[n])
	binary.LittleEndian.PutUint32(rsrc[grpDataEntryOff+4:grpDataEntryOff+8], uint32(len(grpData)))
	copy(rsrc[payloadOffsets[n]:], grpData)

	// Monta o arquivo COFF completo
	const (
		coffHeaderSize = 20
		secHeaderSize  = 40
		relocSize      = 10
		symSize        = 18
		amd64Machine   = 0x8664
		relAddr32NB    = 0x0003
	)

	numRelocs := uint16(n + 1)
	ptrRawData := uint32(coffHeaderSize + secHeaderSize)
	ptrRelocs := ptrRawData + rsrcSize
	ptrSymTable := ptrRelocs + uint32(numRelocs)*relocSize

	var out bytes.Buffer
	// COFF File Header (20 bytes)
	_ = binary.Write(&out, binary.LittleEndian, uint16(amd64Machine))
	_ = binary.Write(&out, binary.LittleEndian, uint16(1))           // NumberOfSections
	_ = binary.Write(&out, binary.LittleEndian, uint32(0))           // TimeDateStamp
	_ = binary.Write(&out, binary.LittleEndian, ptrSymTable)         // PointerToSymbolTable
	_ = binary.Write(&out, binary.LittleEndian, uint32(1))           // NumberOfSymbols
	_ = binary.Write(&out, binary.LittleEndian, uint16(0))           // SizeOfOptionalHeader
	_ = binary.Write(&out, binary.LittleEndian, uint16(0x0104))      // Characteristics

	// Section Header ".rsrc" (40 bytes)
	secName := [8]byte{'.', 'r', 's', 'r', 'c', 0, 0, 0}
	out.Write(secName[:])
	_ = binary.Write(&out, binary.LittleEndian, rsrcSize)            // VirtualSize
	_ = binary.Write(&out, binary.LittleEndian, uint32(0))           // VirtualAddress
	_ = binary.Write(&out, binary.LittleEndian, rsrcSize)            // SizeOfRawData
	_ = binary.Write(&out, binary.LittleEndian, ptrRawData)          // PointerToRawData
	_ = binary.Write(&out, binary.LittleEndian, ptrRelocs)           // PointerToRelocations
	_ = binary.Write(&out, binary.LittleEndian, uint32(0))           // PointerToLinenumbers
	_ = binary.Write(&out, binary.LittleEndian, numRelocs)           // NumberOfRelocations
	_ = binary.Write(&out, binary.LittleEndian, uint16(0))           // NumberOfLinenumbers
	_ = binary.Write(&out, binary.LittleEndian, uint32(0x40000040))  // Characteristics

	// Raw section data
	out.Write(rsrc)

	// Relocations (1 por IMAGE_RESOURCE_DATA_ENTRY)
	for i := 0; i < n+1; i++ {
		dataEntryOff := offDataEntries + uint32(i)*16
		_ = binary.Write(&out, binary.LittleEndian, dataEntryOff)        // VirtualAddress
		_ = binary.Write(&out, binary.LittleEndian, uint32(0))           // SymbolTableIndex (.rsrc)
		_ = binary.Write(&out, binary.LittleEndian, uint16(relAddr32NB)) // Type
	}

	// Symbol Table (1 símbolo: seção .rsrc)
	out.Write(secName[:])
	_ = binary.Write(&out, binary.LittleEndian, uint32(0)) // Value
	_ = binary.Write(&out, binary.LittleEndian, int16(1))  // SectionNumber (1-based)
	_ = binary.Write(&out, binary.LittleEndian, uint16(0)) // Type
	out.WriteByte(3)                                       // StorageClass = IMAGE_SYM_CLASS_STATIC
	out.WriteByte(0)                                       // NumberOfAuxSymbols

	// String Table (4 bytes = vazia)
	_ = binary.Write(&out, binary.LittleEndian, uint32(4))

	return out.Bytes(), nil
}

func align4(v uint32) uint32 {
	if rem := v % 4; rem != 0 {
		return v + (4 - rem)
	}
	return v
}
