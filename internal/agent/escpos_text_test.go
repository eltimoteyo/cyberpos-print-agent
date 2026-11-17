package agent

import (
	"bytes"
	"strings"
	"testing"
)

func TestTicketSpanishEncodingAndPaperWidth(t *testing.T) {
	want := []byte{160, 130, 161, 162, 163, 164, 165, 168, 173}
	if got := encodePrintText("áéíóúñÑ¿¡"); !bytes.Equal(got, want) {
		t.Fatalf("CP850: %v", got)
	}
	if bytes.Contains(encodePrintText("Texto\x1b\x00"), []byte{27}) {
		t.Fatal("el contenido introdujo una orden")
	}
	for _, tc := range []struct {
		paper string
		cols  int
	}{{"58mm", 32}, {"80mm", 42}} {
		rows := wrapPrintText(strings.Repeat("X", 97), tc.cols)
		if strings.Join(rows, "") != strings.Repeat("X", 97) {
			t.Fatal("se perdió texto")
		}
		for _, row := range rows {
			if len([]rune(row)) > tc.cols {
				t.Fatal("desbordamiento")
			}
		}
		ticket := buildTicketBytes("TITULO", []string{"áéíóú"}, nil, false, true, "", nil, tc.paper)
		if bytes.Count(ticket, []byte("TITULO")) != 1 {
			t.Fatal("título duplicado")
		}
		if !bytes.Contains(ticket, []byte(strings.Repeat("=", tc.cols))) {
			t.Fatal("no se respetó el papel")
		}
		if !bytes.HasSuffix(ticket, append([]byte{27, 100, 3}, escposCutPartial()...)) {
			t.Fatal("se corta sin avanzar el contenido")
		}
		if bytes.Contains(ticket, escposOpenDrawer()) {
			t.Fatal("cajón abierto sin solicitud")
		}
	}
}
