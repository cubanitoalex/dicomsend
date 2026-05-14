package main

// Icono del .exe en Windows: el enlazador de Go no usa logo.ico automáticamente; hay que
// generar rsrc_windows_$GOARCH.syso con rsrc antes de go build (ver compilar.bat).

import (
	"bufio"
	_ "embed" // Solución: Importación por efecto secundario para activar //go:embed
	"fmt"
	"image/color"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/ncruces/zenity"
)

//go:embed logo.png
var logoBytes []byte

var (
	colorBg      = color.NRGBA{R: 18, G: 22, B: 36, A: 255}
	colorPanel   = color.NRGBA{R: 26, G: 32, B: 53, A: 255}
	colorAccent  = color.NRGBA{R: 0, G: 180, B: 216, A: 255}
	colorSuccess = color.NRGBA{R: 0, G: 200, B: 120, A: 255}
	colorError   = color.NRGBA{R: 220, G: 50, B: 50, A: 255}
	colorText    = color.NRGBA{R: 220, G: 225, B: 240, A: 255}
	colorMuted   = color.NRGBA{R: 120, G: 130, B: 160, A: 255}
)

const adminPassword = "hcqho"

// ── LogBuffer ─────────────────────────────────────────────────────────────────

type LogBuffer struct {
	mu     sync.Mutex
	lines  []string
	label  *widget.Label
	scroll *container.Scroll
}

func (l *LogBuffer) Append(line string) {
	l.mu.Lock()
	l.lines = append(l.lines, line)
	text := strings.Join(l.lines, "\n")
	l.mu.Unlock()
	l.label.SetText(text)
	l.scroll.ScrollToBottom()
}

func (l *LogBuffer) Clear() {
	l.mu.Lock()
	l.lines = nil
	l.mu.Unlock()
	l.label.SetText("")
}

// ── password dialog ───────────────────────────────────────────────────────────

func askPassword(parent fyne.Window, onOK func()) {
	pwEntry := widget.NewPasswordEntry()
	errLabel := widget.NewLabel("")
	var dlg dialog.Dialog
	dlg = dialog.NewCustomConfirm("🔐 Acceso Restringido", "Confirmar", "Cancelar",
		container.NewVBox(
			widget.NewLabel("Contraseña para editar la configuración:"),
			pwEntry, errLabel,
		),
		func(ok bool) {
			if !ok {
				return
			}
			if pwEntry.Text == adminPassword {
				onOK()
			} else {
				errLabel.SetText("⚠ Contraseña incorrecta.")
				time.AfterFunc(1200*time.Millisecond, func() {
					pwEntry.SetText(""); errLabel.SetText(""); dlg.Show()
				})
			}
		}, parent)
	dlg.Show()
}

// ── dcmdump full parse ────────────────────────────────────────────────────────

var wantedTags = []struct{ tag, name string }{
	{"(0010,0010)", "PatientName"},
	{"(0010,0020)", "PatientID"},
	{"(0010,0030)", "PatientBirthDate"},
	{"(0010,0040)", "PatientSex"},
	{"(0010,1010)", "PatientAge"},
	{"(0008,0020)", "StudyDate"},
	{"(0008,0060)", "Modality"},
	{"(0008,0080)", "InstitutionName"},
	{"(0008,0050)", "AccessionNumber"},
	{"(0008,1030)", "StudyDescription"},
	{"(0008,103e)", "SeriesDescription"},
	{"(0008,1070)", "OperatorsName"},
}

func parseDCMDump(dcmdumpExe, file string) map[string]string {
	cmd := exec.Command(dcmdumpExe, file)
	out, err := cmd.Output()
	result := make(map[string]string)
	if err != nil {
		result["ERROR"] = err.Error()
		return result
	}
	for _, line := range strings.Split(string(out), "\n") {
		trimmed := strings.TrimSpace(line)
		for _, t := range wantedTags {
			if strings.HasPrefix(trimmed, t.tag) {
				s := strings.Index(trimmed, "[")
				e := strings.LastIndex(trimmed, "]")
				if s >= 0 && e > s {
					result[t.name] = strings.TrimSpace(trimmed[s+1 : e])
				} else if strings.Contains(trimmed, "(no value available)") {
					result[t.name] = "(sin valor)"
				}
				break
			}
		}
	}
	return result
}

