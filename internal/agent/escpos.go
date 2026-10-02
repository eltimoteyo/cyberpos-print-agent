package agent

// Las órdenes ESC/POS que no dependen del sistema operativo.

func escposInit() []byte        { return []byte{0x1b, 0x40} }
func escposAlignLeft() []byte   { return []byte{0x1b, 0x61, 0x00} }
func escposAlignCenter() []byte { return []byte{0x1b, 0x61, 0x01} }
func escposCutPartial() []byte  { return []byte{0x1d, 0x56, 0x42, 0x00} }
func escposOpenDrawer() []byte  { return []byte{0x1b, 0x70, 0x00, 0x19, 0xfa} }
