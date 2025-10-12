package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// Structure pour les fichiers GitHub
type GitHubFile struct {
	Name        string `json:"name"`
	Type        string `json:"type"`
	DownloadURL string `json:"download_url"`
}

// Message contenant la liste des fichiers
type filesLoadedMsg struct {
	files []GitHubFile
	err   error
}

// Message pour l'animation du spinner
type tickMsg time.Time

// Message de téléchargement/suppression
type operationCompleteMsg struct {
	filename  string
	operation string // "download" ou "delete"
	err       error
}

type allOperationsCompleteMsg struct{}

// Message pour la sortie du TUI
type tuiOutputMsg struct {
	line string
}

type tuiStoppedMsg struct {
	filename string
}

// Template pour la barre de statut
type statusBar struct {
	message  string
	commands string
}

func (s statusBar) render() string {
	if s.message != "" && s.commands != "" {
		return fmt.Sprintf("%s | %s", s.message, s.commands)
	} else if s.message != "" {
		return s.message
	}
	return s.commands
}

// Le modèle contient l'état de l'application
type model struct {
	width        int
	height       int
	files        []GitHubFile
	loading      bool
	processing   bool
	err          error
	spinnerFrame int
	cursor       int
	statusMsg    string
	localFiles   map[string]bool
	selected     map[int]bool // Fichiers sélectionnés pour téléchargement/suppression
	cmdTemplate  string       // Template du status
	activePanel  int          // 0 = haut, 1 = bas, 2 = droite
	logs         []string     // Historique des logs
	tuiOutput    []string     // Sortie du TUI en cours d'exécution
	runningTUI   string       // Nom du fichier TUI en cours d'exécution
	tuiCmd       *exec.Cmd    // Commande en cours d'exécution
	tuiMutex     sync.Mutex   // Mutex pour l'accès concurrent
	scrollOffset int          // Offset pour le scroll du contenu
	stopTUIChan  chan bool    // Canal pour arrêter le TUI
}

var spinnerFrames = []string{"|", "/", "-", "\\"}

// Initialisation
func initialModel() model {
	return model{
		loading:      true,
		processing:   false,
		files:        []GitHubFile{},
		spinnerFrame: 0,
		cursor:       0,
		localFiles:   make(map[string]bool),
		selected:     make(map[int]bool),
		cmdTemplate:  "↑/↓: Navigation | Tab: Changer panel | Espace: Sélectionner | Enter: Exécuter TUI | d: Télécharger/Supprimer | c: Annuler | s: Arrêter TUI | q: Quitter",
		activePanel:  0,
		logs:         []string{},
		tuiOutput:    []string{},
		runningTUI:   "",
		tuiCmd:       nil,
		scrollOffset: 0,
		stopTUIChan:  make(chan bool, 1),
	}
}

// Ajouter un log avec timestamp
func (m *model) addLog(message string) {
	timestamp := time.Now().Format("15:04:05")
	logEntry := fmt.Sprintf("[%s] %s", timestamp, message)
	m.logs = append(m.logs, logEntry)
}

func (m model) Init() tea.Cmd {
	return tea.Batch(fetchFiles, tickCmd())
}