func formatAnalysis(data map[string]string) string {
	if err, ok := data["ERROR"]; ok {
		return "✘ Error al leer el archivo:\n  " + err
	}
	order := []string{
		"PatientName", "PatientID", "PatientSex", "PatientAge", "PatientBirthDate",
		"StudyDate", "Modality", "InstitutionName", "AccessionNumber",
		"StudyDescription", "SeriesDescription", "OperatorsName",
	}
	labels := map[string]string{
		"PatientName": "Nombre Paciente", "PatientID": "ID Paciente",
		"PatientSex": "Sexo", "PatientAge": "Edad", "PatientBirthDate": "Fecha Nac.",
		"StudyDate": "Fecha Estudio", "Modality": "Modalidad",
		"InstitutionName": "Institución", "AccessionNumber": "Acceso",
		"StudyDescription": "Descripción Estudio", "SeriesDescription": "Descripción Serie",
		"OperatorsName": "Operador",
	}
	var sb strings.Builder
	sb.WriteString("════════════════════════════════\n")
	sb.WriteString("   DATOS DEL PACIENTE / ESTUDIO\n")
	sb.WriteString("════════════════════════════════\n")
	for _, k := range order {
		v, ok := data[k]
		if !ok || v == "" {
			continue
		}
		sb.WriteString(fmt.Sprintf("  %-20s: %s\n", labels[k], v))
	}
	return sb.String()
}

// ── main ─────────────────────────────────────────────────────────────────────

func main() {
	a := app.New()
	a.Settings().SetTheme(&darkTheme{})

	mainWin := buildMainWindow(a)

	// ── Splash ───────────────────────────────────────────────────────────
	splash := a.NewWindow("Hospital Lucía de Holguín")
	splash.SetFixedSize(true)
	splash.Resize(fyne.NewSize(500, 300))
	splash.CenterOnScreen()

	if len(logoBytes) > 0 {
		splash.SetIcon(fyne.NewStaticResource("logo.png", logoBytes))
	}

	topBar := canvas.NewRectangle(colorAccent)
	topBar.SetMinSize(fyne.NewSize(500, 5))
	h1 := canvas.NewText("Dpto. Informática", colorAccent)
	h1.TextSize = 22; h1.TextStyle = fyne.TextStyle{Bold: true}
	h2 := canvas.NewText("Hospital Clínico Quirúrgico Lucía Iñiguez Landín", colorText)
	h2.TextSize = 14
	by := canvas.NewText("Hecho por Lic. Alexis Parra González", colorMuted)
	by.TextSize = 12
	fo := canvas.NewText("Para el personal de salud", colorMuted)
	fo.TextSize = 11
	cd := canvas.NewText("Iniciando en 5 segundo(s)…", colorAccent)
	cd.TextSize = 12
	splashBar := widget.NewProgressBar()
	splashBar.Min = 0; splashBar.Max = 5

	splash.SetContent(container.NewPadded(container.NewVBox(
		topBar, widget.NewSeparator(),
		container.NewCenter(h1), container.NewCenter(h2),
		widget.NewSeparator(),
		container.NewCenter(by), container.NewCenter(fo),
		widget.NewSeparator(),
		container.NewCenter(cd), splashBar,
	)))
	splash.Show()

	go func() {
		for i := 5; i >= 1; i-- {
			cd.Text = fmt.Sprintf("Iniciando en %d segundo(s)…", i)
			cd.Refresh(); splashBar.SetValue(float64(6 - i))
			time.Sleep(time.Second)
		}
		mainWin.Show()
		splash.Close()
	}()

	a.Run()
}

// ── buildMainWindow ───────────────────────────────────────────────────────────

