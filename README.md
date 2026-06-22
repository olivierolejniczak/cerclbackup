# CerclBackup

Sauvegarde P2P chiffrée entre personnes de confiance.

---

## Objectif

La grande majorité des solutions de sauvegarde repose sur un tiers : un service cloud qui stocke vos données, un éditeur qui contrôle les clés, une infrastructure qui peut être compromise, revendue, ou coupée du jour au lendemain.

CerclBackup part d'une prémisse différente : **vous avez déjà les ressources pour sauvegarder vos données sans intermédiaire**. Vos proches — famille, amis, collègues de confiance — ont tous des disques durs avec de l'espace libre. Vous pouvez stocker des fragments de leurs fichiers, ils peuvent stocker des fragments des vôtres. C'est de la réciprocité, pas un service.

Le projet répond à trois questions concrètes :

- **Comment stocker ses fichiers chez ses proches sans qu'ils puissent les lire ?** → Chiffrement AES-256-GCM côté client, avant tout envoi. Personne d'autre que vous ne détient la clé.
- **Comment garantir la récupération même si plusieurs buddies disparaissent ?** → Reed-Solomon : un code correcteur d'erreurs qui reconstruit les données à partir d'un sous-ensemble de fragments.
- **Comment organiser cela sans infrastructure centralisée ?** → libp2p : protocole P2P utilisé par IPFS et Ethereum, sans serveur de coordination.

CerclBackup cible en priorité les **TPE/PME et familles** qui veulent reprendre le contrôle de leurs données sans compétences techniques avancées, et les **praticiens IT** qui déploient pour des tiers et ont besoin d'un outil auditable et sans dépendance cloud.

---

## Choix éthiques

### Pas de serveur central, pas de tiers de confiance

Il n'existe aucun serveur CerclBackup. Aucune entité ne stocke vos données, ne connaît vos buddies, ne peut être contrainte de livrer vos fichiers à un tiers. Le réseau n'existe que dans les connexions directes entre les pairs.

### Les fragments sont mathématiquement inutilisables seuls

Un buddy qui stocke vos shards ne peut pas les lire — pas parce qu'on lui fait confiance pour ne pas regarder, mais parce que chaque fragment est chiffré avec une clé dérivée que lui seul ne possède pas. Même s'il regroupait tous les fragments qu'il stocke, il n'obtiendrait que du bruit chiffré.

### Réciprocité, pas abonnement

Vous stockez pour vos buddies, ils stockent pour vous. Il n'y a pas d'argent, pas de tokens, pas de marketplace. Si la relation de confiance cesse, vous révoquez le buddy et les shards sont redistribués. Le modèle économique de CerclBackup ne peut pas reposer sur la captation de vos données parce qu'il ne les détient pas.

### AGPL-3.0 : copyleft fort

La licence n'est pas MIT ou Apache par choix délibéré. L'AGPL-3.0 impose que tout service SaaS construit sur CerclBackup publie son code source. Cela ferme la porte à un acteur commercial qui voudrait privatiser le code, le transformer en service fermé, et revendre l'accès à vos données. Si quelqu'un améliore CerclBackup, ces améliorations doivent rester ouvertes.

### Zéro télémétrie

CerclBackup ne contacte aucun serveur externe. Il n'y a pas d'analytique, pas de ping de démarrage, pas d'envoi de statistiques d'usage. Ce qui se passe sur votre machine reste sur votre machine.

---

## Choix technologiques et leurs justifications

### Reed-Solomon plutôt que la réplication simple

La réplication naïve ("j'ai deux copies") crée un overhead de 100% pour tolérer une seule panne. Reed-Solomon permet de tolérer *n* pannes avec un overhead bien inférieur, et offre une **garantie mathématique** de reconstruction — pas une probabilité.

Avec un schéma 3/2 (3 données + 2 parités) : 5 buddies, 2 défaillances tolérées, overhead de 67%. Avec de la réplication simple pour le même niveau de tolérance : il faudrait 3 copies complètes, soit 200% d'overhead.

Reed-Solomon est obligatoire dans CerclBackup — il n'existe pas de mode "miroir simple" parce que cela donnerait une fausse impression de sécurité avec une efficacité moindre.

### Chiffrement par shard, pas par fichier

Chaque fragment reçoit une clé dérivée via HKDF de la forme `HKDF(masterKey, fileID || shardIndex)`. Cela a deux conséquences :

1. Un buddy qui stocke deux fragments différents du même fichier ne peut pas savoir qu'ils appartiennent au même fichier (les clés sont différentes).
2. La compromission d'une clé de shard ne compromet pas les autres shards.