// Commande pour le tick du spinner
func tickCmd() tea.Cmd {
	return tea.Tick(time.Millisecond*100, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

// Commande pour récupérer les fichiers
func fetchFiles() tea.Msg {
	url := "https://api.github.com/repos/TWilhem/Plugin/contents/Plugin"

	resp, err := http.Get(url)
	if err != nil {
		return filesLoadedMsg{files: nil, err: err}
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return filesLoadedMsg{files: nil, err: fmt.Errorf("HTTP %d", resp.StatusCode)}
	}

	var gitHubFiles []GitHubFile
	if err := json.NewDecoder(resp.Body).Decode(&gitHubFiles); err != nil {
		return filesLoadedMsg{files: nil, err: err}
	}

	return filesLoadedMsg{files: gitHubFiles, err: nil}
}

// Commande pour exécuter un TUI
func runTUI(filename string, stopChan chan bool) tea.Cmd {
	return func() tea.Msg {
		// Vérifier si le fichier est exécutable
		fileInfo, err := os.Stat(filename)
		if err != nil {
			return tuiOutputMsg{line: fmt.Sprintf("❌ Erreur: %v", err)}
		}

		// Rendre le fichier exécutable si nécessaire
		if fileInfo.Mode()&0111 == 0 {
			if err := os.Chmod(filename, 0755); err != nil {
				return tuiOutputMsg{line: fmt.Sprintf("❌ Impossible de rendre exécutable: %v", err)}
			}
		}

		// Exécuter le fichier
		cmd := exec.Command("./" + filename)

		// Capturer stdout et stderr
		stdout, err := cmd.StdoutPipe()
		if err != nil {
			return tuiOutputMsg{line: fmt.Sprintf("❌ Erreur stdout: %v", err)}
		}

		stderr, err := cmd.StderrPipe()
		if err != nil {
			return tuiOutputMsg{line: fmt.Sprintf("❌ Erreur stderr: %v", err)}
		}

		if err := cmd.Start(); err != nil {
			return tuiOutputMsg{line: fmt.Sprintf("❌ Erreur démarrage: %v", err)}
		}

		// Lire la sortie dans un goroutine
		go func() {
			scanner := bufio.NewScanner(stdout)
			for scanner.Scan() {
				select {
				case <-stopChan:
					cmd.Process.Kill()
					return
				default:
					// Note: Dans une vraie app TUI, on enverrait ces messages
					// Ici on les ignore car on ne peut pas facilement les transmettre
				}
			}
		}()

		go func() {
			scanner := bufio.NewScanner(stderr)
			for scanner.Scan() {
				select {
				case <-stopChan:
					return
				default:
					// Idem pour stderr
				}
			}
		}()

		// Attendre la fin
		go func() {
			cmd.Wait()
		}()

		return tuiOutputMsg{line: fmt.Sprintf("🚀 Démarrage de %s...\n(Le TUI s'exécute en arrière-plan)", filename)}
	}
}

// Commande pour télécharger un fichier
func downloadFile(file GitHubFile) tea.Cmd {
	return func() tea.Msg {
		if file.Type != "file" || file.DownloadURL == "" {
			return operationCompleteMsg{filename: file.Name, operation: "download", err: fmt.Errorf("impossible de télécharger un dossier")}
		}

		resp, err := http.Get(file.DownloadURL)
		if err != nil {
			return operationCompleteMsg{filename: file.Name, operation: "download", err: err}
		}
		defer resp.Body.Close()

		out, err := os.Create(file.Name)
		if err != nil {
			return operationCompleteMsg{filename: file.Name, operation: "download", err: err}
		}
		defer out.Close()

		_, err = io.Copy(out, resp.Body)
		if err != nil {
			return operationCompleteMsg{filename: file.Name, operation: "download", err: err}
		}

		// Rendre le fichier exécutable
		os.Chmod(file.Name, 0755)

		return operationCompleteMsg{filename: file.Name, operation: "download", err: nil}
	}
}

// Commande pour supprimer un fichier
func deleteFile(filename string) tea.Cmd {
	return func() tea.Msg {
		err := os.Remove(filename)
		return operationCompleteMsg{filename: filename, operation: "delete", err: err}
	}
}

// Traiter toutes les opérations sélectionnées
func processSelectedFiles(m model) tea.Cmd {
	var cmds []tea.Cmd

	for idx := range m.selected {
		file := m.files[idx]
		if m.localFiles[file.Name] {
			// Fichier existe localement -> supprimer
			cmds = append(cmds, deleteFile(file.Name))
		} else {
			// Fichier n'existe pas localement -> télécharger
			cmds = append(cmds, downloadFile(file))
		}
	}

	// Ajouter une commande finale pour signaler la fin
	cmds = append(cmds, func() tea.Msg {
		return allOperationsCompleteMsg{}
	})

	return tea.Batch(cmds...)
}

// Gestion des événements
func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		// Quitter avec 'q' ou Ctrl+C
		if msg.String() == "q" || msg.String() == "ctrl+c" {
			// Arrêter le TUI si en cours
			if m.runningTUI != "" && m.tuiCmd != nil {
				m.tuiCmd.Process.Kill()
			}
			return m, tea.Quit
		}

		// Arrêter le TUI avec 's'
		if msg.String() == "s" && m.runningTUI != "" {
			if m.tuiCmd != nil && m.tuiCmd.Process != nil {
				m.tuiCmd.Process.Kill()
				m.addLog(fmt.Sprintf("🛑 Arrêt de %s", m.runningTUI))
			}
			m.runningTUI = ""
			m.tuiOutput = []string{}
			m.tuiCmd = nil
		}

		// Changer de panel avec Tab
		if msg.String() == "tab" && !m.loading && !m.processing {
			m.activePanel = (m.activePanel + 1) % 2
		}

		// Scroll dans le panneau de prévisualisation (panel droit)
		if m.activePanel == 2 && len(m.tuiOutput) > 0 {
			switch msg.String() {
			case "up", "k":
				if m.scrollOffset > 0 {
					m.scrollOffset--
				}
			case "down", "j":
				if m.scrollOffset < len(m.tuiOutput)-1 {
					m.scrollOffset++
				}
			}
		}

		// Navigation avec les flèches (seulement dans le panel du haut)
		if !m.loading && !m.processing && len(m.files) > 0 && m.activePanel == 0 {
			switch msg.String() {
			case "up", "k":
				if m.cursor > 0 {
					m.cursor--
				}
			case "down", "j":
				if m.cursor < len(m.files)-1 {
					m.cursor++
				}
			case " ":
				// Sélectionner/désélectionner le fichier actuel
				if m.selected[m.cursor] {
					delete(m.selected, m.cursor)
				} else {
					m.selected[m.cursor] = true
				}
			case "enter":
				// Exécuter le TUI du fichier sélectionné
				file := m.files[m.cursor]
				if m.localFiles[file.Name] {
					// Arrêter le TUI précédent si en cours
					if m.runningTUI != "" && m.tuiCmd != nil {
						m.tuiCmd.Process.Kill()
					}
					m.runningTUI = file.Name
					m.tuiOutput = []string{fmt.Sprintf("🚀 Exécution de %s...", file.Name)}
					m.scrollOffset = 0
					m.addLog(fmt.Sprintf("▶️  Lancement de %s", file.Name))

					// Créer un nouveau canal d'arrêt
					m.stopTUIChan = make(chan bool, 1)
					return m, runTUI(file.Name, m.stopTUIChan)
				} else {
					m.addLog(fmt.Sprintf("⚠️  %s n'est pas téléchargé", file.Name))
				}
			case "d":
				// Télécharger/Supprimer les fichiers sélectionnés avec 'd'
				if len(m.selected) > 0 {
					m.processing = true
					m.statusMsg = fmt.Sprintf("Traitement de %d Plugin(s)...", len(m.selected))
					m.addLog(fmt.Sprintf("🚀 Démarrage du traitement de %d Plugin(s)", len(m.selected)))
					return m, processSelectedFiles(m)
				}
			case "c":
				// Annuler toutes les sélections
				m.selected = make(map[int]bool)
			}
		}

	case tea.WindowSizeMsg:
		// Capturer la taille de la fenêtre
		m.width = msg.Width
		m.height = msg.Height

	case filesLoadedMsg:
		m.loading = false
		if msg.err != nil {
			m.err = msg.err
			m.addLog(fmt.Sprintf("❌ Erreur lors du chargement: %v", msg.err))
		} else {
			m.files = msg.files
			m.addLog(fmt.Sprintf("✅ %d Plugin(s) chargé(s)", len(msg.files)))
			// Vérifier quels fichiers existent localement
			m.localFiles = make(map[string]bool)
			for _, file := range m.files {
				if _, err := os.Stat(file.Name); err == nil {
					m.localFiles[file.Name] = true
				}
			}
		}

	case tuiOutputMsg:
		m.tuiMutex.Lock()
		m.tuiOutput = append(m.tuiOutput, msg.line)
		// Limiter le nombre de lignes
		if len(m.tuiOutput) > 1000 {
			m.tuiOutput = m.tuiOutput[len(m.tuiOutput)-1000:]
		}
		m.tuiMutex.Unlock()

	case tuiStoppedMsg:
		m.addLog(fmt.Sprintf("⏹️  %s terminé", msg.filename))
		m.runningTUI = ""
		m.tuiCmd = nil

	case operationCompleteMsg:
		if msg.err != nil {
			m.statusMsg = fmt.Sprintf("❌ Erreur %s: %v", msg.filename, msg.err)
			m.addLog(fmt.Sprintf("❌ Erreur %s %s: %v", msg.operation, msg.filename, msg.err))
		} else {
			if msg.operation == "download" {
				m.statusMsg = fmt.Sprintf("✅ %s téléchargé!", msg.filename)
				m.localFiles[msg.filename] = true
				m.addLog(fmt.Sprintf("⬇️  %s téléchargé avec succès", msg.filename))
			} else {
				m.statusMsg = fmt.Sprintf("🗑️  %s supprimé!", msg.filename)
				delete(m.localFiles, msg.filename)
				m.addLog(fmt.Sprintf("🗑️  %s supprimé avec succès", msg.filename))
				// Arrêter le TUI si c'était celui en cours
				if m.runningTUI == msg.filename {
					if m.tuiCmd != nil && m.tuiCmd.Process != nil {
						m.tuiCmd.Process.Kill()
					}
					m.runningTUI = ""
					m.tuiOutput = []string{}
					m.tuiCmd = nil
				}
			}
		}

	case allOperationsCompleteMsg:
		m.processing = false
		m.selected = make(map[int]bool)
		m.statusMsg = "✅ Toutes les opérations terminées!"
		m.addLog("✅ Toutes les opérations terminées!")
		// Effacer le message après 3 secondes
		return m, tea.Tick(time.Second*3, func(t time.Time) tea.Msg {
			return tickMsg(t)
		})

	case tickMsg:
		if m.loading || m.processing {
			m.spinnerFrame = (m.spinnerFrame + 1) % len(spinnerFrames)
			return m, tickCmd()
		} else if m.statusMsg != "" && !strings.Contains(m.statusMsg, "...") {
			// Effacer le message de statut
			m.statusMsg = ""
		}
	}

	return m, nil
}