func buildMainWindow(a fyne.App) fyne.Window {
	w := a.NewWindow("DICOM Sender — Hospital Lucía de Holguín")
	w.Resize(fyne.NewSize(880, 780))
	w.CenterOnScreen()

	// Configuración del Logo
	logoRes := fyne.NewStaticResource("logo.png", logoBytes)
	w.SetIcon(logoRes)

	logoImg := canvas.NewImageFromResource(logoRes)
	logoImg.FillMode = canvas.ImageFillContain
	logoImg.SetMinSize(fyne.NewSize(48, 48))

	title := canvas.NewText("DICOM Sender  ·  DCM4CHEE", colorAccent)
	title.TextSize = 20; title.TextStyle = fyne.TextStyle{Bold: true}
	sub := canvas.NewText("Hospital Clínico Quirúrgico Lucía Iñiguez Landín", colorText)
	sub.TextSize = 13
	
	textHeader := container.NewVBox(title, sub)
	headerRow := container.NewHBox(logoImg, textHeader)
	hSep := canvas.NewRectangle(colorAccent); hSep.SetMinSize(fyne.NewSize(0, 2))
	header := container.NewVBox(container.NewCenter(headerRow), hSep)

	// Configuración de red del Servidor
	cfgIP, cfgPort, cfgSender, cfgReceiver := "192.168.1.3", "11112", "SENDER", "DCM4CHEE"
	locked := true
	lblIP := widget.NewLabel(cfgIP); lblPort := widget.NewLabel(cfgPort)
	lblSender := widget.NewLabel(cfgSender); lblReceiver := widget.NewLabel(cfgReceiver)
	edIP := widget.NewEntry(); edIP.SetText(cfgIP)
	edPort := widget.NewEntry(); edPort.SetText(cfgPort)
	edSender := widget.NewEntry(); edSender.SetText(cfgSender)
	edReceiver := widget.NewEntry(); edReceiver.SetText(cfgReceiver)
	edIP.Hide(); edPort.Hide(); edSender.Hide(); edReceiver.Hide()

	var lockBtn *widget.Button
	lockBtn = widget.NewButtonWithIcon("🔒 Editar Configuración", theme.SettingsIcon(), nil)
	lockBtn.Importance = widget.MediumImportance
	lockBtn.OnTapped = func() {
		if locked {
			askPassword(w, func() {
				locked = false
				lblIP.Hide(); lblPort.Hide(); lblSender.Hide(); lblReceiver.Hide()
				edIP.Show(); edPort.Show(); edSender.Show(); edReceiver.Show()
				lockBtn.SetText("🔓 Guardar y Bloquear"); lockBtn.Refresh()
			})
		} else {
			cfgIP = strings.TrimSpace(edIP.Text); cfgPort = strings.TrimSpace(edPort.Text)
			cfgSender = strings.TrimSpace(edSender.Text); cfgReceiver = strings.TrimSpace(edReceiver.Text)
			lblIP.SetText(cfgIP); lblPort.SetText(cfgPort)
			lblSender.SetText(cfgSender); lblReceiver.SetText(cfgReceiver)
			edIP.Hide(); edPort.Hide(); edSender.Hide(); edReceiver.Hide()
			lblIP.Show(); lblPort.Show(); lblSender.Show(); lblReceiver.Show()
			locked = true; lockBtn.SetText("🔒 Editar Configuración"); lockBtn.Refresh()
		}
	}

	// Origen de Carpeta de Imágenes
	exeDir, _ := filepath.Abs(filepath.Dir(os.Args[0]))
	imagesPath := filepath.Join(exeDir, "IMAGES")
	imagesLabel := widget.NewLabel(imagesPath)
	imagesLabel.Wrapping = fyne.TextWrapBreak

	btnFolder := widget.NewButtonWithIcon("📁 Examinar…", theme.FolderOpenIcon(), func() {
		go func() {
			dir, err := zenity.SelectFile(
				zenity.Title("Seleccionar Carpeta de Imágenes"),
				zenity.Directory(),
				zenity.Filename(imagesPath),
			)
			if err == nil && dir != "" {
				imagesPath = dir
				imagesLabel.SetText(imagesPath)
			}
		}()
	})
	btnFolder.Importance = widget.LowImportance

	form := widget.NewForm(
		widget.NewFormItem("IP del Servidor", container.NewStack(lblIP, edIP)),
		widget.NewFormItem("Puerto", container.NewStack(lblPort, edPort)),
		widget.NewFormItem("AET Remitente", container.NewStack(lblSender, edSender)),
		widget.NewFormItem("AET Receptor", container.NewStack(lblReceiver, edReceiver)),
		widget.NewFormItem("Carpeta Origen", container.NewBorder(nil, nil, nil, btnFolder, imagesLabel)),
	)

	pacsCard := widget.NewCard("🌐 Conectividad PACS", "Defina los parámetros de red y el origen de los ficheros", container.NewVBox(form, container.NewCenter(lockBtn)))

	// Componentes de Progreso de Envío
	progressBar := widget.NewProgressBar()
	progressBar.Min = 0; progressBar.Max = 1; progressBar.Hide()
	progressLabel := canvas.NewText("", colorAccent)
	progressLabel.TextSize = 11; progressLabel.Hide()
	progressBox := container.NewVBox(progressBar, container.NewCenter(progressLabel))

	// Consola de Procesos
	logLabel := widget.NewLabel("")
	logLabel.Wrapping = fyne.TextWrapWord
	scroll := container.NewVScroll(logLabel)
	scroll.SetMinSize(fyne.NewSize(0, 220))
	logBuf := &LogBuffer{label: logLabel, scroll: scroll}
	logTitle := canvas.NewText("▸ Consola de ejecución en tiempo real", colorAccent)
	logTitle.TextSize = 12; logTitle.TextStyle = fyne.TextStyle{Bold: true}
	logPanel := container.NewBorder(logTitle, nil, nil, nil, scroll)

	statusLabel := canvas.NewText("Listo.", colorText)
	statusLabel.TextSize = 12
	setStatus := func(msg string, col color.NRGBA) {
		statusLabel.Text = msg; statusLabel.Color = col; statusLabel.Refresh()
	}

	btnStart := widget.NewButtonWithIcon("  Iniciar Transmisión Masiva", theme.MediaPlayIcon(), nil)
	btnStart.OnTapped = func() {
		ip := cfgIP; port := cfgPort; sender := cfgSender; receiver := cfgReceiver
		if !locked {
			ip = strings.TrimSpace(edIP.Text); port = strings.TrimSpace(edPort.Text)
			sender = strings.TrimSpace(edSender.Text); receiver = strings.TrimSpace(edReceiver.Text)
		}
		if ip == "" || port == "" || sender == "" || receiver == "" || imagesPath == "" {
			setStatus("⚠ Rellena todos los campos.", colorError); return
		}
		btnStart.Disable()
		logBuf.Clear(); progressBar.SetValue(0); progressBar.Show(); progressLabel.Show()
		setStatus("⏳ Procesando…", colorAccent)
		go func() {
			defer func() { btnStart.Enable(); progressBar.Hide(); progressLabel.Hide() }()
			if runProcess(logBuf, progressBar, progressLabel, ip, port, sender, receiver, imagesPath) {
				setStatus("✔ Proceso completado con éxito.", colorSuccess)
			} else {
				setStatus("✘ Error. Revisa la consola.", colorError)
			}
		}()
	}
	btnStart.Importance = widget.HighImportance

	panelEnvio := container.NewVBox(
		pacsCard,
		widget.NewSeparator(),
		container.NewCenter(btnStart), container.NewCenter(statusLabel),
		progressBox,
		widget.NewSeparator(),
		logPanel,
	)

	// ── COMPONENTES DE PESTAÑA 2 (ANALIZADOR) ─────────────────────────────────
	dcmFilePath := ""
	dcmFileLabel := widget.NewLabel("(ningún archivo DICOM seleccionado)")
	dcmFileLabel.Wrapping = fyne.TextWrapBreak

	analysisLabel := widget.NewLabel("")
	analysisLabel.Wrapping = fyne.TextWrapWord
	analysisScroll := container.NewVScroll(analysisLabel)
	analysisScroll.SetMinSize(fyne.NewSize(0, 320))
	analysisScroll.Hide()

	btnExaminarDCM := widget.NewButtonWithIcon("📂 Examinar Fichero…", theme.FileIcon(), func() {
		go func() {
			file, err := zenity.SelectFile(
				zenity.Title("Seleccionar Archivo DICOM"),
				zenity.FileFilter{
					Name:     "Archivos DICOM (*.dcm)",
					Patterns: []string{"*.dcm"},
				},
				zenity.FileFilter{
					Name:     "Todos los archivos (*.*)",
					Patterns: []string{"*.*"},
				},
				zenity.Filename(imagesPath),
			)
			if err == nil && file != "" {
				dcmFilePath = file
				dcmFileLabel.SetText(dcmFilePath)
			}
		}()
	})
	btnExaminarDCM.Importance = widget.LowImportance

	var btnAnalizar *widget.Button
	btnAnalizar = widget.NewButtonWithIcon("🔬 Analizar Cabecera Meta-Data", theme.SearchIcon(), func() {
		if dcmFilePath == "" {
			dialog.ShowInformation("Aviso", "Seleccione un archivo DICOM primero.", w)
			return
		}
		btnAnalizar.Disable()
		go func() {
			defer btnAnalizar.Enable()
			binDir, err := ensureEmbeddedBinDir()
			if err != nil {
				analysisLabel.SetText("✘ No se pudieron preparar las herramientas embebidas:\n  " + err.Error())
				analysisScroll.Show()
				return
			}
			dcmdump := filepath.Join(binDir, "dcmdump.exe")
			data := parseDCMDump(dcmdump, dcmFilePath)
			result := formatAnalysis(data)
			analysisLabel.SetText(result)
			analysisScroll.Show()
		}()
	})
	btnAnalizar.Importance = widget.HighImportance

	analyzeForm := widget.NewForm(
		widget.NewFormItem("Archivo Seleccionado", container.NewBorder(nil, nil, nil, btnExaminarDCM, dcmFileLabel)),
	)
	
	analizadorCard := widget.NewCard("🔬 Lector de Tags DICOM (dcmdump)", "Inspeccione los metadatos estructurados del archivo antes o después de la transmisión", 
		container.NewVBox(analyzeForm, container.NewCenter(btnAnalizar)),
	)

	panelAnalisis := container.NewVBox(
		analizadorCard,
		widget.NewSeparator(),
		analysisScroll,
	)

	// ── TABS SYSTEM ──────────────────────────────────────────────────────────
	tabs := container.NewAppTabs(
		container.NewTabItem("🚀 Transmisión PACS", panelEnvio),
		container.NewTabItem("🔬 Analizador de Cabeceras", panelAnalisis),
	)
	tabs.SetTabLocation(container.TabLocationTop)

	// Global Footer
	fSep := canvas.NewRectangle(colorAccent); fSep.SetMinSize(fyne.NewSize(0, 1))
	fTxt := canvas.NewText("Admin: Alexis Parra González  ·  Hospital Lucía de Holguín  ·  alexishlg@infomed.sld.cu", colorMuted)
	fTxt.TextSize = 10
	footer := container.NewVBox(fSep, container.NewCenter(fTxt))

	mainContent := container.NewBorder(header, footer, nil, nil, tabs)

	w.SetContent(container.NewPadded(mainContent))
	return w
}

