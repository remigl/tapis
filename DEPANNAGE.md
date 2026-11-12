# Dépannage

[English](TROUBLESHOOTING.md)

Commence par la liste de vérification : elle règle la plupart des problèmes en une minute. Cherche ensuite ton symptôme plus bas.

## Vérification rapide

1. Tapis tourne sur l'ordinateur et sa fenêtre affiche `Firewall: OK` (Windows).
2. Le téléphone est sur le **même réseau Wi-Fi** que l'ordinateur (pas en données mobiles, pas sur un réseau « invité ») et son **VPN est coupé** ou autorise le réseau local.
3. Tu ouvres l'adresse affichée par Tapis (`https://192.168.x.x:47800`) **dans Chrome** (Android) ou **Safari** (iPhone).
4. Sur l'avertissement de certificat, tu as touché **Paramètres avancés → Continuer**.

Pour tester l'ordinateur seul, ouvre `https://localhost:47800` dans un navigateur de l'ordinateur. Si la page sombre de Tapis s'affiche, Tapis fonctionne et le problème se situe entre le téléphone et l'ordinateur.

---

## La page reste blanche, charge sans fin, ou « Ce site est inaccessible »

Le téléphone n'arrive pas à joindre l'ordinateur. Causes, de la plus à la moins probable :

**Le pare-feu Windows bloque Tapis.** Depuis la version 0.1.1, Tapis configure lui-même le pare-feu : au premier lancement, Windows demande l'autorisation administrateur, réponds **Oui**. Si tu as répondu Non, ou si la fenêtre affiche `Firewall: NOT configured`, ferme Tapis et relance-le pour retrouver la demande.
La règle créée est très restreinte : ce programme seulement, son port seulement, et uniquement depuis ton réseau local. Elle est donc sûre sur n'importe quel réseau, même classé « Public ».
Correction manuelle (PowerShell **en administrateur**, adapte le chemin) :
```powershell
New-NetFirewallRule -DisplayName "Tapis (TCP 47800, local network)" -Direction Inbound -Action Allow `
  -Program "C:\chemin\vers\tapis.exe" -Protocol TCP -LocalPort 47800 -RemoteAddress LocalSubnet -Profile Any
