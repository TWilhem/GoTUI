# GoTUI (Pannel)

**GoTUI** est une interface **TUI (Text-based User Interface)** écrite en **Go** utilisant le framework [Bubbletea](https://github.com/charmbracelet/bubbletea) et [Lipgloss](https://github.com/charmbracelet/lipgloss).  
Elle permet de **gérer dynamiquement des plugins Go** (.so) à partir de dépôts GitHub ou de sources locales, tout en offrant un environnement ergonomique et interactif directement dans le terminal **Linux/Debian**.

![TUI](http://twilhem.github.io/Presentation/Presentation/image/GoTUI.png)

---

## Fonctionnalités principales

- **Gestion automatique des dépôts**
  → Lecture automatique d’un fichier `repo.conf` (JSON) dans le dossier de configuration.  
  → Chaque dépôt listé dans ce fichier est interrogé pour charger ses fichiers disponibles (plugins `.so`, scripts, etc.).  
  → Si aucun fichier `repo.conf` n’existe, GoTUI utilise par défaut le dépôt [`TWilhem/Plugin`](https://github.com/TWilhem/Plugin).

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

- **Barre de statut dynamique**
  → Affiche en permanence les actions en cours, les sélections, ou le plugin actuellement exécuté.

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

## Fichier `repo.conf` (optionnel)

Tu peux définir plusieurs dépôts de plugins personnalisés :  
Crée `~/.Plugin/repo.conf` avec par exemple :
```json
{
  "repos": [
    {
      "name": "Plugins officiels",
      "url": "https://api.github.com/repos/TWilhem/Plugin/contents/Plugin"
    },
    {
      "name": "Plugins internes",
      "url": "https://api.github.com/repos/MonOrganisation/Plugins/contents/"
    }
  ]
}
```

Chaque URL doit pointer vers une **API GitHub** retournant une liste JSON de fichiers.

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
| **Enter** | Télécharger / supprimer les plugins sélectionnés ou ouverture / fermeture du dossier repo |
| **e** | Exécuter le plugin sélectionné |
| **c** | Annuler la sélection |
| **Tab** | Changer de panneau (plugins / logs / TUI plugin) |
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
├── README.md            # Documentation
└── repo.conf            # Renseigne les depôts github
```

---

## Dépendances principales

- [Bubbletea](https://github.com/charmbracelet/bubbletea) → Framework TUI Go
- [Lipgloss](https://github.com/charmbracelet/lipgloss) → Styles et mise en page terminal
- [plugin](https://pkg.go.dev/plugin) → Chargement dynamique de modules Go

---

## Exemple de fonctionnement

1. Lancer `Pannel`
2. Les dépôts listés dans `repo.conf` (ou celui par défaut) s’affichent.  
3. Sélectionner un plugin avec **Espace**, puis valider avec **Enter** pour le télécharger.  
4. Appuyer sur **e** pour exécuter le TUI embarqué du plugin directement dans GoTUI.  
6. Suivre les logs dans le panneau inférieur

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
