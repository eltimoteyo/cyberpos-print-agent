package agent

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"testing"
)

func logoDePuntos(ancho, alto int) *TicketLogo {
	puntos := bytes.Repeat([]byte{0xAA}, ancho/8*alto)
	return &TicketLogo{Width: ancho, Height: alto, Data: base64.StdEncoding.EncodeToString(puntos)}
}

// El logo sale con la orden de imagen de ESC/POS, centrado, y con el tamaño
// por delante tal como lo espera la impresora: ancho en BYTES y alto en puntos,
// cada uno en dos bytes (bajo, alto).
func TestElLogoSaleComoImagenDePuntos(t *testing.T) {
	out, err := escposLogo(logoDePuntos(344, 115))
	if err != nil {
		t.Fatal(err)
	}
	cabecera := []byte{0x1b, 0x61, 0x01, 0x1d, 0x76, 0x30, 0x00, 43, 0, 115, 0}
	if !bytes.HasPrefix(out, cabecera) {
		t.Fatalf("cabecera de la imagen: % x", out[:len(cabecera)])
	}
	puntos := out[len(cabecera) : len(cabecera)+43*115]
	if !bytes.Equal(puntos, bytes.Repeat([]byte{0xAA}, 43*115)) {
		t.Fatal("los puntos no viajan tal cual")
	}
	// Después del logo, el ticket sigue alineado a la izquierda.
	if !bytes.HasSuffix(out, []byte{'\r', '\n', 0x1b, 0x61, 0x00}) {
		t.Fatalf("tras el logo hay que volver a la izquierda: % x", out[len(out)-5:])
	}

	// Un alto de más de 255 puntos usa el segundo byte.
	out, err = escposLogo(logoDePuntos(8, 300))
	if err != nil {
		t.Fatal(err)
	}
	if out[9] != byte(300&0xff) || out[10] != byte(300>>8) {
		t.Fatalf("alto en dos bytes: % x", out[7:11])
	}
}

// Sin logo no se manda nada, y no es un error: el ticket sale como siempre.
func TestSinLogoNoSeMandaNada(t *testing.T) {
	for nombre, logo := range map[string]*TicketLogo{"nulo": nil, "vacío": {}, "sin datos": {Width: 8, Height: 1}} {
		out, err := escposLogo(logo)
		if err != nil || out != nil {
			t.Fatalf("%s: %v, % x", nombre, err, out)
		}
	}
}

// Un logo mal formado NO se manda. La orden de imagen lleva el tamaño por
// delante: si los datos no casan, la impresora se come como imagen el ticket
// que viene después.
func TestUnLogoMalFormadoNoSeManda(t *testing.T) {
	bueno := logoDePuntos(16, 4)
	malos := map[string]*TicketLogo{
		"ancho que no es de bytes enteros": {Width: 10, Height: 4, Data: bueno.Data},
		"faltan datos":                     {Width: 16, Height: 5, Data: bueno.Data},
		"sobran datos":                     {Width: 16, Height: 3, Data: bueno.Data},
		"no es base64":                     {Width: 16, Height: 4, Data: "###"},
		"tamaño negativo":                  {Width: -8, Height: 4, Data: bueno.Data},
		"demasiado ancho":                  logoDePuntos(ticketLogoMaxWidth+8, 1),
		"demasiado alto":                   logoDePuntos(8, ticketLogoMaxHeight+1),
	}
	for nombre, logo := range malos {
		if out, err := escposLogo(logo); err == nil || out != nil {
			t.Errorf("%s: debió rechazarse", nombre)
		}
	}
}

// El logo llega igual por los dos caminos: el ticket por HTTP local y el
// trabajo por WebSocket usan el mismo campo.
func TestElLogoLlegaPorHTTPYPorWebSocket(t *testing.T) {
	const cuerpo = `{"logo":{"width":16,"height":2,"data":"qqqqqg=="}}`
	var porHTTP printTicketRequest
	if err := json.Unmarshal([]byte(cuerpo), &porHTTP); err != nil {
		t.Fatal(err)
	}
	var porWS WSJobPayload
	if err := json.Unmarshal([]byte(cuerpo), &porWS); err != nil {
		t.Fatal(err)
	}
	for nombre, logo := range map[string]*TicketLogo{"HTTP": porHTTP.Logo, "WebSocket": porWS.Logo} {
		if logo == nil || logo.Width != 16 || logo.Height != 2 {
			t.Fatalf("%s: el logo no se leyó: %+v", nombre, logo)
		}
		if _, err := escposLogo(logo); err != nil {
			t.Fatalf("%s: %v", nombre, err)
		}
	}
	// Un ticket sin logo —el de un servidor anterior— se lee igual.
	var sin printTicketRequest
	if err := json.Unmarshal([]byte(`{"title":"Ticket"}`), &sin); err != nil || sin.Logo != nil {
		t.Fatalf("sin logo: %v %+v", err, sin.Logo)
	}
}
