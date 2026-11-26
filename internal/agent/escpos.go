package agent

import "strings"

// Las órdenes ESC/POS que no dependen del sistema operativo.

// CP850 es la página multilingüe de ESC/POS: los bytes UTF-8 no son texto
// válido en una térmica que arranca en una página de un byte.
func escposInit() []byte        { return []byte{0x1b, 0x40, 0x1b, 0x74, 2} }
func escposAlignLeft() []byte   { return []byte{0x1b, 0x61, 0x00} }
func escposAlignCenter() []byte { return []byte{0x1b, 0x61, 0x01} }
func escposCutPartial() []byte  { return []byte{0x1d, 0x56, 0x42, 0x00} }
func escposOpenDrawer() []byte  { return []byte{0x1b, 0x70, 0x00, 0x19, 0xfa} }

// fontADots: ancho de una letra de la fuente normal (A), 12 puntos tanto en
// las térmicas de 203 ppp como en las de 180 ppp (Epson TM-T88).
const fontADots = 12

// thermalPrintableDots: el ancho imprimible en puntos que reporta el driver de
// Windows, solo si tiene sentido para una térmica de rollo (180–210 ppp, hasta
// 104 mm). Cualquier otra cosa —un driver genérico de texto, una impresora de
// oficina— da 0: no se toca el margen.
func thermalPrintableDots(horzRes, dpi int) int {
	if dpi < 180 || dpi > 210 || horzRes < 300 || horzRes > 832 {
		return 0
	}
	return horzRes
}

// escposPrintArea centra el bloque de texto en el papel (GS L margen izquierdo
// + GS W ancho del área).
//
// En 80 mm el texto va a 42 columnas (504 puntos), pero la mayoría de térmicas
// de 203 ppp imprimen 576 puntos: sin esto los 72 puntos sobrantes (9 mm)
// quedaban todos a la derecha y el ticket se veía corrido a la izquierda,
// mientras el título, el logo y el QR sí salían centrados. Con el área
// centrada, el centrado de ESC a 1 también cae en el medio del papel.
//
// Sin el ancho real (0) no se manda nada: el ticket sale como antes.
func escposPrintArea(printableDots, cols int) []byte {
	text := cols * fontADots
	if printableDots <= text {
		return nil
	}
	margin := (printableDots - text) / 2
	return []byte{
		0x1d, 0x4c, byte(margin), byte(margin >> 8), // GS L: margen izquierdo
		0x1d, 0x57, byte(text), byte(text >> 8), // GS W: ancho del área
	}
}

// escposResetPrintArea devuelve el área completa para quien imprima después
// sin inicializar la impresora.
func escposResetPrintArea(printableDots int) []byte {
	return []byte{0x1d, 0x4c, 0, 0, 0x1d, 0x57, byte(printableDots), byte(printableDots >> 8)}
}

func buildTicketBytes(title string, lines, footer []string, drawer, cut bool, qr string, logo *TicketLogo, paper string, printableDots int) []byte {
	cols := 42
	module := byte(6)
	if strings.HasPrefix(paper, "58") {
		cols, module = 32, 5
	}
	out := escposInit()
	area := escposPrintArea(printableDots, cols)
	out = append(out, area...)
	if drawing, err := escposLogo(logo); err == nil {
		out = append(out, drawing...)
	}
	write := func(text string) {
		for _, row := range wrapPrintText(text, cols) {
			out = append(out, encodePrintText(row)...)
			out = append(out, '\r', '\n')
		}
	}
	out = append(out, escposAlignCenter()...)
	write(title)
	out = append(out, escposAlignLeft()...)
	write(strings.Repeat("=", cols))
	for _, line := range lines {
		write(line)
	}
	if len(footer) > 0 {
		write(strings.Repeat("-", cols))
		for _, line := range footer {
			write(line)
		}
	}
	if strings.TrimSpace(qr) != "" {
		out = append(out, escposAlignCenter()...)
		out = append(out, escposQR(qr, module)...)
		out = append(out, escposAlignLeft()...)
	}
	out = append(out, 0x1b, 0x64, 3) // Avanzar antes de cortar, incluido el QR.
	if len(area) > 0 {
		out = append(out, escposResetPrintArea(printableDots)...)
	}
	if drawer {
		out = append(out, escposOpenDrawer()...)
	}
	if cut {
		out = append(out, escposCutPartial()...)
	}
	return out
}

// Conserva todo el texto; los saltos de línea no se sustituyen por recortes.
func wrapPrintText(text string, cols int) []string {
	var out []string
	text = normalizePrintText(text)
	for _, paragraph := range strings.Split(strings.ReplaceAll(text, "\r", ""), "\n") {
		runes := []rune(paragraph)
		for len(runes) > cols {
			end := cols
			for i := cols; i > cols/2; i-- {
				if runes[i] == ' ' {
					end = i
					break
				}
			}
			out = append(out, string(runes[:end]))
			runes = runes[end:]
			if len(runes) > 0 && runes[0] == ' ' {
				runes = runes[1:]
			}
		}
		out = append(out, string(runes))
	}
	return out
}

func escposQR(data string, moduleSize byte) []byte {
	payload := []byte(data)
	storeLen := len(payload) + 3
	out := []byte{0x1d, 0x28, 0x6b, 0x04, 0x00, 0x31, 0x41, 0x32, 0x00}
	out = append(out, 0x1d, 0x28, 0x6b, 0x03, 0x00, 0x31, 0x43, moduleSize)
	out = append(out, 0x1d, 0x28, 0x6b, 0x03, 0x00, 0x31, 0x45, 0x31)
	out = append(out, 0x1d, 0x28, 0x6b, byte(storeLen&0xff), byte((storeLen>>8)&0xff), 0x31, 0x50, 0x30)
	out = append(out, payload...)
	return append(out, 0x1d, 0x28, 0x6b, 0x03, 0x00, 0x31, 0x51, 0x30)
}

const cp850 = `ÇüéâäàåçêëèïîìÄÅÉæÆôöòûùÿÖÜø£Ø×ƒáíóúñÑªº¿®¬½¼¡«»░▒▓│┤ÁÂÀ©╣║╗╝¢¥┐└┴┬├─┼ãÃ╚╔╩╦╠═╬¤ðÐÊËÈıÍÎÏ┘┌█▄¦Ì▀ÓßÔÒõÕµþÞÚÛÙýÝ¯´­±‗¾¶§÷¸°¨·¹³²■ `

func encodePrintText(text string) []byte {
	text = normalizePrintText(text)
	var out []byte
	for _, r := range text {
		if r >= 32 && r < 127 {
			out = append(out, byte(r))
			continue
		}
		if r < 32 || r == 127 {
			continue
		} // El texto nunca introduce órdenes ESC/POS.
		index := -1
		for i, candidate := range []rune(cp850) {
			if candidate == r {
				index = i
				break
			}
		}
		if index >= 0 {
			out = append(out, byte(index+128))
		} else {
			out = append(out, '?')
		}
	}
	return out
}

func normalizePrintText(text string) string {
	return strings.NewReplacer("—", "-", "–", "-", "’", "'", "“", "\"", "”", "\"", "…", "...", "\t", " ").Replace(text)
}
