package agent

import (
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
)

// El logo del negocio en el ticket.
//
// El agente NO convierte imágenes: recibe el logo ya hecho puntos —un bit por
// punto, o negro o papel— y lo manda a la impresora con la orden de imagen de
// ESC/POS. La conversión la hace el servidor, una sola vez y para todos los
// caminos por los que sale un ticket, de modo que el mismo negocio imprime el
// mismo logo desde cualquier caja.
//
// Quien manda el ticket comprueba antes si este agente declara "escpos_logo";
// uno anterior ignora el campo y el ticket sale como siempre.

// TicketLogo: el logo como mapa de puntos.
type TicketLogo struct {
	// Width en puntos; múltiplo de 8 (cada fila son bytes enteros).
	Width  int `json:"width"`
	Height int `json:"height"`
	// Data: un bit por punto, fila a fila, el bit alto primero (1 = negro), en
	// base64.
	Data string `json:"data"`
}

const (
	// ticketLogoMaxWidth: el papel más ancho que se usa en un punto de venta
	// (104 mm a 8 puntos/mm). Más que eso no es un logo de ticket.
	ticketLogoMaxWidth = 832
	// ticketLogoMaxHeight: 64 mm de papel. El servidor manda como mucho 15.
	ticketLogoMaxHeight = 512
)

// escposLogo arma las órdenes que dibujan el logo, centrado.
//
// Se comprueba TODO antes de mandar nada: la orden de imagen lleva el tamaño
// por delante, y si los datos no casan con él la impresora se come como imagen
// lo que venga después —el ticket entero sale hecho ruido—.
func escposLogo(logo *TicketLogo) ([]byte, error) {
	if logo == nil || strings.TrimSpace(logo.Data) == "" {
		return nil, nil
	}
	if logo.Width <= 0 || logo.Height <= 0 || logo.Width%8 != 0 {
		return nil, fmt.Errorf("tamaño de logo no válido: %d × %d", logo.Width, logo.Height)
	}
	if logo.Width > ticketLogoMaxWidth || logo.Height > ticketLogoMaxHeight {
		return nil, fmt.Errorf("el logo es demasiado grande para un ticket: %d × %d", logo.Width, logo.Height)
	}
	puntos, err := base64.StdEncoding.DecodeString(strings.TrimSpace(logo.Data))
	if err != nil {
		return nil, errors.New("los datos del logo no se pueden leer")
	}
	anchoBytes := logo.Width / 8
	if len(puntos) != anchoBytes*logo.Height {
		return nil, fmt.Errorf("el logo dice medir %d × %d y trae %d bytes", logo.Width, logo.Height, len(puntos))
	}

	// GS v 0: imagen de puntos. El ancho va en BYTES y el alto en puntos, cada
	// uno en dos bytes (bajo, alto).
	out := make([]byte, 0, len(puntos)+16)
	out = append(out, escposAlignCenter()...)
	out = append(out, 0x1d, 0x76, 0x30, 0x00,
		byte(anchoBytes&0xff), byte((anchoBytes>>8)&0xff),
		byte(logo.Height&0xff), byte((logo.Height>>8)&0xff))
	out = append(out, puntos...)
	out = append(out, '\r', '\n')
	out = append(out, escposAlignLeft()...)
	return out, nil
}
