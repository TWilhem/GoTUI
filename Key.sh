Key() {
    TMPFILE=/tmp/CleSshActif.txt

    # Démarre ssh-agent si nécessaire
    if [ -z "$SSH_AUTH_SOCK" ]; then
        eval "$(ssh-agent -s)"
    fi

    # Vérifie que dialog est installé
    if ! command -v dialog >/dev/null 2>&1; then
        echo "Veuillez installer dialog : sudo apt install dialog"
        return 1
    fi

    # Toutes les clés disponibles
    all_keys=($(ls -1 ~/.ssh/ | grep -vE "(\.pub$|\.old$|known_hosts$)"))

    # Clés déjà actives
    if [[ -f $TMPFILE ]]; then
        active_keys=($(cat "$TMPFILE"))
    else
        active_keys=()
    fi

    # Construire la liste pour dialog
    menu_items=()
    for k in "${all_keys[@]}"; do
        if [[ " ${active_keys[*]} " == *" $k "* ]]; then
            menu_items+=("$k" "" "on")
        else
            menu_items+=("$k" "" "off")
        fi
    done

    # Lancer le menu interactif checkbox
    selected=$(dialog --clear --stdout --checklist \
        "Sélectionnez les clés SSH (Espace pour basculer, Entrée pour valider) :" 20 60 15 \
        "${menu_items[@]}")

    # Nettoyer l’écran après dialog
    clear

    # Sauvegarder les clés sélectionnées
    if [[ -n "$selected" ]]; then
        printf "%s\n" $selected > "$TMPFILE"
    else
        > "$TMPFILE"
    fi

    echo
    echo "Activation des clés dans ssh-agent…"

    # Activer les clés sélectionnées
    for k in $selected; do
        ssh-add ~/.ssh/"$k" 2>/dev/null
    done

    # Désactiver les clés non sélectionnées
    for k in "${all_keys[@]}"; do
        if ! [[ " $selected " =~ " $k " ]]; then
            ssh-add -d ~/.ssh/"$k" 2>/dev/null
        fi
    done

    # Afficher le résultat
    echo
    cat "$TMPFILE"
}
