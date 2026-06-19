//go:build !windows

package agent

import "errors"

type PrinterInfo struct {
	Name        string `json:"name"`
	Default     bool   `json:"default"`
	WorkOffline bool   `json:"workOffline"`
	PortName    string `json:"portName"`
	DriverName  string `json:"driverName"`
}

func listPrinters() ([]PrinterInfo, error) {
	return []PrinterInfo{}, nil
}

func sendTestPrint(printerName string) error {
	return errors.New("test print is currently supported only on Windows")
}

func sendTicketPrint(printerName, title string, lines, footer []string, openDrawer, cutPaper bool) error {
	return errors.New("ticket print is currently supported only on Windows")
}
