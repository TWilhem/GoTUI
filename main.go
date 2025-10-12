package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
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
	activePanel  int          // 0 = haut, 1 = bas
	logs         []string     // Historique des logs
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
		cmdTemplate:  "↑/↓: Navigation | Tab: Changer panel | Espace: Sélectionner | Enter: Valider | c: Annuler | q: Quitter",
		activePanel:  0,
		logs:         []string{},
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
			return m, tea.Quit
		}

		// Changer de panel avec Tab
		if msg.String() == "tab" && !m.loading && !m.processing {
			m.activePanel = (m.activePanel + 1) % 2
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
				// Valider et exécuter les opérations
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
	// Largeur fixe pour les panels
	panelWidth := 35

	// Calculer la hauteur disponible pour les panels (hauteur totale - 1 ligne pour la barre de statut)
	availableHeight := m.height - 1

	// Hauteur fixe pour chaque panel (répartition 60/40)
	topHeight := int(float64(availableHeight) * 0.6)
	bottomHeight := availableHeight - topHeight - 4 // -4 pour les bordures

	// Style pour le panel actif (haut)
	topBoxStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("86")).
		Padding(1, 2).
		Width(panelWidth).
		Height(topHeight)

	// Style pour le panel inactif (haut)
	topBoxInactiveStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("240")).
		Padding(1, 2).
		Width(panelWidth).
		Height(topHeight)

	// Style pour le panel actif (bas)
	bottomBoxStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("86")).
		Padding(1, 2).
		Width(panelWidth).
		Height(bottomHeight)

	// Style pour le panel inactif (bas)
	bottomBoxInactiveStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("240")).
		Padding(1, 2).
		Width(panelWidth).
		Height(bottomHeight)

	// Style de la barre de statut
	statusStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("240")).
		Width(m.width - 2).
		Align(lipgloss.Left)

	// Style pour l'élément sélectionné (ligne complète avec fond)
	selectedStyle := lipgloss.NewStyle().
		Background(lipgloss.Color("240")).
		Foreground(lipgloss.Color("15"))

	// Style pour fichier non téléchargé (bleu clair)
	notDownloadedStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("39"))

	// Style pour fichier téléchargé (vert)
	downloadedStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("2"))

	// Style pour fichier à supprimer (rouge)
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
			// Déterminer la couleur selon l'état du fichier
			var textStyle lipgloss.Style
			if m.selected[i] && m.localFiles[file.Name] {
				textStyle = toDeleteStyle
			} else if m.localFiles[file.Name] || (m.selected[i] && !m.localFiles[file.Name]) {
				textStyle = downloadedStyle
			} else {
				textStyle = notDownloadedStyle
			}

			if i == m.cursor && m.activePanel == 0 {
				selectedWithColor := selectedStyle.
					Foreground(textStyle.GetForeground())
				paddedLine := file.Name + strings.Repeat(" ", panelWidth-len(file.Name)-4)
				topContent.WriteString(selectedWithColor.Render(paddedLine) + "\n")
			} else {
				topContent.WriteString(textStyle.Render(file.Name) + "\n")
			}
		}
	}

	// === PANEL DU BAS - LOGS ===
	var bottomContent strings.Builder
	bottomContent.WriteString("Logs d'activité\n\n")

	if len(m.logs) == 0 {
		bottomContent.WriteString("Aucun log pour le moment...")
	} else {
		// Calculer le nombre de logs affichables selon la hauteur du panel
		maxLogs := bottomHeight - 5
		if maxLogs < 1 {
			maxLogs = 1
		}

		startIdx := 0
		if len(m.logs) > maxLogs {
			startIdx = len(m.logs) - maxLogs
		}

		// Largeur maximale pour les logs (largeur du panel - padding - bordures)
		maxLogWidth := panelWidth - 6

		for i := len(m.logs) - 1; i >= startIdx; i-- {
			log := m.logs[i]
			// Tronquer le log s'il est trop long
			if len(log) > maxLogWidth {
				log = log[:maxLogWidth-3] + "..."
			}
			bottomContent.WriteString(log + "\n")
		}
	}

	// Choisir le style selon le panel actif
	var topStyle, bottomStyle lipgloss.Style
	if m.activePanel == 0 {
		topStyle = topBoxStyle
		bottomStyle = bottomBoxInactiveStyle
	} else {
		topStyle = topBoxInactiveStyle
		bottomStyle = bottomBoxStyle
	}

	// Empiler les deux panels verticalement
	panels := lipgloss.JoinVertical(
		lipgloss.Left,
		topStyle.Render(topContent.String()),
		bottomStyle.Render(bottomContent.String()),
	)

	// Calculer l'espace restant pour remplir jusqu'à la barre de statut
	panelsHeight := topHeight + bottomHeight + 4 // +4 pour les bordures
	spacerHeight := availableHeight - panelsHeight
	if spacerHeight < 0 {
		spacerHeight = 0
	}
	spacer := strings.Repeat("\n", spacerHeight)

	// Barre de statut en bas
	var statusBar statusBar

	if m.loading {
		statusBar.message = fmt.Sprintf("Récupération %s", spinnerFrames[m.spinnerFrame])
	} else if m.processing {
		statusBar.message = fmt.Sprintf("%s %s", m.statusMsg, spinnerFrames[m.spinnerFrame])
	} else if m.statusMsg != "" {
		statusBar.message = m.statusMsg
	} else if len(m.selected) > 0 {
		statusBar.message = fmt.Sprintf("%d Plugin(s) sélectionné(s)", len(m.selected))
	}
	statusBar.commands = m.cmdTemplate

	return panels + spacer + "\n" + statusStyle.Render(statusBar.render())
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