// ── runProcess ────────────────────────────────────────────────────────────────

func runProcess(log *LogBuffer, bar *widget.ProgressBar, barLabel *canvas.Text, ip, port, sender, receiver, imagesDir string) bool {
	log.Append("═══════════════════════════════════════")
	log.Append("      INICIO DEL PROCESO DICOM")
	log.Append("═══════════════════════════════════════")

	exeDir, err := filepath.Abs(filepath.Dir(os.Args[0]))
	if err != nil {
		exeDir = "."
	}
	binDir, err := ensureEmbeddedBinDir()
	if err != nil {
		log.Append("  ✘ No se pudieron preparar las herramientas embebidas: " + err.Error())
		return false
	}
	dcmodify := filepath.Join(binDir, "dcmodify.exe")
	dcmsend := filepath.Join(binDir, "dcmsend.exe")
	absImages := imagesDir
	if !filepath.IsAbs(imagesDir) {
		absImages = filepath.Join(exeDir, imagesDir)
	}

	// Phase 1: Fix UIDs
	log.Append("\n[1/3] ─ Corrigiendo UIDs (dcmodify -nb -gin)…")
	dcmFiles, _ := filepath.Glob(filepath.Join(absImages, "*.dcm"))
	if len(dcmFiles) == 0 {
		log.Append("  ⚠  No se encontraron archivos .dcm en: " + absImages)
	} else {
		args := append([]string{"-nb", "-gin"}, dcmFiles...)
		if e := runCmd(log, dcmodify, args...); e != nil {
			log.Append("  ✘ dcmodify falló: " + e.Error()); return false
		}
		log.Append(fmt.Sprintf("  ✔ %d archivo(s) corregidos.", len(dcmFiles)))
	}

	// Phase 2: Remove .bak
	log.Append("\n[2/3] ─ Eliminando archivos .bak…")
	bakFiles, _ := filepath.Glob(filepath.Join(absImages, "*.bak"))
	if len(bakFiles) == 0 {
		log.Append("  ✔ Sin archivos .bak.")
	} else {
		for _, bak := range bakFiles {
			if e := os.Remove(bak); e != nil {
				log.Append("  ⚠ " + filepath.Base(bak) + ": " + e.Error())
			} else {
				log.Append("  ✔ Eliminado: " + filepath.Base(bak))
			}
		}
	}

	// Phase 3: Send
	log.Append(fmt.Sprintf("\n[3/3] ─ Enviando a %s (%s:%s) como [%s]…", receiver, ip, port, sender))
	allDcm := collectDCM(absImages)
	total := len(allDcm)
	if total == 0 {
		total = 1
	}
	barLabel.Text = fmt.Sprintf("Preparando %d archivo(s)…", len(allDcm)); barLabel.Refresh()

	if e := runCmdProgress(log, bar, barLabel, total, dcmsend, "-v",
		"-aet", sender, "-aec", receiver, ip, port,
		"--scan-directories", "--recurse", absImages); e != nil {
		log.Append("\n  ✘ dcmsend error: " + e.Error()); return false
	}
	bar.SetValue(1)
	barLabel.Text = fmt.Sprintf("✔ %d archivo(s) enviados.", len(allDcm)); barLabel.Refresh()

	log.Append("\n═══════════════════════════════════════")
	log.Append("      ENVÍO COMPLETADO CON ÉXITO ✔")
	log.Append("═══════════════════════════════════════")
	return true
}