```
Supprime aussi toute règle au nom du programme Tapis qui le **bloque** dans *Pare-feu Windows Defender → Paramètres avancés → Règles de trafic entrant* : une règle de blocage l'emporte toujours.

**Un autre logiciel de sécurité filtre le réseau** (Norton, McAfee, Kaspersky, Bitdefender, ESET…). Il remplace le pare-feu Windows : autorise `tapis.exe` pour le réseau local dans ses réglages.

**Le téléphone n'est pas sur le même réseau.** Vérifie que le Wi-Fi est activé et connecté au même réseau que l'ordinateur. Les réseaux invités, d'hôtel, de campus et certains Wi-Fi de café isolent les appareils entre eux (« isolation des clients ») : Tapis ne peut pas y fonctionner. Utilise plutôt le partage de connexion du téléphone : connecte l'ordinateur au point d'accès du téléphone, puis relance Tapis (une nouvelle adresse s'affiche).

**Tapis affiche une mauvaise adresse.** Compare-la avec l'adresse IPv4 de ta carte Wi-Fi/Ethernet donnée par `ipconfig` (Windows) ou `ip addr` (Linux). Si l'ordinateur a plusieurs réseaux, Tapis les liste tous sous « other network » : essaie chacun.

**Un VPN est actif** sur l'ordinateur ou le téléphone. Certains VPN bloquent le réseau local : désactive-le, ou active « autoriser l'accès au réseau local » dans ses réglages.

**Pare-feu Linux.** Avec ufw : `sudo ufw allow from 192.168.0.0/16 to any port 47800 proto tcp` (adapte à ton réseau). Avec firewalld : `sudo firewall-cmd --zone=home --add-port=47800/tcp --permanent && sudo firewall-cmd --reload`, et vérifie que ton Wi-Fi est dans la zone `home`.

## Ça marche en Wi-Fi mais pas quand le VPN du téléphone est actif

Un VPN envoie tout le trafic du téléphone dans un tunnel chiffré vers un serveur distant, y compris celui destiné à ton propre réseau : le téléphone ne peut donc plus joindre l'ordinateur. Aucun site web ne peut contourner le VPN du téléphone, mais toutes les bonnes applis VPN ont une option pour ça :

- **Proton VPN :** Paramètres → *Connexions LAN* (autoriser les connexions LAN).
- **NordVPN :** *Split tunneling* → exclure Chrome.
- **Surfshark, Mullvad, ExpressVPN, WireGuard et autres :** cherche *Partage du réseau local*, *Autoriser le LAN*, *Contourner le LAN* ou *Split tunneling* (exclure Chrome).
- Le **DNS privé** d'Android ne pose pas ce problème, seuls les VPN le font.

Après avoir changé le réglage, recharge la page Tapis.

## « Confirme sur l'ordinateur » s'affiche et rien ne se passe

Depuis la 0.2.0, chaque nouveau téléphone doit être autorisé sur l'ordinateur : dans la fenêtre Tapis, tape `y` puis Entrée (ou `n` pour refuser). Sans réponse en une minute, l'appairage est refusé. Si Tapis tourne sans fenêtre (service Linux), lance `tapis --pair` dans un terminal pour appairer.

## Verrouiller ou mettre en veille ne fait rien (Linux)

Le verrouillage utilise `loginctl lock-session` (ou `xdg-screensaver lock`) et la veille `systemctl suspend`. Vérifie qu'ils fonctionnent tapés dans un terminal ; certains bureaux ont besoin d'un verrouilleur d'écran installé.

## La page reste blanche juste après le scan, mais marche quand je tape l'adresse dans Chrome

L'appareil photo a ouvert le lien dans sa visionneuse intégrée, qui ne sait pas afficher l'avertissement de certificat. Dans l'appareil photo ou Google Lens, choisis **Ouvrir dans Chrome** (menu ⋮), ou recopie l'adresse affichée par Tapis dans Chrome.

## « Votre connexion n'est pas privée » / NET::ERR_CERT_AUTHORITY_INVALID

C'est normal la première fois : le certificat a été créé par ton ordinateur, pas par une autorité publique. Touche **Paramètres avancés → Continuer vers…**. Le navigateur s'en souvient.
Tu le reverras quand l'adresse de l'ordinateur change (autre réseau) : c'est normal. S'il réapparaît **sur le même réseau** sans que tu aies réinitialisé Tapis, ne continue pas : quelqu'un intercepte peut-être la connexion.
Pour t'en débarrasser définitivement, installe le certificat de Tapis sur le téléphone : voir [README › Sécurité](README.fr.md#sécurité).

## « Appairage refusé » : le code a déjà servi ou a expiré

Un QR code ne sert qu'une fois et pendant 5 minutes. Dans la fenêtre Tapis, tape `p` puis Entrée, et scanne le nouveau code.

## « Pas encore appairé » en ouvrant l'adresse

Ce navigateur n'a jamais été appairé (ou ses données ont été effacées, ou tu utilises un autre navigateur ou un onglet privé). Affiche un nouveau code avec `p` + Entrée et scanne-le. Chaque navigateur s'appaire séparément.

## « Téléphone désappairé »

Le téléphone a été retiré avec `tapis revoke` ou `r <id>`, ou le dossier de configuration a été supprimé. Appaire-le de nouveau avec un nouveau code.

## « Ordinateur injoignable » alors que ça marchait

Tapis a été fermé, l'ordinateur s'est mis en veille, ou un des appareils a changé de réseau. Relance Tapis, réveille l'ordinateur, vérifie le Wi-Fi, puis touche **Réessayer**.

## Le titre du média ne s'affiche pas

Tapis affiche ce que le système lui-même sait être en lecture (comme l'encart du volume de Windows ou le widget média de Linux). Si rien n'apparaît :

- **Les navigateurs** (YouTube, Netflix, etc. dans Chrome, Edge, Firefox) fonctionnent. Dans Firefox, `media.hardwaremediakeys.enabled` doit rester activé (par défaut).
- **VLC 3 sous Windows** ne signale pas ce qu'il lit à Windows. VLC 4, les applis Films et TV / Lecteur multimédia, Spotify et les navigateurs le font. Sous Linux, VLC fonctionne.
- **Les diaporamas** (PowerPoint, visionneuses de photos) ne sont pas des lecteurs média pour le système : rien ne s'affiche, c'est normal.
- Sous Linux, le titre est lu via `busctl` (fourni avec systemd).

## L'icône de Tapis n'est pas visible (Windows)

Tapis tourne dans la zone de notification : clique sur la flèche `^` à côté de l'horloge pour voir les icônes cachées. Glisse l'icône de Tapis sur la barre des tâches pour la garder visible. Relancer `tapis.exe` affiche la fenêtre du QR code.

## Je peux bouger le pointeur sur l'écran de verrouillage mais pas taper (Windows)

C'est Windows qui protège l'écran de verrouillage. Clic droit sur l'icône de Tapis → **Taper sur l'écran de verrouillage**, et accepte la demande administrateur. Si c'était déjà activé et que ça ne marche plus après une mise à jour, décoche puis recoche pour mettre l'assistant à jour.

## Le pointeur bouge mais certaines fenêtres ne réagissent pas (Windows)

Windows empêche les programmes normaux de contrôler ceux lancés **en administrateur** (Gestionnaire des tâches, installateurs, certains jeux), ainsi que Ctrl+Alt+Suppr et les demandes UAC. Lance Tapis en administrateur si tu dois contrôler ces fenêtres. Pour l'écran de verrouillage, voir plus haut.

## De mauvais caractères sont tapés (Linux)

Tapis doit connaître ta disposition clavier. Il la lit dans le système ; s'il se trompe, lance-le avec `--layout be` (AZERTY belge), `--layout fr` (AZERTY français) ou `--layout us`.
Les caractères absents de ta disposition sont tapés avec Ctrl+Maj+U : ça marche dans la plupart des applications GTK/Qt, mais pas partout.

## « cannot open /dev/uinput: permission denied » (Linux)

Installe la règle udev une fois, puis déconnecte-toi et reconnecte-toi :
```sh
sudo cp packaging/linux/60-tapis-uinput.rules /etc/udev/rules.d/
echo uinput | sudo tee /etc/modules-load.d/uinput.conf
sudo modprobe uinput && sudo udevadm control --reload && sudo udevadm trigger
```

## « port 47800 is already in use »

Tapis tourne déjà (dans une autre fenêtre ou en arrière-plan), ou un autre programme utilise ce port. Ferme l'autre Tapis, ou lance-le avec `--port 47801` et appaire de nouveau.

## Windows affiche « Windows a protégé votre ordinateur » (SmartScreen) ou l'antivirus avertit

Tapis n'est pas signé avec un certificat payant, donc Windows ne le connaît pas. Clique **Informations complémentaires → Exécuter quand même**. Tu peux vérifier le fichier avec `SHA256SUMS` dans la release, ou le compiler toi-même depuis le code source.

## La fenêtre se ferme tout de suite (Windows)

Depuis la version 0.1.1, la fenêtre reste ouverte et affiche l'erreur. Avec une version plus ancienne, ouvre un terminal (`cmd`), va dans le dossier et lance `tapis.exe` depuis là pour lire le message.

## L'écran du téléphone s'éteint pendant l'utilisation

Garder l'écran allumé nécessite un certificat de confiance. Installe le certificat de Tapis (voir README › Sécurité) ou allonge la mise en veille de l'écran dans les réglages du téléphone.

---

Toujours bloqué ? [Ouvre une issue](https://github.com/remigl/tapis/issues) en indiquant : ton système (version de Windows/Linux), ce qu'affiche la fenêtre Tapis et ce que tu vois sur le téléphone. Ne publie jamais ton lien d'appairage ni le contenu du dossier de configuration.
