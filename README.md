# CerclBackup

Sauvegarde P2P chiffrée entre personnes de confiance.

## Philosophie

- Aucun serveur central ne détient les données
- Chaque buddy ne stocke que des fragments chiffrés **inutilisables seuls**
- Reed-Solomon (toujours obligatoire, jamais de simple miroir) garantit la
  reconstruction même si des buddies disparaissent
- AES-256-GCM : les clés ne quittent jamais la machine du propriétaire
- Zéro dépendance à une infrastructure permanente

## Prérequis

```
Go 1.22 ou supérieur
```

## Installation des dépendances

```bash
go mod tidy
```

## Compilation

### Linux (développement, WSL2)

```bash
go build -o build/cerclbackup-linux ./cmd/cerclbackup
```

### Windows 11 (cross-compilation depuis WSL2/Linux)

```bash
GOOS=windows GOARCH=amd64 go build -o build/cerclbackup.exe ./cmd/cerclbackup
```

### NAS Synology/QNAP (ARM64)

```bash
GOOS=linux GOARCH=arm64 go build -o build/cerclbackup-arm64 ./cmd/cerclbackup
```

## Utilisation (Phase 1 — CLI local)

### Sauvegarder un fichier

```bash
./cerclbackup backup \
  --src "/chemin/vers/rapport.docx" \
  --password "mon-mot-de-passe" \
  --buddies 5
```

### Lister les fichiers sauvegardés

```bash
./cerclbackup list --password "mon-mot-de-passe"
```

### Restaurer un fichier

```bash
./cerclbackup restore \
  --file-id "a1b2c3d4e5f6a7b8" \
  --out "/chemin/restauration/rapport.docx" \
  --password "mon-mot-de-passe"
```

## Lancer les tests

```bash
go test ./... -v
```

## Structure du projet

```
cerclbackup/
├── cmd/cerclbackup/    # CLI Phase 1
├── internal/
│   ├── chunker/        # Découpage fichiers (4 MB chunks)
│   ├── codec/          # Reed-Solomon (klauspost/reedsolomon)
│   ├── crypto/         # AES-256-GCM + Argon2id + Keystore
│   ├── manifest/       # Index chiffré des sauvegardes
│   ├── storage/        # Store local des shards
│   └── watcher/        # Surveillance fsnotify
├── pkg/protocol/       # Types partagés
├── pipeline_test.go    # Tests d'intégration
└── ARCHITECTURE.md     # Document d'architecture complet
```

## Schémas Reed-Solomon

Reed-Solomon est toujours obligatoire — minimum 3 buddies/appareils requis,
aucun fallback en simple miroir 1/1.

| Buddies | Schéma | Perte tolérée | Overhead |
|---------|--------|---------------|----------|
| 3       | 2/1    | 1 buddy       | +50%     |
| 5       | 3/2    | 2 buddies     | +67%     |
| 8       | 5/3    | 3 buddies     | +60%     |
| 10      | 6/4    | 4 buddies     | +67%     |

## Roadmap

Voir `ARCHITECTURE.md` pour le détail complet des phases.

- **Phase 1** ✅ Pipeline local (chunker + RS + AES + store + manifest)
- **Phase 2a** ✅ P2P invite/join, buddy registry, push/pull protocol, offline queue
- **Phase 2b** ✅ Backup pushes shards to buddies; restore fetches missing shards; full RS+AES+P2P pipeline tested
- **Phase 2c** ✅ Scrub — SHA-256 shard verification; Silent Revive re-fetches corrupted shards from owner
- **Phase 2d** ✅ Rebalance — `revoke` auto-redistributes to surviving buddies; `rebalance` command for manual passes
- **Phase 2e** ✅ mDNS LAN discovery — auto-connect to known buddies on LAN; addresses persisted; offline queue flushed on peer found
- **Phase 2f** 🔲 Email invites, MFA
- **Phase 3** 🔲 UI Windows (systray, installeur, cercles multiples, versioning)

## Licence

AGPL-3.0 — voir `LICENSE`.
