package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

type PrinterInfo struct {
	Name        string `json:"name"`
	Default     bool   `json:"default"`
	WorkOffline bool   `json:"workOffline"`
	PortName    string `json:"portName"`
	DriverName  string `json:"driverName"`
}

func listPrinters() ([]PrinterInfo, error) {
	if runtime.GOOS != "windows" {
		return []PrinterInfo{}, nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	command := "Get-CimInstance Win32_Printer | Select-Object Name,Default,WorkOffline,PortName,DriverName | ConvertTo-Json -Compress"
	out, err := exec.CommandContext(ctx, "powershell", "-NoProfile", "-NonInteractive", "-Command", command).CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("powershell printer query failed: %w (%s)", err, strings.TrimSpace(string(out)))
	}

	trimmed := strings.TrimSpace(string(out))
	if trimmed == "" || trimmed == "null" {
		return []PrinterInfo{}, nil
	}

	var many []PrinterInfo
	if err := json.Unmarshal([]byte(trimmed), &many); err == nil {
		return many, nil
	}

	var one PrinterInfo
	if err := json.Unmarshal([]byte(trimmed), &one); err != nil {
		return nil, fmt.Errorf("invalid printer JSON response")
	}

	return []PrinterInfo{one}, nil
}

// tcpAddressForPrinter looks up the printer's Windows port name and returns
// an IP:port string if the port is a network TCP/IP port, or "" otherwise.
// Recognized formats: "IP_192.168.1.250", "192.168.1.250", "192.168.1.250:9100".
func tcpAddressForPrinter(printerName string) string {
	printers, err := listPrinters()
	if err != nil {
		return ""
	}
	for _, p := range printers {
		if !strings.EqualFold(p.Name, printerName) {
			continue
		}
		port := strings.TrimSpace(p.PortName)
		// "IP_x.x.x.x" — standard Windows TCP/IP port
		if strings.HasPrefix(port, "IP_") {
			return net.JoinHostPort(port[3:], "9100")
		}
		// Already "x.x.x.x" or "x.x.x.x:port"
		host, rawPort, err := net.SplitHostPort(port)
		if err != nil {
			// No port part — try bare IP
			if net.ParseIP(port) != nil {
				return net.JoinHostPort(port, "9100")
			}
			return ""
		}
		if _, err := strconv.Atoi(rawPort); err != nil {
			return ""
		}
		return net.JoinHostPort(host, rawPort)
	}
	return ""
}

// writeRawViaTCP sends raw bytes to a printer via a direct TCP socket (port 9100).
// This bypasses the Windows print spooler and works with any ESC/POS network printer.
func writeRawViaTCP(address string, data []byte) error {
	conn, err := net.DialTimeout("tcp", address, 5*time.Second)
	if err != nil {
		return fmt.Errorf("TCP connect to %s failed: %w", address, err)
	}
	defer conn.Close()
	conn.SetWriteDeadline(time.Now().Add(15 * time.Second))
	n, err := conn.Write(data)
	if err != nil {
		return fmt.Errorf("TCP write failed: %w", err)
	}
	if n != len(data) {
		return fmt.Errorf("TCP write incomplete: %d/%d bytes", n, len(data))
	}
	return nil
}

// dispatchRaw sends raw ESC/POS bytes to the named printer.
// It automatically uses TCP/IP for network printers and the Windows spooler for USB.
func dispatchRaw(printerName string, data []byte) error {
	if addr := tcpAddressForPrinter(printerName); addr != "" {
		return writeRawViaTCP(addr, data)
	}
	return writeRawToPrinter(printerName, data)
}

func sendTestPrint(printerName string) error {
	if runtime.GOOS != "windows" {
		return errors.New("test print is currently supported only on Windows")
	}

	printerName = strings.TrimSpace(printerName)
	if printerName == "" {
		return errors.New("printerName is required")
	}

	content := strings.Join([]string{
		"==============================",
		"CyberERP - Test Print",
		"If you can read this, agent connectivity is OK.",
		time.Now().Format(time.RFC3339),
		"==============================",
	}, "\r\n") + "\r\n\r\n"

	data := escposInit()
	data = append(data, []byte(content)...)
	data = append(data, escposCutPartial()...)

	return dispatchRaw(printerName, data)
}