Le chiffrement est AES-256-GCM : authentifié (l'intégrité est vérifiée au déchiffrement), standard (auditables par des tiers), et résistant aux attaques actives.

### libp2p plutôt qu'un protocole maison

libp2p est le socle réseau d'IPFS et de plusieurs blockchains. Il gère l'identité des pairs (Ed25519), le multiplexage de protocoles, la traversée NAT, et le chiffrement de transport. Écrire un protocole P2P correct depuis zéro est un effort de plusieurs années — libp2p résout ces problèmes de façon battle-tested.

### mDNS TOFU plutôt que DHT

Le DHT Kademlia (la table de routage décentralisée) permettrait de trouver des pairs inconnus sur Internet. C'est précisément ce que CerclBackup ne veut pas. Le modèle de confiance est basé sur des relations préexistantes : vous ne voulez pas que votre machine contacte des inconnus.

mDNS découvre automatiquement les buddies déjà connus sur le réseau local (LAN) sans DHT, sans serveur de rendezvous, sans contacter qui que ce soit d'externe. La connexion ne s'établit que si le pair détecté figure déjà dans le registre de buddies — TOFU (Trust On First Use) sur invitation explicite.

### BIP39 pour les codes d'invitation

Les codes BIP39 (listes de mots utilisées pour les portefeuilles Bitcoin) sont conçus pour être pronounçables, partageables à l'oral, et résistants aux erreurs de transcription. Un code d'invitation CerclBackup peut être lu à voix haute au téléphone. C'est un choix d'accessibilité autant que de sécurité.

### Double canal pour l'invitation par email (Phase 2f)

L'invitation classique via code BIP39 suppose un canal sécurisé (Signal, QR code en face-à-face). L'email n'est pas un canal sécurisé. CerclBackup résout ce problème avec un schéma de commitment :

- **Canal email (public)** : payload signé Ed25519 contenant l'engagement cryptographique `C = SHA-256(S)` — lisible par n'importe qui, ne révèle rien.
- **Canal hors-bande (SMS, Signal, voix)** : les 12 mots BIP39 encodant le secret `S`.

Le joiner doit avoir les deux pour procéder. Intercepter l'email ne suffit pas. Intercepter le SMS ne suffit pas. C'est un MFA sur deux canaux de nature différente.

### Ed25519 plutôt que RSA

Clés 32 fois plus courtes, signatures déterministes (pas de side-channel par aléa de signature), pas d'attaque par oracle de padding, génération rapide. Ed25519 est devenu le standard de facto pour les systèmes P2P modernes.

### Argon2id pour le keystore

Le mot de passe maître dérive les clés via Argon2id (vainqueur de la Password Hashing Competition 2015). Argon2id est memory-hard : un attaquant avec du matériel GPU massif n'a pas d'avantage significatif par rapport à une machine ordinaire. C'est une protection concrète contre les attaques par dictionnaire sur le fichier keystore.

### Scrub proactif + Silent Revive

La plupart des systèmes de backup détectent la corruption au moment de la restauration — c'est-à-dire le pire moment possible. CerclBackup vérifie l'intégrité des shards stockés périodiquement (SHA-256 comparé à un sidecar créé à l'écriture). Si un shard est corrompu, il est silencieusement refetché depuis le propriétaire et réécrit — sans intervention humaine, avant que la corruption ne devienne un problème.

### Queue offline

Un buddy n'a pas besoin d'être en ligne au moment de la sauvegarde. Les shards non livrés sont mis en queue chiffrée sur disque. Quand le buddy revient en ligne (détecté par mDNS), la queue est vidée automatiquement. La sauvegarde ne bloque jamais sur la disponibilité d'un pair.

---

## Prérequis

```
Go 1.22 ou supérieur
```

## Installation

```bash
git clone https://github.com/olivierolejniczak/cerclbackup
cd cerclbackup
go mod tidy
```

## Compilation

```bash
# Linux / WSL2 (développement)
go build -o build/cerclbackup ./cmd/cerclbackup

# Windows 11 (cross-compilation)
GOOS=windows GOARCH=amd64 go build -o build/cerclbackup.exe ./cmd/cerclbackup

# NAS Synology/QNAP (ARM64)
GOOS=linux GOARCH=arm64 go build -o build/cerclbackup-arm64 ./cmd/cerclbackup
```

## Utilisation — CLI

### Démarrer le daemon (écoute P2P + scrub + mDNS)

```bash
cerclbackup serve --password <pwd>
```

### Inviter un buddy (code BIP39 à partager vocalement)

```bash
cerclbackup invite --password <pwd>
# → affiche 12 mots à lire à votre buddy
```

### Inviter par email avec MFA hors-bande

```bash
cerclbackup invite-email \
  --to ami@example.com \
  --circle "Famille" \
  --smtp-host smtp.gmail.com \
  --smtp-user moi@gmail.com \
  --smtp-pass "app-password" \
  --password <pwd>
# → envoie le payload par email
# → affiche les 12 mots à partager par SMS/Signal/voix
```

### Rejoindre un cercle

```bash
# Via code BIP39
cerclbackup join --peer-addr /ip4/192.168.1.10/tcp/4001/p2p/12D3K... \
  --words "word1 word2 ... word12" --password <pwd>

# Via email + code hors-bande
cerclbackup join-email --payload invite.json \
  --words "word1 word2 ... word12" --password <pwd>
```

### Sauvegarder un fichier

```bash
cerclbackup backup --src /chemin/vers/fichier.zip --password <pwd>
```

### Restaurer un fichier

```bash
cerclbackup restore --file-id <id> --out /chemin/sortie --password <pwd>
```

### Redistribuer après révocation d'un buddy

```bash
cerclbackup revoke --peer-id 12D3K... --password <pwd>
# → supprime le buddy et rebalance automatiquement vers les buddies restants
```

## Lancer les tests

```bash
go test ./... -v
```

51 tests couvrant : pipeline RS+AES complet, push/pull P2P, file d'attente offline, scrub + silent revive, rebalance, mDNS, invitation BIP39 et email MFA.

## Structure du projet

```
cerclbackup/
├── cmd/cerclbackup/         # CLI — toutes les commandes
├── internal/
│   ├── buddy/               # Registre des buddies + store des shards reçus
│   ├── chunker/             # Découpage en chunks de 4 MB
│   ├── codec/               # Reed-Solomon (klauspost/reedsolomon)
│   ├── crypto/              # AES-256-GCM, HKDF, Argon2id, Keystore
│   ├── emailinvite/         # Invitation email dual-canal (commitment + SMTP)
│   ├── invite/              # Tokens BIP39, commitments email
│   ├── manifest/            # Index chiffré des sauvegardes
│   ├── p2p/                 # libp2p : host, handlers, push/pull, queue, mDNS
│   ├── rebalance/           # Redistribution des shards après révocation
│   ├── scrub/               # Vérification périodique + silent revive
│   ├── storage/             # Store local des shards du propriétaire
│   └── watcher/             # Surveillance fsnotify (prévu Phase 3)
├── pkg/
│   ├── protocol/            # Types partagés (RSScheme, ManifestEntry…)
│   └── wire/                # Framing réseau (length-prefix + JSON)
├── pipeline_test.go         # Tests d'intégration bout-en-bout
└── ARCHITECTURE.md          # Document d'architecture complet
```

## Schémas Reed-Solomon

Reed-Solomon est obligatoire — il n'existe pas de mode miroir simple.

| Buddies | Schéma | Perte tolérée | Overhead stockage |
|---------|--------|---------------|-------------------|
| 3       | 2/1    | 1 buddy       | +50%              |
| 5       | 3/2    | 2 buddies     | +67%              |
| 8       | 5/3    | 3 buddies     | +60%              |
| 10      | 6/4    | 4 buddies     | +67%              |

## Roadmap

| Phase | Statut | Description |
|-------|--------|-------------|
| 1     | ✅ | Pipeline local — RS + AES + chunker + manifest + store |
| 2a    | ✅ | P2P invite/join, registre buddies, protocoles push/pull, queue offline |
| 2b    | ✅ | `backup` pousse vers les buddies ; `restore` récupère les shards manquants ; tests E2E RS+AES+P2P |
| 2c    | ✅ | Scrub SHA-256 + Silent Revive — détection et réparation silencieuse de la corruption |
| 2d    | ✅ | Rebalance — `revoke` redistribue automatiquement ; commande `rebalance` |
| 2e    | ✅ | mDNS LAN — découverte automatique des buddies connus, flush queue à la reconnexion |
| 2f    | ✅ | Invitation email MFA dual-canal — payload signé par email + 12 mots OOB |
| 2g    | 🔲 | Recovery Phrase — identité déterministe (BIP39), restauration sur nouvelle machine |
| 2h    | 🔲 | DHT + UDP hole punching — buddies sur Internet, traversée NAT |
| 2i    | 🔲 | Manifest distribué — les buddies conservent une copie chiffrée du manifest |
| 3     | 🔲 | UI Windows (systray, installeur WiX, cercles multiples, versioning) |

## Licence

AGPL-3.0 — voir `LICENSE`.

Toute utilisation dans un service en ligne impose la publication du code source modifié.
