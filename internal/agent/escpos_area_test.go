package agent

import (
	"bytes"
	"testing"
)

// En una térmica de 80 mm y 203 ppp (576 puntos) el texto de 42 columnas mide
// 504 puntos: los 72 que sobran se reparten 36 y 36, no 0 y 72.
func TestTicketCentradoEnElPapel(t *testing.T) {
	gsL := []byte{0x1d, 0x4c, 36, 0}
	gsW := []byte{0x1d, 0x57, byte(504 & 0xff), byte(504 >> 8)}

	ticket := buildTicketBytes("BOLETA", []string{"TOTAL"}, nil, false, true, "", nil, "80mm", 576)
	if !bytes.Contains(ticket, append(append([]byte{}, gsL...), gsW...)) {
		t.Fatalf("no centró el área de 504 puntos en 576")
	}
	init := escposInit()
	if !bytes.HasPrefix(ticket, append(append(append([]byte{}, init...), gsL...), gsW...)) {
		t.Fatalf("el área va justo después de inicializar, antes del logo y del título")
	}
	// Devuelve el área completa antes de cortar, para quien imprima después.
	reset := escposResetPrintArea(576)
	if i, cut := bytes.LastIndex(ticket, reset), bytes.LastIndex(ticket, escposCutPartial()); i < 0 || i > cut {
		t.Fatalf("no devolvió el área completa antes del corte")
	}

	// Epson TM-T88 (180 ppp, 512 puntos): 4 de margen.
	if area := escposPrintArea(512, 42); !bytes.Equal(area[:4], []byte{0x1d, 0x4c, 4, 0}) {
		t.Fatalf("TM-T88: %v", area)
	}
	// 58 mm (384 puntos, 32 columnas = 384): nada que centrar.
	if area := escposPrintArea(384, 32); area != nil {
		t.Fatalf("58 mm no debería tocar el margen: %v", area)
	}
	// Sin el ancho real, el ticket sale como antes.
	before := buildTicketBytes("BOLETA", []string{"TOTAL"}, nil, false, true, "", nil, "80mm", 0)
	if bytes.Contains(before, []byte{0x1d, 0x4c}) || bytes.Contains(before, []byte{0x1d, 0x57}) {
		t.Fatalf("sin ancho no debe mandar márgenes")
	}
}

func TestSoloTermicasDeRollo(t *testing.T) {
	cases := []struct{ horz, dpi, want int }{
		{576, 203, 576}, // la mayoría de 80 mm
		{512, 180, 512}, // Epson TM-T88
		{384, 203, 384}, // 58 mm
		{4960, 600, 0},  // una láser de oficina
		{576, 72, 0},    // driver genérico de texto
		{0, 0, 0},       // el driver no respondió
	}
	for _, c := range cases {
		if got := thermalPrintableDots(c.horz, c.dpi); got != c.want {
			t.Errorf("%d puntos a %d ppp: %d, esperaba %d", c.horz, c.dpi, got, c.want)
		}
	}
}