func sendTicketPrint(printerName, title string, lines, footer []string, openDrawer, cutPaper bool) error {
	if runtime.GOOS != "windows" {
		return errors.New("ticket print is currently supported only on Windows")
	}

	printerName = strings.TrimSpace(printerName)
	if printerName == "" {
		return errors.New("printerName is required")
	}

	title = strings.TrimSpace(title)
	if title == "" {
		title = "CyberERP Ticket"
	}

	ticketLines := []string{
		"================================",
		title,
		time.Now().Format(time.RFC3339),
		"--------------------------------",
	}
	ticketLines = append(ticketLines, lines...)

	if len(footer) > 0 {
		ticketLines = append(ticketLines, "--------------------------------")
		ticketLines = append(ticketLines, footer...)
	}

	ticketLines = append(ticketLines, "================================")
	ticketLines = append(ticketLines, "")
	ticketLines = append(ticketLines, "")

	content := strings.Join(ticketLines, "\r\n")
	data := escposInit()
	data = append(data, escposAlignCenter()...)
	data = append(data, []byte(title+"\r\n")...)
	data = append(data, escposAlignLeft()...)
	data = append(data, []byte(content+"\r\n")...)

	if openDrawer {
		data = append(data, escposOpenDrawer()...)
	}
	if cutPaper {
		data = append(data, escposCutPartial()...)
	}

	return dispatchRaw(printerName, data)
}

func escposInit() []byte        { return []byte{0x1b, 0x40} }
func escposAlignLeft() []byte   { return []byte{0x1b, 0x61, 0x00} }
func escposAlignCenter() []byte { return []byte{0x1b, 0x61, 0x01} }
func escposCutPartial() []byte  { return []byte{0x1d, 0x56, 0x42, 0x00} }
func escposOpenDrawer() []byte  { return []byte{0x1b, 0x70, 0x00, 0x19, 0xfa} }

type docInfo1 struct {
	pDocName    *uint16
	pOutputFile *uint16
	pDatatype   *uint16
}

func writeRawToPrinter(printerName string, data []byte) error {
	if len(data) == 0 {
		return errors.New("empty print payload")
	}

	printerNamePtr, err := syscall.UTF16PtrFromString(printerName)
	if err != nil {
		return fmt.Errorf("invalid printer name: %w", err)
	}

	dll := syscall.NewLazyDLL("winspool.drv")
	openPrinter := dll.NewProc("OpenPrinterW")
	closePrinter := dll.NewProc("ClosePrinter")
	startDocPrinter := dll.NewProc("StartDocPrinterW")
	endDocPrinter := dll.NewProc("EndDocPrinter")
	startPagePrinter := dll.NewProc("StartPagePrinter")
	endPagePrinter := dll.NewProc("EndPagePrinter")
	writePrinter := dll.NewProc("WritePrinter")

	var handle uintptr
	r1, _, err := openPrinter.Call(uintptr(unsafe.Pointer(printerNamePtr)), uintptr(unsafe.Pointer(&handle)), 0)
	if r1 == 0 {
		return fmt.Errorf("OpenPrinterW failed: %w", err)
	}
	defer closePrinter.Call(handle)

	docName, _ := syscall.UTF16PtrFromString("CyberERP Ticket")
	rawType, _ := syscall.UTF16PtrFromString("RAW")
	docInfo := docInfo1{
		pDocName:  docName,
		pDatatype: rawType,
	}

	r1, _, err = startDocPrinter.Call(handle, 1, uintptr(unsafe.Pointer(&docInfo)))
	if r1 == 0 {
		return fmt.Errorf("StartDocPrinterW failed: %w", err)
	}
	defer endDocPrinter.Call(handle)

	r1, _, err = startPagePrinter.Call(handle)
	if r1 == 0 {
		return fmt.Errorf("StartPagePrinter failed: %w", err)
	}
	defer endPagePrinter.Call(handle)

	var written uint32
	r1, _, err = writePrinter.Call(
		handle,
		uintptr(unsafe.Pointer(&data[0])),
		uintptr(uint32(len(data))),
		uintptr(unsafe.Pointer(&written)),
	)
	if r1 == 0 {
		return fmt.Errorf("WritePrinter failed: %w", err)
	}
	if written != uint32(len(data)) {
		return fmt.Errorf("WritePrinter wrote %d/%d bytes", written, len(data))
	}

	return nil
}