// ── helpers ───────────────────────────────────────────────────────────────────

func collectDCM(root string) []string {
	var files []string
	_ = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() && strings.EqualFold(filepath.Ext(path), ".dcm") {
			files = append(files, path)
		}
		return nil
	})
	return files
}

func runCmd(log *LogBuffer, name string, args ...string) error {
	log.Append("  $ " + filepath.Base(name) + " " + strings.Join(args, " "))
	cmd := exec.Command(name, args...)
	out, err := cmd.CombinedOutput()
	for _, line := range strings.Split(strings.TrimRight(string(out), "\r\n"), "\n") {
		log.Append("    " + strings.TrimRight(line, "\r"))
	}
	return err
}

func runCmdProgress(log *LogBuffer, bar *widget.ProgressBar, barLabel *canvas.Text, total int, name string, args ...string) error {
	log.Append("  $ " + filepath.Base(name) + " " + strings.Join(args, " "))
	cmd := exec.Command(name, args...)
	pr, pw := io.Pipe()
	cmd.Stdout = pw
	cmd.Stderr = pw
	if err := cmd.Start(); err != nil {
		pw.Close(); pr.Close(); return err
	}
	var waitErr error
	done := make(chan struct{})
	go func() { waitErr = cmd.Wait(); pw.Close(); close(done) }()
	sent := 0
	scanner := bufio.NewScanner(pr)
	for scanner.Scan() {
		line := scanner.Text()
		log.Append("    " + line)
		lower := strings.ToLower(line)
		if strings.Contains(lower, "sending file") || strings.Contains(lower, "store response") {
			sent++
			frac := float64(sent) / float64(total)
			if frac > 1 {
				frac = 1
			}
			bar.SetValue(frac)
			barLabel.Text = fmt.Sprintf("Enviando… %d / %d  (%.0f%%)", sent, total, frac*100)
			barLabel.Refresh()
		}
	}
	pr.Close(); <-done
	return waitErr
}

// ── dark theme ────────────────────────────────────────────────────────────────

type darkTheme struct{}

func (d *darkTheme) Color(n fyne.ThemeColorName, v fyne.ThemeVariant) color.Color {
	switch n {
	case theme.ColorNameBackground:
		return colorBg
	case theme.ColorNameButton:
		return colorAccent
	case theme.ColorNameForeground:
		return colorText
	case theme.ColorNamePrimary:
		return colorAccent
	case theme.ColorNameInputBackground:
		return colorPanel
	case theme.ColorNameSeparator:
		return color.NRGBA{R: 40, G: 50, B: 80, A: 255}
	}
	return theme.DefaultTheme().Color(n, v)
}
func (d *darkTheme) Font(s fyne.TextStyle) fyne.Resource     { return theme.DefaultTheme().Font(s) }
func (d *darkTheme) Icon(n fyne.ThemeIconName) fyne.Resource { return theme.DefaultTheme().Icon(n) }
func (d *darkTheme) Size(n fyne.ThemeSizeName) float32       { return theme.DefaultTheme().Size(n) }