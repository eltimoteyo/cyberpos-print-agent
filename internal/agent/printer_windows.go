package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"runtime"
	"syscall"
	"strings"
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

	return writeRawToPrinter(printerName, data)
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

	return writeRawToPrinter(printerName, data)
}

func escposInit() []byte { return []byte{0x1b, 0x40} }
func escposAlignLeft() []byte { return []byte{0x1b, 0x61, 0x00} }
func escposAlignCenter() []byte { return []byte{0x1b, 0x61, 0x01} }
func escposCutPartial() []byte { return []byte{0x1d, 0x56, 0x42, 0x00} }
func escposOpenDrawer() []byte { return []byte{0x1b, 0x70, 0x00, 0x19, 0xfa} }

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