// Affichage
func (m model) View() string {
	// Largeur pour les panels gauches
	leftPanelWidth := 35
	// Largeur pour le panel droit (TUI)
	rightPanelWidth := m.width - leftPanelWidth - 6

	// Calculer la hauteur disponible
	availableHeight := m.height - 1

	// Hauteur fixe pour chaque panel gauche
	topHeight := int(float64(availableHeight) * 0.6)
	bottomHeight := availableHeight - topHeight - 4

	// Hauteur du panel de droite
	rightPanelHeight := availableHeight - 2

	// Styles pour les panels gauches
	topBoxStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("86")).
		Padding(1, 2).
		Width(leftPanelWidth).
		Height(topHeight)

	topBoxInactiveStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("240")).
		Padding(1, 2).
		Width(leftPanelWidth).
		Height(topHeight)

	bottomBoxStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("86")).
		Padding(1, 2).
		Width(leftPanelWidth).
		Height(bottomHeight)

	bottomBoxInactiveStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("240")).
		Padding(1, 2).
		Width(leftPanelWidth).
		Height(bottomHeight)

	// Style pour le panel de droite
	rightBoxStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("86")).
		Padding(1, 2).
		Width(rightPanelWidth).
		Height(rightPanelHeight)

	// Style de la barre de statut
	statusStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("240")).
		Width(m.width - 2).
		Align(lipgloss.Left)

	// Styles pour les fichiers
	selectedStyle := lipgloss.NewStyle().
		Background(lipgloss.Color("240")).
		Foreground(lipgloss.Color("15"))

	notDownloadedStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("39"))

	downloadedStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("2"))

	toDeleteStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("1"))

	// === PANEL DU HAUT ===
	var topContent strings.Builder
	topContent.WriteString("Repository: TWilhem/Plugin\n\n")

	if m.loading {
		topContent.WriteString("Chargement des Plugin...\n\n")
	} else if m.err != nil {
		topContent.WriteString(fmt.Sprintf("❌ Erreur: %v\n\n", m.err))
	} else if len(m.files) == 0 {
		topContent.WriteString("Aucun Plugin trouvé\n\n")
	} else {
		topContent.WriteString(fmt.Sprintf("%d Plugin disponible:\n\n", len(m.files)))
		for i, file := range m.files {
			var textStyle lipgloss.Style
			if m.selected[i] && m.localFiles[file.Name] {
				textStyle = toDeleteStyle
			} else if m.localFiles[file.Name] || (m.selected[i] && !m.localFiles[file.Name]) {
				textStyle = downloadedStyle
			} else {
				textStyle = notDownloadedStyle
			}

			// Ajouter un indicateur si c'est le TUI en cours
			prefix := ""
			if file.Name == m.runningTUI {
				prefix = "▶ "
			}

			if i == m.cursor && m.activePanel == 0 {
				selectedWithColor := selectedStyle.Foreground(textStyle.GetForeground())
				paddedLine := prefix + file.Name + strings.Repeat(" ", leftPanelWidth-len(file.Name)-len(prefix)-4)
				topContent.WriteString(selectedWithColor.Render(paddedLine) + "\n")
			} else {
				topContent.WriteString(textStyle.Render(prefix+file.Name) + "\n")
			}
		}
	}

	// === PANEL DU BAS - LOGS ===
	var bottomContent strings.Builder
	bottomContent.WriteString("Logs d'activité\n\n")

	if len(m.logs) == 0 {
		bottomContent.WriteString("Aucun log pour le moment...")
	} else {
		maxLogs := bottomHeight - 4
		if maxLogs < 1 {
			maxLogs = 1
		}

		startIdx := 0
		if len(m.logs) > maxLogs {
			startIdx = len(m.logs) - maxLogs
		}

		maxLogWidth := leftPanelWidth - 6

		for i := len(m.logs) - 1; i >= startIdx; i-- {
			log := m.logs[i]
			if len(log) > maxLogWidth {
				log = log[:maxLogWidth-3] + "..."
			}
			bottomContent.WriteString(log + "\n")
		}
	}

	// === PANEL DE DROITE - TUI OUTPUT ===
	var rightContent strings.Builder
	if m.runningTUI != "" {
		rightContent.WriteString(fmt.Sprintf("🖥️  TUI: %s\n\n", m.runningTUI))

		m.tuiMutex.Lock()
		if len(m.tuiOutput) > 0 {
			maxLines := rightPanelHeight - 4
			if maxLines < 1 {
				maxLines = 1
			}

			startLine := len(m.tuiOutput) - maxLines
			if startLine < 0 {
				startLine = 0
			}

			maxWidth := rightPanelWidth - 6

			for i := startLine; i < len(m.tuiOutput); i++ {
				line := m.tuiOutput[i]
				if len(line) > maxWidth {
					line = line[:maxWidth-3] + "..."
				}
				rightContent.WriteString(line + "\n")
			}
		} else {
			rightContent.WriteString("En attente de sortie...\n")
		}
		m.tuiMutex.Unlock()
	} else {
		rightContent.WriteString("Exécution TUI\n\n")
		rightContent.WriteString("Sélectionnez un fichier\n")
		rightContent.WriteString("téléchargé et appuyez\n")
		rightContent.WriteString("sur Enter pour exécuter\n")
		rightContent.WriteString("son TUI ici.\n\n")
		rightContent.WriteString("Commandes:\n")
		rightContent.WriteString("• Enter: Exécuter\n")
		rightContent.WriteString("• s: Arrêter le TUI\n")
		rightContent.WriteString("• d: Télécharger/Supprimer")
	}

	// Choisir les styles selon le panel actif
	var topStyle, bottomStyle lipgloss.Style
	if m.activePanel == 0 {
		topStyle = topBoxStyle
		bottomStyle = bottomBoxInactiveStyle
	} else {
		topStyle = topBoxInactiveStyle
		bottomStyle = bottomBoxStyle
	}

	// Empiler les panels gauches
	leftPanels := lipgloss.JoinVertical(
		lipgloss.Left,
		topStyle.Render(topContent.String()),
		bottomStyle.Render(bottomContent.String()),
	)

	// Joindre gauche et droite
	allPanels := lipgloss.JoinHorizontal(
		lipgloss.Top,
		leftPanels,
		rightBoxStyle.Render(rightContent.String()),
	)

	panelsHeight := topHeight + bottomHeight + 4
	spacerHeight := availableHeight - panelsHeight
	if spacerHeight < 0 {
		spacerHeight = 0
	}
	spacer := strings.Repeat("\n", spacerHeight)

	// Barre de statut
	var statusBar statusBar

	if m.loading {
		statusBar.message = fmt.Sprintf("Récupération %s", spinnerFrames[m.spinnerFrame])
	} else if m.processing {
		statusBar.message = fmt.Sprintf("%s %s", m.statusMsg, spinnerFrames[m.spinnerFrame])
	} else if m.statusMsg != "" {
		statusBar.message = m.statusMsg
	} else if len(m.selected) > 0 {
		statusBar.message = fmt.Sprintf("%d Plugin(s) sélectionné(s)", len(m.selected))
	} else if m.runningTUI != "" {
		statusBar.message = fmt.Sprintf("▶️  %s en cours d'exécution", m.runningTUI)
	}

	statusBar.commands = m.cmdTemplate

	return allPanels + spacer + "\n" + statusStyle.Render(statusBar.render())
}

func main() {
	p := tea.NewProgram(
		initialModel(),
		tea.WithAltScreen(),
	)

	if _, err := p.Run(); err != nil {
		fmt.Printf("Erreur: %v\n", err)
		os.Exit(1)
	}
}
