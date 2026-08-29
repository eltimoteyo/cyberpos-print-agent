//go:build windows

package main

import (
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
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
		fmt.Println("Uso: cybererp-print-agent.exe install --token TOKEN | --token-file PATH [opciones]")
		fmt.Println()
		fmt.Println("Opciones:")
		fs.PrintDefaults()
		fmt.Println()
		fmt.Println("Genera el token en: CyberERP > Configuración de Impresión > Tokens de Agente")
	}

	token := fs.String("token", "", "Token de autenticación del agente")
	tokenFile := fs.String("token-file", "", "Ruta a archivo que contiene el token (alternativa segura a --token)")
	gateway := fs.String("gateway-ws", "wss://api.createam.cloud/api/v1/print-agent/ws", "URL WebSocket del gateway")
	name := fs.String("name", "", "Nombre descriptivo para este agente (ej: Caja 1)")
	addr := fs.String("addr", "127.0.0.1:12345", "Dirección HTTP local del agente")

	_ = fs.Parse(args)

	resolvedToken := *token
	if resolvedToken == "" && *tokenFile != "" {
		data, err := os.ReadFile(*tokenFile)
		if err != nil {
			log.Fatalf("ERROR: no se pudo leer token-file: %v", err)
		}
		resolvedToken = strings.TrimSpace(string(data))
		_ = os.Remove(*tokenFile)
	}

	if resolvedToken == "" {
		fs.Usage()
		fmt.Println()
		log.Fatal("ERROR: --token o --token-file es requerido.")
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

	// 2. Conectar al Service Control Manager (necesario antes de detener el servicio)
	m, err := mgr.Connect()
	if err != nil {
		log.Fatalf("ERROR: no se pudo conectar al Service Control Manager.\n¿Ejecutaste como Administrador?\nDetalle: %v", err)
	}
	defer m.Disconnect()

	// 3. Detener y eliminar versión anterior ANTES de copiar el exe.
	//    El servicio en ejecución mantiene el archivo bloqueado; si copiamos primero
	//    obtenemos "Access is denied" al intentar renombrar el .tmp.
	if existing, err := m.OpenService(svcName); err == nil {
		fmt.Println("  [..] Deteniendo servicio anterior...")
		_, _ = existing.Control(svc.Stop)
		// Esperar hasta 20 s a que el proceso libere el archivo. Antes eran 10 y,
		// si no le daba tiempo, el servicio se borraba IGUAL: quedaba un proceso
		// huerfano sujetando el .exe y sin servicio que detener, asi que la
		// siguiente instalacion no encontraba nada que parar y moria con
		// "Access is denied" para siempre.
		stopped := false
		for i := 0; i < 40; i++ {
			time.Sleep(500 * time.Millisecond)
			status, qerr := existing.Query()
			if qerr != nil || status.State == svc.Stopped {
				stopped = true
				break
			}
		}
		_ = existing.Delete()
		existing.Close()
		time.Sleep(1 * time.Second)
		if stopped {
			fmt.Println("  [OK] Servicio anterior eliminado")
		} else {
			fmt.Println("  [!!] El servicio anterior no confirmo su parada; se continua igualmente")
		}
	}

	// 4. Copiar exe a Program Files (ahora que el servicio está detenido)
	exePath, err := os.Executable()
	if err != nil {
		log.Fatalf("ERROR: no se pudo resolver ruta del ejecutable: %v", err)
	}
	destExe := filepath.Join(installDir, "cybererp-print-agent.exe")
	cleanupOldExecutables(installDir)
	if err := copyFileSafe(exePath, destExe); err != nil {
		// Ultimo recurso: Windows NO deja sobrescribir un ejecutable en uso,
		// pero SI deja renombrarlo. Se aparta el viejo y se copia encima; el
		// apartado se borra en la siguiente instalacion, cuando ya nadie lo
		// tenga abierto.
		aside := fmt.Sprintf("%s.old-%d", destExe, time.Now().Unix())
		if rerr := os.Rename(destExe, aside); rerr == nil {
			err = copyFileSafe(exePath, destExe)
			if err == nil {
				fmt.Println("  [OK] Se aparto el ejecutable anterior, que seguia en uso")
				_ = os.Remove(aside)
			} else {
				_ = os.Rename(aside, destExe) // dejar las cosas como estaban
			}
		}
		if err != nil {
			log.Fatalf("ERROR: no se pudo copiar ejecutable a %s: %v\n"+
				"El agente anterior sigue abierto. Cierralo (Administrador de tareas > "+
				"cybererp-print-agent.exe) o reinicia el equipo, y vuelve a ejecutar este instalador.",
				destExe, err)
		}
	}
	fmt.Printf("  [OK] Ejecutable copiado a %s\n", destExe)

	// 5. Escribir agent.env con la configuración
	envContent := fmt.Sprintf(
		"PRINT_AGENT_TOKEN=%s\n"+
			"PRINT_AGENT_GATEWAY_WS_URL=%s\n"+
			"PRINT_AGENT_HOSTNAME=%s\n"+
			"PRINT_AGENT_ADDR=%s\n"+
			"PRINT_AGENT_DATA_DIR=%s\n"+
			"PRINT_AGENT_VERSION=%s\n",
		resolvedToken, *gateway, agentName, *addr, dataDir, Version,
	)
	envFile := filepath.Join(installDir, "agent.env")
	if err := os.WriteFile(envFile, []byte(envContent), 0o600); err != nil {
		log.Fatalf("ERROR: no se pudo escribir agent.env: %v", err)
	}
	fmt.Printf("  [OK] Configuración guardada en %s\n", envFile)

	// 6. Registrar el servicio
	// golang.org/x/sys/windows/svc/mgr already quotes paths with spaces internally;
	// passing a pre-quoted string causes double-quoting ("\"path\"") in the registry,
	// which breaks SCM parsing and returns ERROR_ACCESS_DENIED on StartService.
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

// cleanupOldExecutables borra los ejecutables apartados por instalaciones
// anteriores. Se hace al principio, cuando ya nadie los tiene abiertos; si
// alguno sigue en uso se ignora y se intentara la proxima vez.
func cleanupOldExecutables(dir string) {
	matches, err := filepath.Glob(filepath.Join(dir, "cybererp-print-agent.exe.old-*"))
	if err != nil {
		return
	}
	for _, old := range matches {
		_ = os.Remove(old)
	}
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

	// Escribir en archivo temporal primero, luego renombrar
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

	// Eliminar destino anterior e intentar renombrar con reintentos.
	// Windows puede mantener el handle del exe unos instantes tras detener
	// el servicio (antivirus, indexador, etc.).
	var lastErr error
	for attempt := 1; attempt <= 10; attempt++ {
		_ = os.Remove(dst)
		if lastErr = os.Rename(tmp, dst); lastErr == nil {
			return nil
		}
		time.Sleep(time.Duration(attempt) * 500 * time.Millisecond)
	}
	os.Remove(tmp)
	return fmt.Errorf("no se pudo reemplazar el ejecutable tras 10 intentos: %w", lastErr)
}
