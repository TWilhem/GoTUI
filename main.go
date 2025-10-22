package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/user"
	"path/filepath"
	"plugin"
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

// Message pour le chargement de plugin
type pluginLoadedMsg struct {
	model tea.Model
	err   error
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
	width            int
	height           int
	files            []GitHubFile
	loading          bool
	processing       bool
	err              error
	spinnerFrame     int
	cursor           int
	statusMsg        string
	localFiles       map[string]bool
	selected         map[int]bool // Fichiers sélectionnés pour téléchargement/suppression
	cmdTemplate      string       // Template du status
	activePanel      int          // 0 = haut, 1 = bas, 2 = droite
	logs             []string     // Historique des logs
	tuiOutput        []string     // Sortie du TUI en cours d'exécution
	runningTUI       string       // Nom du fichier TUI en cours d'exécution
	embeddedTUI      tea.Model
	tuiMutex         *sync.Mutex // Mutex pour l'accès concurrent
	scrollOffset     int         // Offset pour le scroll du contenu
	embeddedPluginID string      // Id plugin
	pluginDir        string
}

var spinnerFrames = []string{"|", "/", "-", "\\"}

// Initialisation
func initialModel(baseDir string) model {
	// pluginDir = baseDir/.Plugin (si baseDir est un dossier fourni)
	pluginDir := filepath.Join(baseDir, "Plugin")

	// Créer le dossier s'il n'existe pas
	if _, err := os.Stat(pluginDir); os.IsNotExist(err) {
		os.Mkdir(pluginDir, 0755)
	}

	return model{
		loading:      true,
		processing:   false,
		files:        []GitHubFile{},
		spinnerFrame: 0,
		cursor:       0,
		localFiles:   make(map[string]bool),
		selected:     make(map[int]bool),
		cmdTemplate:  "Navigation: ↑/↓ | Panel: Tab | Selection: Espace | Execution: e | Validation: Enter | Annuler: c | Stop: s | Quitter: q",
		activePanel:  0,
		logs:         []string{},
		tuiOutput:    []string{},
		runningTUI:   "",
		embeddedTUI:  nil,
		tuiMutex:     &sync.Mutex{},
		scrollOffset: 0,
		pluginDir:    pluginDir,
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
func downloadFile(file GitHubFile, pluginDir string) tea.Cmd {
	return func() tea.Msg {
		if file.Type != "file" || file.DownloadURL == "" {
			return operationCompleteMsg{filename: file.Name, operation: "download", err: fmt.Errorf("impossible de télécharger un dossier")}
		}

		resp, err := http.Get(file.DownloadURL)
		if err != nil {
			return operationCompleteMsg{filename: file.Name, operation: "download", err: err}
		}
		defer resp.Body.Close()

		filePath := filepath.Join(pluginDir, file.Name)
		out, err := os.Create(filePath)
		if err != nil {
			return operationCompleteMsg{filename: file.Name, operation: "download", err: err}
		}
		defer out.Close()

		_, err = io.Copy(out, resp.Body)
		if err != nil {
			return operationCompleteMsg{filename: file.Name, operation: "download", err: err}
		}

		// Rendre le fichier exécutable
		os.Chmod(filePath, 0755)

		return operationCompleteMsg{filename: file.Name, operation: "download", err: nil}
	}
}

// Commande pour supprimer un fichier
func deleteFile(filename string, pluginDir string) tea.Cmd {
	return func() tea.Msg {
		filePath := filepath.Join(pluginDir, filename)
		err := os.Remove(filePath)
		return operationCompleteMsg{filename: filename, operation: "delete", err: err}
	}
}

// Charger un plugin externe
func loadPlugin(filename string, pluginDir string) tea.Cmd {
	return func() tea.Msg {
		// Ouvrir le plugin
		pluginPath := filepath.Join(pluginDir, filename)
		plug, err := plugin.Open(pluginPath)
		if err != nil {
			return pluginLoadedMsg{model: nil, err: fmt.Errorf("erreur ouverture plugin: %v", err)}
		}

		// Chercher le symbole NewTUI
		symNewTUI, err := plug.Lookup("NewTUI")
		if err != nil {
			return pluginLoadedMsg{model: nil, err: fmt.Errorf("symbole NewTUI non trouvé: %v", err)}
		}

		// Convertir en fonction
		newTUI, ok := symNewTUI.(func() tea.Model)
		if !ok {
			return pluginLoadedMsg{model: nil, err: fmt.Errorf("format de plugin invalide")}
		}

		// Créer le modèle
		tuiModel := newTUI()
		return pluginLoadedMsg{model: tuiModel, err: nil}
	}
}

// Traiter toutes les opérations sélectionnées
func processSelectedFiles(m model) tea.Cmd {
	var cmds []tea.Cmd

	for idx := range m.selected {
		file := m.files[idx]
		if m.localFiles[file.Name] {
			// Fichier existe localement -> supprimer
			cmds = append(cmds, deleteFile(file.Name, m.pluginDir))
		} else {
			// Fichier n'existe pas localement -> télécharger
			cmds = append(cmds, downloadFile(file, m.pluginDir))
		}
	}

	// Ajouter une commande finale pour signaler la fin
	cmds = append(cmds, func() tea.Msg {
		return allOperationsCompleteMsg{}
	})

	return tea.Batch(cmds...)
}

// Ajoute un alias dans ~/.Plugin/.pluginbashrc
func addAliasToPluginBashrc(filename, pluginDir string) error {
	pluginFile := filepath.Join(filepath.Dir(pluginDir), ".pluginbashrc")

	aliasLine := fmt.Sprintf("alias %s='%s/%s'\n", strings.TrimSuffix(filename, filepath.Ext(filename)), pluginDir, filename)

	// Lire le contenu existant
	content, _ := os.ReadFile(pluginFile)
	if strings.Contains(string(content), aliasLine) {
		return nil // alias déjà présent
	}

	// Ajouter la ligne
	f, err := os.OpenFile(pluginFile, os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer f.Close()

	_, err = f.WriteString(aliasLine)
	return err
}

// Supprime l’alias correspondant à un fichier
func removeAliasFromPluginBashrc(filename, pluginDir string) error {
	pluginFile := filepath.Join(filepath.Dir(pluginDir), ".pluginbashrc")

	aliasPrefix := fmt.Sprintf("alias %s='%s/%s'", strings.TrimSuffix(filename, filepath.Ext(filename)), pluginDir, filename)

	content, err := os.ReadFile(pluginFile)
	if err != nil {
		return err
	}

	lines := strings.Split(string(content), "\n")
	var newLines []string
	for _, line := range lines {
		if !strings.HasPrefix(line, aliasPrefix) {
			newLines = append(newLines, line)
		}
	}

	return os.WriteFile(pluginFile, []byte(strings.Join(newLines, "\n")), 0644)
}

// Gestion des événements
func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd

	// Si plugin actif : PROPAGER TOUS les messages au plugin
	if m.embeddedTUI != nil && m.activePanel == 2 {
		// Propager d'abord
		newModel, c := m.embeddedTUI.Update(msg)
		m.embeddedTUI = newModel
		// Executer la commande renvoyée par le plugin (si présente)
		cmd = c

		// Gérer les messages "PLUGIN_QUIT:<id>" renvoyés par le plugin
		if s, ok := msg.(string); ok && strings.HasPrefix(s, "PLUGIN_QUIT:") {
			id := strings.TrimPrefix(s, "PLUGIN_QUIT:")
			// Optionnel : vérifier que id correspond au plugin actif
			if m.embeddedPluginID == "" || m.embeddedPluginID == id {
				m.addLog("🛑 Plugin arrêté: " + id)
				m.embeddedTUI = nil
				m.embeddedPluginID = ""
				m.runningTUI = ""
				m.activePanel = 0
				// ne pas propager plus loin
				return m, nil
			}
		}

		// Sinon continuer le flux normal (retourner la commande du plugin)
		return m, cmd
	}

	switch msg := msg.(type) {
	case tea.KeyMsg:
		// Quitter avec 'q' ou Ctrl+C
		if msg.String() == "q" || msg.String() == "ctrl+c" {
			fmt.Println("🔄  Veuillez exécuter : source ~/.bashrc pour recharger vos alias.")
			return m, tea.Quit
		}

		// Changer de panel avec Tab
		if msg.String() == "tab" && !m.loading && !m.processing {
			if m.embeddedTUI != nil {
				m.activePanel = (m.activePanel + 1) % 3
			} else {
				m.activePanel = (m.activePanel + 1) % 2
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
			case "e":
				// Exécuter le TUI du fichier sélectionné
				file := m.files[m.cursor]
				if m.localFiles[file.Name] {
					m.runningTUI = file.Name
					m.addLog(fmt.Sprintf("▶️  Chargement de %s...", file.Name))
					m.activePanel = 2
					return m, loadPlugin(file.Name, m.pluginDir)
				} else {
					m.addLog(fmt.Sprintf("⚠️  %s n'est pas téléchargé", file.Name))
				}
			case "enter":
				// Télécharger/Supprimer les fichiers sélectionnés
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
				filePath := filepath.Join(m.pluginDir, file.Name)
				if _, err := os.Stat(filePath); err == nil {
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
				// ✅ Ajouter l'alias automatiquement
				if err := addAliasToPluginBashrc(msg.filename, m.pluginDir); err != nil {
					m.addLog(fmt.Sprintf("⚠️  Impossible d'ajouter l'alias pour %s: %v", msg.filename, err))
				} else {
					m.addLog(fmt.Sprintf("🔗 Alias ajouté pour %s", msg.filename))
				}
			} else {
				m.statusMsg = fmt.Sprintf("🗑️  %s supprimé!", msg.filename)
				delete(m.localFiles, msg.filename)
				m.addLog(fmt.Sprintf("🗑️  %s supprimé avec succès", msg.filename))
				// ✅ Supprimer l'alias automatiquement
				if err := removeAliasFromPluginBashrc(msg.filename, m.pluginDir); err != nil {
					m.addLog(fmt.Sprintf("⚠️  Impossible de retirer l'alias pour %s: %v", msg.filename, err))
				} else {
					m.addLog(fmt.Sprintf("🔗 Alias supprimé pour %s", msg.filename))
				}
				// Arrêter le TUI si c'était celui en cours
				if m.runningTUI == msg.filename {
					m.embeddedTUI = nil
					m.runningTUI = ""
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

	case pluginLoadedMsg:
		if msg.err != nil {
			m.addLog(fmt.Sprintf("❌ Erreur chargement plugin: %v", msg.err))
			m.runningTUI = ""
			m.activePanel = 0
		} else {
			m.embeddedTUI = msg.model
			m.addLog(fmt.Sprintf("✅ Plugin %s chargé avec succès", m.runningTUI))
			return m, m.embeddedTUI.Init()
		}

	case tickMsg:
		if m.loading || m.processing {
			m.spinnerFrame = (m.spinnerFrame + 1) % len(spinnerFrames)
			return m, tickCmd()
		} else if m.statusMsg != "" && !strings.Contains(m.statusMsg, "...") {
			// Effacer le message de statut
			m.statusMsg = ""
		}
	}

	// Propager les messages au TUI imbriqué
	if m.embeddedTUI != nil {
		m.embeddedTUI, cmd = m.embeddedTUI.Update(msg)
		return m, cmd
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

	rightBoxInactiveStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("240")).
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
		maxLogs := bottomHeight - 5
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
	if m.embeddedTUI != nil {
		// Afficher le TUI imbriqué
		rightContent.WriteString(m.embeddedTUI.View())
	} else {
		rightContent.WriteString("Exécution TUI\n\n")
		rightContent.WriteString("Sélectionnez un fichier\n")
		rightContent.WriteString("téléchargé et appuyez\n")
		rightContent.WriteString("sur 'e' pour exécuter\n")
		rightContent.WriteString("son TUI ici.\n\n")
		rightContent.WriteString("Commandes:\n")
		rightContent.WriteString("• e: Exécuter\n")
		rightContent.WriteString("• s: Arrêter le TUI\n")
		rightContent.WriteString("• Enter: Télécharger/Supprimer")
	}

	// Choisir les styles selon le panel actif
	var topStyle, bottomStyle, rightStyle lipgloss.Style
	if m.activePanel == 0 {
		topStyle = topBoxStyle
		bottomStyle = bottomBoxInactiveStyle
		rightStyle = rightBoxInactiveStyle
	} else if m.activePanel == 1 {
		topStyle = topBoxInactiveStyle
		bottomStyle = bottomBoxStyle
		rightStyle = rightBoxInactiveStyle
	} else if m.activePanel == 2 {
		topStyle = topBoxInactiveStyle
		bottomStyle = bottomBoxInactiveStyle
		rightStyle = rightBoxStyle
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
		rightStyle.Render(rightContent.String()),
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
	} else {
		statusBar.commands = m.cmdTemplate
	}

	return allPanels + spacer + "\n" + statusStyle.Render(statusBar.render())
}

func main() {
	usr, err := user.Current()
	if err != nil {
		fmt.Println("Impossible de récupérer l'utilisateur courant:", err)
		return
	}

	chemin := flag.String("c", "", "Chemin du fichier à utiliser")
	flag.Parse()

	var baseDir string
	if *chemin != "" {
		// si l'utilisateur spécifie un chemin, l'utiliser tel quel (prendre sa version absolue)
		abs, err := filepath.Abs(*chemin)
		if err != nil {
			baseDir = *chemin
		} else {
			baseDir = abs
		}
	} else {
		// Par défaut, utiliser le dossier utilisateur (HOME)
		baseDir = filepath.Join(usr.HomeDir, ".Plugin/")
	}

	m := initialModel(baseDir)
	pluginDir := m.pluginDir
	pluginFile := filepath.Join(filepath.Dir(pluginDir), ".pluginbashrc")

	home := usr.HomeDir
	bashrcPath := filepath.Join(home, ".bashrc")

	pluginBlock := fmt.Sprintf(`# Ajout Liste Plugin
if [ -f %s ]; then 
    source %s
fi`, pluginFile, pluginFile)

	// Vérification des arguments
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "install":
			// --- Créer le dossier ./.Plugin/Plugin s'il n'existe pas ---
			if _, err := os.Stat(pluginDir); os.IsNotExist(err) {
				err := os.MkdirAll(pluginDir, 0755)
				if err != nil {
					fmt.Printf("Erreur création du dossier %s: %s", pluginDir, err)
					return
				}
				fmt.Printf("Dossier %s créé.\n", pluginDir)
			}

			// --- Créer le fichier ./.Plugin/.pluginbashrc ---
			if _, err := os.Stat(pluginFile); os.IsNotExist(err) {
				content := "# Plugin bashrc initialisé\n" + "alias Plugin=\"$HOME/.Plugin/Pannel\"\n"
				err := os.WriteFile(pluginFile, []byte(content), 0644)
				if err != nil {
					fmt.Printf("Erreur création du fichier %s: %s", pluginFile, err)
					return
				}
				fmt.Printf("Fichier %s créé.\n", pluginFile)
			} else {
				fmt.Printf("Fichier %s déjà existant.\n", pluginFile)
			}

			// --- Ajouter le bloc dans ~/.bashrc ---
			content, _ := os.ReadFile(bashrcPath)
			if !strings.Contains(string(content), pluginBlock) {
				f, err := os.OpenFile(bashrcPath, os.O_APPEND|os.O_WRONLY, 0644)
				if err != nil {
					fmt.Printf("Erreur ouverture %s/.bashrc: %s\n", home, err)
					return
				}
				defer f.Close()

				if _, err := f.WriteString("\n" + pluginBlock + "\n"); err != nil {
					fmt.Printf("Erreur écriture dans %s/.bashrc: %s\n", home, err)
					return
				}
				fmt.Printf("Bloc plugin ajouté à %s/.bashrc\n", home)
			} else {
				fmt.Printf("Bloc plugin déjà présent dans %s/.bashrc\n", home)
			}
			return

		case "uninstall":
			// --- Supprimer le fichier .pluginbashrc ---
			if err := os.Remove(pluginFile); err == nil {
				fmt.Printf("Fichier %s supprimé.\n", pluginFile)
			} else if os.IsNotExist(err) {
				fmt.Printf("Fichier %s déjà supprimé.\n", pluginFile)
			} else {
				fmt.Printf("Erreur suppression du fichier %s: %s\n", pluginFile, err)
			}

			// --- Supprimer le bloc du ~/.bashrc ---
			content, err := os.ReadFile(bashrcPath)
			if err != nil {
				fmt.Printf("Erreur lecture %s/.bashrc: %s\n", home, err)
				return
			}

			newContent := strings.ReplaceAll(string(content), pluginBlock, "")
			if err := os.WriteFile(bashrcPath, []byte(newContent), 0644); err != nil {
				fmt.Printf("Erreur écriture %s/.bashrc: %s\n", home, err)
				return
			}
			fmt.Printf("Bloc plugin supprimé de %s/.bashrc\n", home)
			return

		default:
			fmt.Printf("Commande inconnue: %s\n", os.Args[1])
			return
		}
	}

	p := tea.NewProgram(
		m,
		tea.WithAltScreen(),
	)

	if _, err := p.Run(); err != nil {
		fmt.Printf("Erreur: %v\n", err)
		os.Exit(1)
	}
}
