# GoTUI

**GoTUI** est une interface **TUI (Text-based User Interface)** écrite en **Go** utilisant le framework [Bubbletea](https://github.com/charmbracelet/bubbletea).  
Elle permet de **gérer dynamiquement des plugins** destinés à simplifier le **management de systèmes Debian/Linux**, en offrant un environnement visuel complet dans le terminal.

---

## Fonctionnalités principales

- **Exploration automatique** des plugins disponibles sur GitHub  
  → Récupère la liste des fichiers du dépôt [`TWilhem/Plugin`](https://github.com/TWilhem/Plugin).

- **Téléchargement & suppression** des plugins directement depuis l’interface.  
  → Les plugins sont stockés dans `~/.Plugin/Plugin`.

- **Chargement dynamique de plugins `.so`**  
  → Chaque plugin peut embarquer son propre TUI et être exécuté sans quitter GoTUI.

- **Gestion intelligente des alias Bash**  
  → Chaque plugin téléchargé ajoute automatiquement un alias dans `~/.Plugin/.pluginbashrc`, chargé depuis ton `.bashrc`.

- **Journalisation en temps réel**  
  → Une console de logs intégrée affiche toutes les actions effectuées (téléchargements, exécutions, erreurs...).

- **Interface à panneaux multiples :**
  - Panneau supérieur : liste des plugins
  - Panneau inférieur : logs d’activité
  - Panneau droit : exécution du TUI du plugin actif

---

## Installation

### 1. Cloner le dépôt :
```bash
git clone https://github.com/TWilhem/GoTUI.git
cd GoTUI
```

### 2. Compiler :
```bash
go build -o Pannel
```

### 3. Initialiser l’environnement :
```bash
./Pannel install
```

Cette commande :
- crée le répertoire `~/.Plugin/Plugin`
- télécharge le fichier `Chargeur`
- génère le fichier `~/.Plugin/.pluginbashrc`
- ajoute le bloc nécessaire à ton `~/.bashrc`

Une fois l’installation terminée, recharge ton bash :
```bash
source ~/.bashrc
```

---

## Utilisation

### Lancer GoTUI :
```bash
Pannel
```

### Commandes clavier :
| Touche | Action |
|:------:|:--------|
| ↑ / ↓ | Naviguer dans la liste des plugins |
| **Espace** | Sélectionner / désélectionner un plugin |
| **Enter** | Télécharger ou supprimer les plugins sélectionnés |
| **e** | Exécuter le plugin sélectionné |
| **Tab** | Changer de panneau |
| **c** | Annuler la sélection |
| **q** | Quitter GoTUI |

---

## Désinstallation

Pour supprimer complètement GoTUI et ses fichiers :
```bash
./Pannel uninstall
```

Cette commande :
- supprime le répertoire `~/.Plugin/Plugin`
- supprime les fichiers `Chargeur` et `.pluginbashrc`
- retire le bloc ajouté à ton `.bashrc`

---

## Structure du projet

```
GoTUI/
├── main.go              # Code principal de l’application TUI
├── go.mod / go.sum      # Dépendances Go
└── README.md            # Documentation
```

---

## Dépendances principales

- [Bubbletea](https://github.com/charmbracelet/bubbletea) → Framework TUI Go
- [Lipgloss](https://github.com/charmbracelet/lipgloss) → Styles et mise en page terminal
- [plugin](https://pkg.go.dev/plugin) → Chargement dynamique de modules Go

---

## Exemple de fonctionnement

1. Lancer `GoTUI`
2. L’interface affiche les plugins disponibles sur GitHub.  
3. Sélectionner un plugin avec **Espace**, puis valider avec **Enter** pour le télécharger.  
4. Appuyer sur **e** pour exécuter le TUI embarqué du plugin directement dans GoTUI.  
5. Les logs des opérations s’affichent en bas de l’écran.

---

## Auteur

**TWilhem**  
Projet personnel open-source visant à faciliter la gestion de plugins d’administration Debian via une interface terminal moderne en Go.

---

## Licence

Ce projet est distribué sous licence **MIT**.  
Tu es libre de l’utiliser, le modifier et le redistribuer.

---

> *GoTUI – Simplifie la gestion de tes outils Debian directement depuis ton terminal !*
