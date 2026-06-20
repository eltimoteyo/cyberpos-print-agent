//go:build windows

package main

import (
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"time"

	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/eventlog"
	"golang.org/x/sys/windows/svc/mgr"
)

const (
	svcName    = "CyberERPPrintAgent"
	svcDisplay = "CyberERP Print Agent"
	svcDesc    = "Agente local de impresión para CyberERP POS. Comunica la app con la impresora térmica."
	installDir = `C:\Program Files\CyberERP\PrintAgent`
	dataDir    = `C:\ProgramData\CyberERP\PrintAgent`
)

func selfInstall(args []string) {
	fs := flag.NewFlagSet("install", flag.ExitOnError)
	fs.Usage = func() {
		fmt.Println("Uso: cybererp-print-agent.exe install --token TOKEN [opciones]")
		fmt.Println()
		fmt.Println("Opciones:")
		fs.PrintDefaults()
		fmt.Println()
		fmt.Println("Genera el token en: CyberERP > Configuración de Impresión > Tokens de Agente")
	}

	token := fs.String("token", "", "Token de autenticación del agente (requerido)")
	gateway := fs.String("gateway-ws", "wss://api.createam.cloud/api/v1/print-agent/ws", "URL WebSocket del gateway")
	name := fs.String("name", "", "Nombre descriptivo para este agente (ej: Caja 1)")
	addr := fs.String("addr", "127.0.0.1:12345", "Dirección HTTP local del agente")

	_ = fs.Parse(args)

	if *token == "" {
		fs.Usage()
		fmt.Println()
		log.Fatal("ERROR: --token es requerido.")
	}

	agentName := *name
	if agentName == "" {
		hostname, _ := os.Hostname()
		if hostname == "" {
			hostname = "agente"
		}
		agentName = hostname
	}

	fmt.Println("Instalando CyberERP Print Agent...")
	fmt.Println()

	// 1. Crear directorios
	for _, dir := range []string{installDir, dataDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			log.Fatalf("ERROR: no se pudo crear directorio %s: %v", dir, err)
		}
	}
	fmt.Printf("  [OK] Directorios listos\n")

	// 2. Copiar exe a Program Files
	exePath, err := os.Executable()
	if err != nil {
		log.Fatalf("ERROR: no se pudo resolver ruta del ejecutable: %v", err)
	}
	destExe := filepath.Join(installDir, "cybererp-print-agent.exe")
	if err := copyFileSafe(exePath, destExe); err != nil {
		log.Fatalf("ERROR: no se pudo copiar ejecutable a %s: %v", destExe, err)
	}
	fmt.Printf("  [OK] Ejecutable copiado a %s\n", destExe)

	// 3. Escribir agent.env con la configuración
	envContent := fmt.Sprintf(
		"PRINT_AGENT_TOKEN=%s\n"+
			"PRINT_AGENT_GATEWAY_WS_URL=%s\n"+
			"PRINT_AGENT_HOSTNAME=%s\n"+
			"PRINT_AGENT_ADDR=%s\n"+
			"PRINT_AGENT_DATA_DIR=%s\n",
		*token, *gateway, agentName, *addr, dataDir,
	)
	envFile := filepath.Join(installDir, "agent.env")
	if err := os.WriteFile(envFile, []byte(envContent), 0o600); err != nil {
		log.Fatalf("ERROR: no se pudo escribir agent.env: %v", err)
	}
	fmt.Printf("  [OK] Configuración guardada en %s\n", envFile)

	// 4. Conectar al Service Control Manager
	m, err := mgr.Connect()
	if err != nil {
		log.Fatalf("ERROR: no se pudo conectar al Service Control Manager.\n¿Ejecutaste como Administrador?\nDetalle: %v", err)
	}
	defer m.Disconnect()

	// 5. Desinstalar versión anterior si existe
	if existing, err := m.OpenService(svcName); err == nil {
		fmt.Println("  [..] Deteniendo servicio anterior...")
		_, _ = existing.Control(svc.Stop)
		time.Sleep(2 * time.Second)
		_ = existing.Delete()
		existing.Close()
		fmt.Println("  [OK] Servicio anterior eliminado")
	}

	// 6. Registrar el servicio
	s, err := m.CreateService(
		svcName,
		destExe,
		mgr.Config{
			StartType:        mgr.StartAutomatic,
			DisplayName:      svcDisplay,
			Description:      svcDesc,
			DelayedAutoStart: true,
		},
	)
	if err != nil {
		log.Fatalf("ERROR: no se pudo registrar el servicio: %v", err)
	}
	defer s.Close()

	// 7. Configurar reinicio automático ante fallos (3 intentos)
	_ = s.SetRecoveryActions([]mgr.RecoveryAction{
		{Type: mgr.ServiceRestart, Delay: 5 * time.Second},
		{Type: mgr.ServiceRestart, Delay: 10 * time.Second},
		{Type: mgr.ServiceRestart, Delay: 30 * time.Second},
	}, 3600)

	// 8. Registrar fuente de eventos
	_ = eventlog.InstallAsEventCreate(svcName, eventlog.Error|eventlog.Warning|eventlog.Info)

	// 9. Iniciar el servicio
	if err := s.Start(); err != nil {
		log.Fatalf("ERROR: servicio registrado pero no se pudo iniciar: %v", err)
	}
	fmt.Println("  [OK] Servicio iniciado")
	fmt.Println()
	fmt.Printf("Instalacion completada.\n")
	fmt.Printf("  Servicio:  %s\n", svcDisplay)
	fmt.Printf("  Escucha:   http://%s\n", *addr)
	fmt.Printf("  Gateway:   %s\n", *gateway)
	fmt.Printf("  Agente:    %s\n", agentName)
	fmt.Println()
	fmt.Println("Puedes verificar el estado con: sc query CyberERPPrintAgent")
}

func selfUninstall() {
	m, err := mgr.Connect()
	if err != nil {
		log.Fatalf("ERROR: %v\n¿Ejecutaste como Administrador?", err)
	}
	defer m.Disconnect()

	s, err := m.OpenService(svcName)
	if err != nil {
		fmt.Printf("El servicio '%s' no está instalado.\n", svcName)
		return
	}

	fmt.Println("Desinstalando CyberERP Print Agent...")
	_, _ = s.Control(svc.Stop)
	time.Sleep(2 * time.Second)
	_ = s.Delete()
	s.Close()
	_ = eventlog.Remove(svcName)

	fmt.Printf("  [OK] Servicio '%s' eliminado.\n", svcName)
}

func copyFileSafe(src, dst string) error {
	// Si el destino es el mismo exe en ejecución, no hacer nada
	srcAbs, _ := filepath.Abs(src)
	dstAbs, _ := filepath.Abs(dst)
	if srcAbs == dstAbs {
		return nil
	}

	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	// Escribir en archivo temporal primero, luego renombrar (atómico)
	tmp := dst + ".tmp"
	out, err := os.Create(tmp)
	if err != nil {
		return err
	}

	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		os.Remove(tmp)
		return err
	}
	out.Close()

	// Eliminar destino anterior si existe y renombrar
	_ = os.Remove(dst)
	return os.Rename(tmp, dst)
}
