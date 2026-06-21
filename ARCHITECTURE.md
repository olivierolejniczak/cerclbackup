# CerclBackup — Document d'Architecture

> Version : 0.7  
> Date : 2026-06-20  
> Statut : Conception — Phase 1 livrée (code mis à jour : RS obligatoire), Phase 2 en cours de spécification  
> Licence : AGPL-3.0

---

## 1. Vision

CerclBackup est un logiciel de sauvegarde P2P pour Windows destiné au grand public.
Il permet de sauvegarder ses fichiers sur les machines de ses amis, famille, collègues,
ou ses propres appareils, sans serveur central, sans abonnement, et sans qu'aucun buddy
ne puisse lire les données sauvegardées.

**Principes fondateurs :**
- Installation one-click, zéro configuration réseau
- Aucun serveur central ne détient les données
- Chaque buddy ne stocke que des fragments chiffrés inutilisables seuls
- La perte de buddies ne cause pas de perte de données (Reed-Solomon)
- Alertes proactives si les données deviennent à risque
- Plusieurs cercles indépendants pour isoler les données par domaine de vie
- Toute cryptographie est invisible pour l'utilisateur

**Positionnement vs Syncthing :**

| Aspect | Syncthing | CerclBackup |
|---|---|---|
| Modèle | Synchronisation | Sauvegarde avec versioning |
| Suppression accidentelle | Propagée partout | Protégée — versions conservées |
| Ransomware | Propagé partout | Versions antérieures récupérables |
| Buddies sociaux | Non | Oui |
| Chiffrement au repos | Non natif | AES-256-GCM |
| Alerte de risque | Non | Oui — temps réel |
| Grand public | Non | Oui — objectif core |

> *"Syncthing synchronise vos fichiers. CerclBackup protège vos souvenirs."*

**Contexte :**
CerclBackup est publié sous licence AGPL-3.0. Cette licence garantit que toute
acquisition par un tiers ne peut pas fermer le projet — la communauté peut toujours
forker. Protection explicite contre le modèle "acquire to kill" subi par BuddyBackup.

---

## 2. Deux Modes de Fonctionnement

### Mode Cercle Social (buddies = personnes de confiance)

```
Alice ←→ Bob ←→ Carol ←→ Dave
Réciprocité : je stocke pour toi, tu stockes pour moi
Confiance : réseau d'amis, famille, collègues
```

- Enforcement de réciprocité actif (±20% de tolérance)
- Invitation par email + code vocal (voir section 6 (invitation email))
- Chunks data vers buddies directs, chunks parity vers cercle étendu

### Mode Cercle Personnel (buddies = mes propres appareils)

```
PC bureau ←→ NAS Synology ←→ VPS IONOS ←→ Laptop
Réciprocité : désactivée (tout m'appartient)
Confiance : absolue
```

- Authentification via Master Device Key (dérivée du mot de passe maître)
- Appairage par QR code ou code court affiché sur les deux écrans
- Quota asymétrique : chaque device contribue ce qu'il peut
- Binaire Go compilé pour ARM64 → compatible NAS Synology/QNAP natif

### Mode Hybride (recommandé)

```
Mes propres devices (3) + buddies sociaux (2)
→ schéma RS 5/8
→ devices personnels reçoivent les chunks data (priorité)
→ buddies sociaux reçoivent les chunks parity
→ résiste à un incendie physique détruisant tous mes appareils
```

---

## 3. Cercles Multiples — Isolation par Domaine de Vie

Un utilisateur peut créer plusieurs cercles indépendants, chacun avec ses propres
buddies, son propre schéma RS, et sa propre clé dérivée.

### Exemple de configuration typique

```
Cercle "Famille"
  → Contenu : Photos, vidéos, souvenirs
  → Buddies : NAS maison, parents, frère
  → Schéma RS : 3/5
  → Criticité : haute (irremplaçable)

Cercle "Travail"
  → Contenu : Documents professionnels, projets
  → Buddies : VPS IONOS, collègue de confiance
  → Schéma RS : 2/3
  → Criticité : haute (confidentialité requise)

Cercle "Privé"
  → Contenu : Données sensibles, clés, mots de passe
  → Buddies : mes appareils uniquement (minimum 2)
  → Schéma RS : 2/1 minimum (jamais de simple miroir 1/1)
  → Criticité : maximale (isolation totale)
```

### Propriétés d'isolation

- **Clé dérivée distincte par cercle :**
  ```
  Argon2id(password + cercle_id + salt) → Master Key du cercle
  ```
  Un buddy du cercle Famille ne peut jamais déchiffrer les chunks du cercle Travail.

- **Manifest séparé par cercle** — chiffré avec la clé du cercle
- **Un buddy peut appartenir à plusieurs cercles** avec des quotas distincts
- **Compromission d'un cercle** → les autres cercles restent intacts
- **Schéma RS indépendant** → criticité adaptée par domaine

### UI Cercles Multiples

```
Systray
  ├── Cercle "Famille"   ✅ 3/5 buddies en ligne
  ├── Cercle "Travail"   🟡 1/3 buddies en ligne — attention
  ├── Cercle "Privé"     ✅ 2/2 appareils en ligne
  └── + Créer un cercle
```

### Suppression d'un cercle — double confirmation obligatoire

Décision validée (revue pré-lancement, juin 2026) : supprimer un cercle
déclenche la suppression des shards correspondants chez tous les buddies
associés. C'est une action destructive et irréversible — aucune suppression
ne doit pouvoir se produire en un seul clic, y compris lors de manipulations
de test.

```
[Supprimer le cercle "Test"]
        ↓
┌─────────────────────────────────────────────────┐
│ ⚠️ Ceci va supprimer définitivement :            │
│    47 fichiers — 12.3 Go                         │
│    chez 3 buddies (Bob, Carol, NAS)              │
│                                                   │
│ Cette action est irréversible.                   │
│                                                   │
│ Tape le nom du cercle pour confirmer :           │
│ [________________]                              │
│                                                   │
│         [Annuler]      [Supprimer définitivement]│
└─────────────────────────────────────────────────┘
```

La confirmation par saisie du nom (pas juste un bouton "Oui/Non") évite les
clics accidentels — modèle identique à la suppression de dépôt sur GitHub.

---

## 4. Système d'Alertes de Risque

### Principe

CerclBackup surveille en permanence la disponibilité des buddies et calcule,
fichier par fichier, si une restauration reste possible. L'utilisateur est alerté
**avant** de perdre ses données — pas après.

### Trois niveaux de risque

```
🟢 SÉCURISÉ
   Shards disponibles > seuil RS + marge
   "Toutes tes données sont protégées"

🟡 ATTENTION
   Shards disponibles == seuil RS minimum (juste au seuil)
   "2 buddies hors ligne. Encore sécurisé,
    mais 1 buddy supplémentaire hors ligne
    mettra certaines données en risque."
   → Notification discrète en systray

🔴 CRITIQUE
   Shards disponibles < seuil RS minimum
   "rapport.docx ne peut plus être restauré
    si Bob reste hors ligne."
   → Notification immédiate + email à l'utilisateur
   → Actions proposées automatiquement
```

### Calcul du risque

```go
type RiskLevel int

const (
    RiskNone     RiskLevel = iota // Tous buddies OK + marge
    RiskWarning                   // Exactement au seuil RS
    RiskCritical                  // En dessous du seuil RS
    RiskLost                      // Irrécupérable définitivement
)

func ComputeFileRisk(entry ManifestEntry, onlineBuddies []string) RiskLevel {
    available := countAvailableShards(entry.Shards, onlineBuddies)
    margin := available - entry.Scheme.DataShards
    switch {
    case margin > 1:
        return RiskNone
    case margin == 1:
        return RiskWarning
    case margin <= 0:
        return RiskCritical
    }
}
```

### Actions automatiques proposées en cas de risque

```
🔴 "Photos/Noël2025.mp4 en risque critique"
   ┌─────────────────────────────────────────────┐
   │ Bob est hors ligne depuis 8 jours.          │
   │ Ce fichier n'est plus restaurable.          │
   │                                             │
   │ [📥 Télécharger maintenant]                 │
   │ [👤 Inviter un nouveau buddy]               │
   │ [🔄 Redistribuer vers buddies actifs]       │
   │ [⏰ Me rappeler dans 3 jours]              │
   └─────────────────────────────────────────────┘
```

### Health Check périodique

CerclBackup vérifie régulièrement l'intégrité des shards distants :

```
Toutes les 24h :
  → Demande à chaque buddy : "as-tu toujours le shard X ?"
  → Buddy répond avec le SHA-256 du shard stocké
  → CerclBackup compare avec le hash attendu dans le manifest
  → Si divergence → redistribution automatique depuis d'autres buddies
```

Détecte : corruption silencieuse, suppression involontaire, défaillance disque buddy.

---

## 5. Versioning des Fichiers

### Principe

CerclBackup conserve N versions antérieures de chaque fichier, pas seulement
la dernière. Protection complète contre ransomware, suppression accidentelle,
et corruption silencieuse.

### Politique de rétention (configurable par cercle)

```
Par défaut :
  → Versions des 30 derniers jours : toutes conservées
  → Entre 30 et 90 jours : 1 version par semaine
  → Au-delà de 90 jours : 1 version par mois
  → Maximum : 50 versions par fichier

Exemple pour rapport.docx :
  rapport.docx — v23 (aujourd'hui)        ← actuelle
  rapport.docx — v22 (hier)
  rapport.docx — v21 (avant-hier)
  ...
  rapport.docx — v1  (il y a 6 mois)
```

### Déduplication entre versions

Les chunks identiques entre deux versions ne sont stockés qu'une seule fois
(hash-based deduplication). Un fichier modifié à 10% ne génère que 10% de
nouveaux chunks.

### Restauration d'une version antérieure

```
UI : clic droit sur un fichier → "Historique des versions"
  → Liste des versions avec date, taille, différence
  → [Restaurer cette version] [Comparer] [Télécharger]
```

### Protection ransomware explicite

```
Scénario ransomware :
  J-0 : ransomware chiffre tous les fichiers sur le PC
  J-0 : Watcher détecte les modifications → nouvelles versions créées
        (les versions chiffrées par le ransomware)
  J-0 : L'utilisateur voit l'alerte CerclBackup : "1847 fichiers modifiés
        en 3 minutes — activité inhabituelle détectée"
  J-0 : [Pause automatique de la sync] proposée
  J+1 : Restauration depuis la version J-1 → données récupérées ✅
```

### Volume Shadow Copy (VSS) — lecture cohérente des fichiers verrouillés

Décision validée (revue pré-lancement, juin 2026) : sans VSS, tout fichier
verrouillé par une autre application au moment de la sauvegarde (base Outlook
`.pst` ouverte, fichier de VM en cours d'exécution, base SQLite verrouillée)
échoue silencieusement ou produit une copie incohérente. C'est un point
bloquant avant toute release Windows, identique à ce qu'implémentent tous
les outils de backup sérieux (Veeam, Acronis, Windows Backup natif).

```
Watcher détecte une modification sur fichier.pst (verrouillé par Outlook)
        ↓
[VSS Manager] crée un instantané (snapshot) du volume via l'API VSS Windows
        ↓
Le Chunker lit depuis le snapshot, pas depuis le fichier live
        ↓
État cohérent garanti, même si Outlook continue d'écrire pendant la lecture
        ↓
Snapshot libéré après lecture complète
```

```go
// internal/vss/vss.go — Windows uniquement (no-op sur Linux/NAS)
type ShadowCopy interface {
    CreateSnapshot(volume string) (snapshotPath string, err error)
    ReleaseSnapshot(snapshotPath string) error
}
```

Sur NAS Synology/QNAP (Linux), l'équivalent est un snapshot Btrfs/LVM si
disponible, sinon un fallback en lecture directe avec avertissement explicite
si le fichier est verrouillé.

### Résolution de conflits — modifications concurrentes hors ligne

Décision validée (revue pré-lancement, juin 2026) : en Mode Cercle Personnel,
deux appareils peuvent modifier le même fichier pendant qu'ils sont tous deux
hors ligne l'un de l'autre (ex : PC et laptop modifiés séparément en
déplacement). Sans détection explicite, la version qui se synchronise en
dernier écraserait silencieusement l'autre.

```
PC (hors ligne)      modifie rapport.docx → v23-PC
Laptop (hors ligne)  modifie rapport.docx → v23-Laptop
Les deux reviennent en ligne
        ↓
[Conflict Detector] compare les hash de la version commune (v22) :
   v23-PC      diverge de v22
   v23-Laptop  diverge de v22 différemment
        ↓
Conflit détecté → AUCUNE version n'écrase l'autre automatiquement
        ↓
rapport.docx (conservé — dernière version synchronisée)
rapport (conflit du PC bureau, 19/06/2026 14h32).docx  ← copie de conflit
        ↓
Notification : "Conflit détecté sur rapport.docx — les deux versions
                ont été conservées, à toi de les fusionner."
```

Modèle identique à la gestion de conflits de Syncthing ("sync conflict
copy") — aucune perte de données, la décision de fusion reste humaine.

---

## 6. Compression & Déduplication

### Contrainte d'ordre dans le pipeline

AES-256-GCM produit un flux pseudo-aléatoire sans redondance statistique.
Compresser après chiffrement n'apporte aucun gain (parfois une perte). L'ordre
du pipeline est donc contraint :

```
❌ Chunk → RS encode → AES encrypt → Compress    (inutile, ciphertext incompressible)
✅ Chunk → Compress → RS encode → AES encrypt    (efficace)
```

```
fichier.docx
        ↓
[Chunker] découpe en chunks 4 MB
        ↓
[Compressor] zstd niveau 3 — uniquement si gain détecté
        ↓ chunk compressé (taille variable, padding géré par Chunk.Size)
[RS Encoder] schéma adaptatif
        ↓
[Encryptor] AES-256-GCM
        ↓
Distribution buddies
```

### Compression sélective par contenu réel (pas par extension)

De nombreux formats sont déjà compressés en interne — recompresser n'apporte
rien et coûte du CPU pour rien. C'est notamment le cas des formats Office
modernes (`.docx`, `.xlsx`, `.pptx` sont des archives ZIP en interne).

```go
// internal/compress/compress.go
func ShouldCompress(data []byte) bool {
    sample := data[:min(len(data), 64*1024)]
    compressed := zstdCompress(sample)
    ratio := float64(len(compressed)) / float64(len(sample))
    return ratio < 0.9 // ne compresse que si gain réel > 10%
}

func Compress(data []byte) ([]byte, error)
func Decompress(data []byte, originalSize int) ([]byte, error)
```

| Type de contenu | Gain typique | Décision |
|---|---|---|
| Texte, JSON, CSV, logs | 40-70% | Compresser |
| PDF | 5-20% (variable) | Tester puis décider |
| `.docx`/`.xlsx`/`.pptx` (déjà ZIP) | ~0-5% | Généralement skip |
| JPEG, MP4, ZIP, 7z, MP3 | ~0% | Skip systématique |

### Trois niveaux de déduplication

**Niveau 1 — Inter-versions du même fichier**

Déjà couvert en section 5 (Versioning) : les chunks identiques entre deux
versions successives d'un même fichier ne sont stockés qu'une fois.

```
rapport_v22.docx vs rapport_v23.docx
→ 90% des chunks identiques (même hash SHA-256)
→ seuls les chunks modifiés sont re-uploadés
```

**Niveau 2 — Inter-fichiers, même utilisateur, même cercle**

Détection de doublons entre fichiers différents possédant les mêmes chunks
(ex. un même PDF rangé dans deux dossiers).

```go
type ChunkRef struct {
    Hash     [32]byte // clé de dédup, locale au cercle
    RefCount int       // nombre de fichiers/versions pointant vers ce chunk
}
```

Un chunk identique référencé par plusieurs fichiers n'est stocké physiquement
qu'une seule fois chez les buddies, avec un compteur de références dans le
manifest du cercle.

**Niveau 3 — Inter-utilisateurs : volontairement exclu**

La déduplication entre cercles ou entre utilisateurs différents (convergent
encryption — clé dérivée du hash du contenu en clair) est une fonctionnalité
**délibérément absente** de CerclBackup, pour une raison de sécurité connue :

```
Convergent encryption permettrait de détecter si deux utilisateurs
possèdent le même fichier — y compris sans jamais le déchiffrer.

→ Attaque "confirmation of file" : un attaquant connaissant le hash
  d'un fichier sensible (ou illégal) peut tester sa présence chez
  un tiers simplement en observant si la dédup se déclenche.
```

C'est la faille qui a valu des critiques publiques à plusieurs services cloud
grand public par le passé. Chaque cercle CerclBackup utilise une clé dérivée
distincte (section 3), ce qui rend cette dédup impossible par construction —
la frontière du cercle est aussi la frontière de la déduplication.

### Impact combiné sur l'overhead Reed-Solomon

```
Fichier original             : 100 MB
Après compression (zstd)     :  60 MB   (-40%, fichiers texte/office)
Après dédup (40% redondant)  :  40 MB   (versions + doublons internes)
Après RS 5/8 (+60%)          :  64 MB   stockés chez les buddies

Sans compression ni dédup    : 160 MB   stockés chez les buddies
```

Gain net estimé : environ 60% d'espace économisé chez les buddies malgré
l'overhead de redondance Reed-Solomon.

### Évolution du type Chunk

```go
// pkg/protocol/messages.go
type Chunk struct {
    Index        int
    Data         []byte
    Hash         [32]byte
    Size         int
    Compressed   bool // nouveau
    OriginalSize int  // nouveau — requis pour décompresser correctement
}
```

---

## 7. Intégrité et Auto-Réparation — Principes Inspirés de ZFS

Décision validée (revue pré-lancement, juin 2026) : CerclBackup adopte trois
principes du système de fichiers ZFS, adaptés au contexte distribué et
non fiable du stockage chez des buddies (par opposition à des disques que
l'on contrôle physiquement).

### 7.1 Copy-on-Write (CoW) comme principe explicite

Le versioning (section 5) et la déduplication (section 6) produisent déjà un
comportement CoW de facto — un chunk existant n'est jamais modifié en place,
seules de nouvelles versions sont écrites. Cette section l'élève au rang de
**principe architectural garanti**, avec deux conséquences concrètes :

```
Garantie d'atomicité :
  Une écriture de nouvelle version est soit complète, soit absente —
  jamais un état intermédiaire visible si le processus est interrompu
  (crash, coupure réseau pendant la distribution des shards).

Garantie d'immuabilité des shards :
  Un shard déjà distribué chez un buddy n'est jamais écrasé.
  Une modification du fichier source produit toujours de NOUVEAUX
  shards, jamais une réécriture du contenu existant.
```

```go
// internal/manifest/manifest.go — extension
// Upsert n'écrase jamais une ManifestEntry existante : il crée toujours
// une nouvelle entrée de version, liée à la précédente par PreviousVersion.
type ManifestEntry struct {
    // ... champs existants ...
    PreviousVersion string `json:"previous_version,omitempty"` // FileID de la version antérieure
}
```

Cette garantie simplifie aussi la résolution de conflits (section 5) : comme
rien n'est jamais écrasé, un conflit de modification concurrente ne peut
jamais causer de perte silencieuse — au pire, deux branches de versions
coexistent jusqu'à fusion manuelle.

### 7.2 Scrubbing périodique — preuve de possession, pas déclaration

Faiblesse identifiée lors de la revue précédente (4.1) : le Health Check
existant (section 12.6) fait confiance à la réponse du buddy concernant
son propre hash. Un buddy malveillant contrôlant son propre client peut
mentir indéfiniment sur l'intégrité réelle d'un shard.

Le scrub corrige ça en forçant une preuve fraîche et non pré-calculable,
inspiré du scrub ZFS (qui relit physiquement chaque bloc plutôt que de
faire confiance à un état déclaré) :

```
[Scrub Manager] toutes les 24-72h (configurable par cercle) :
        ↓
Sélectionne un échantillon aléatoire de shards à vérifier
(pondéré : les shards jamais vérifiés récemment sont prioritaires)
        ↓
Pour chaque shard sélectionné :
    génère un nonce aléatoire frais N
    envoie au buddy : "calcule SHA-256(shard_data || N)"
        ↓
Le buddy DOIT lire le shard réel pour répondre correctement
(impossible de pré-calculer une réponse sans posséder le shard,
contrairement à un simple hash statique qu'il pourrait avoir mis en cache)
        ↓
Compare la réponse au résultat attendu (calculé localement à partir
du shard d'origine, que le propriétaire reconstruit virtuellement
via le hash stocké dans le manifest)
        ↓
✅ Correspond → shard intact, marqué "vérifié" avec timestamp
❌ Ne correspond pas ou timeout → shard suspecté corrompu ou absent
        ↓
[Silent Revive] déclenché automatiquement (voir 7.3)
```

```go
// internal/scrub/scrub.go
type ScrubChallenge struct {
    FileID     string
    ShardIndex int
    Nonce      []byte // 16 bytes aléatoires, frais à chaque challenge
}

type ScrubResponse struct {
    Challenge ScrubChallenge
    Proof     [32]byte // SHA-256(shard_ciphertext || Nonce)
    RespondedAt time.Time
}

func (s *ScrubManager) VerifyShard(buddy PeerID, fileID string, shardIndex int) (bool, error) {
    nonce := randomNonce()
    challenge := ScrubChallenge{FileID: fileID, ShardIndex: shardIndex, Nonce: nonce}

    resp, err := s.requestProof(buddy, challenge) // round-trip réseau
    if err != nil {
        return false, err // buddy injoignable — traité comme suspect
    }

    expected := sha256.Sum256(append(s.expectedCiphertext(fileID, shardIndex), nonce...))
    return resp.Proof == expected, nil
}
```

### 7.3 Silent Revive — auto-réparation transparente

Dès qu'un scrub détecte une divergence (shard corrompu, absent, ou buddy ne
répondant pas correctement), Reed-Solomon reconstruit le shard manquant
depuis les autres et le redistribue — équivalent du "resilver" ZFS après
détection de bit rot.

```
Scrub détecte : shard 4 chez Carol ne correspond pas à la preuve attendue
        ↓
[Risk Monitor] recalcule le niveau de risque du fichier concerné
        ↓
Si shards restants ≥ seuil RS minimum :
    [RS Decoder] reconstruit le shard 4 depuis les shards valides
        ↓
    [Redistribution] le shard reconstruit est renvoyé vers :
        - Carol (si elle redevient fiable — nouvelle tentative)
        - OU un autre buddy disponible (rotation si Carol échoue 2 fois)
        ↓
    Aucune alerte utilisateur nécessaire — réparation transparente
Si shards restants < seuil RS minimum :
    → escalade vers le Risk Monitor (alerte 🔴, section 4)
```

Toute la mécanique est silencieuse pour l'utilisateur tant que la résilience
est intacte — exactement le comportement attendu d'un système de stockage
fiable : la fiabilité se maintient en arrière-plan, l'alerte n'arrive que
si le système ne peut plus compenser seul.

### 7.4 Historique de fiabilité par buddy (réputation locale)

Sous-produit naturel du scrub : chaque client peut tenir un score de
fiabilité local et privé pour chacun de ses buddies, basé sur l'historique
réel des réponses aux scrubs — jamais partagé, jamais centralisé.

```go
type BuddyReliability struct {
    PeerID            string
    ScrubsPassed      int
    ScrubsFailed      int
    AvgResponseTime   time.Duration
    LastFailureAt     *time.Time
}
```

Usage concret : en cas de redistribution (7.3), le système privilégie
naturellement les buddies au score le plus fiable. Sert aussi à atténuer
le risque Sybil (4.5 de la revue précédente) — un buddy nouvellement
ajouté avec un historique encore vide reçoit initialement moins de shards
critiques (data) et davantage de shards parity, jusqu'à accumuler un
historique de fiabilité suffisant.

---

## 8. Système d'Invitation — Email + Canal Hors-Bande

### Principe général

L'email est le canal d'invitation universel. Il transporte uniquement des
**identifiants stables** (PeerID, commitment cryptographique). Les **adresses
réseau éphémères** (IP:port) vivent dans le DHT et se mettent à jour automatiquement.

### Payload email (public — ne contient aucun secret)

```json
{
  "version": 1,
  "inviter_peer_id": "12D3KooWAbc...",
  "inviter_pubkey": "ed25519:abc...",
  "commitment": "sha256:def...",
  "circle_name": "Famille",
  "expiry": "2026-06-26T10:00:00Z",
  "nonce": "random-32-bytes",
  "signature": "sig-over-all-fields"
}
```

### Protection MITM — Engagement cryptographique

```
Alice génère :
  secret S (16 bytes aléatoires)
  commitment C = SHA-256(S)

Canal 1 — Email (peut être intercepté) :
  → PeerID Alice + clé publique + C + nom du cercle

Canal 2 — Hors-bande (SMS, vocal, Signal, en personne) :
  → S encodé en 6 mots mémorisables (BIP39)
  → ex : "TIGRE BLEU SOLEIL MONTAGNE RIVIÈRE FAUCON"

Bob entre les 6 mots → SHA-256(S) == C ✅
→ MITM nécessite contrôler simultanément email ET SMS/vocal
```

### Trois niveaux de sécurité — le code à 6 mots est obligatoire par défaut

Le code à 6 mots (commitment SHA-256, canal hors-bande) constitue le **MFA natif**
du système d'invitation :

```
Facteur 1 : possession de la boîte mail (réception du lien d'invitation)
Facteur 2 : connaissance du secret hors-bande (les 6 mots, par SMS/vocal)
```

Décision validée (revue pré-lancement, juin 2026) : ce second facteur n'est
**pas optionnel**. Le rendre facultatif créerait une fausse impression de
sécurité — un attaquant ayant compromis la boîte mail d'Alice pourrait inviter
Bob à sa place, et Bob l'accepterait par réflexe social ("ça vient d'Alice").
Seul le Niveau 3 (QR code en présentiel) peut remplacer le Niveau 1, car il
est intrinsèquement plus fort (canal physique unique, aucune interception
possible à distance).

| Niveau | Mécanisme | Statut | Usage recommandé |
|--------|-----------|--------|-----------------|
| 1 — Standard | Email + 6 mots SMS/vocal | **Obligatoire par défaut** | Famille, amis |
| 2 — Renforcé | Email + vérification DKIM auto | Option supplémentaire au-dessus du Niveau 1 | Données sensibles |
| 3 — Maximum | QR code en présentiel | Seule alternative substituant le Niveau 1 | Données critiques |

### Appairage mode personnel

```
Pas d'email nécessaire.
Les deux appareils affichent un code court à 8 chiffres.
L'utilisateur vérifie visuellement que les codes correspondent.
Master Device Key → authentification automatique.
```

---

## 9. Récupération d'Identité — Perte de l'Ordinateur

### Le problème

Restaurer ses fichiers après une perte totale du PC nécessite plus que le mot de
passe maître : le nouvel appareil génère par défaut un **nouveau PeerID** (nouvelle
paire de clés Ed25519), que les buddies ne reconnaissent pas. Sans mécanisme dédié,
les buddies refusent la connexion — ils ne savent pas que c'est toujours toi.

### Solution principale — Recovery Phrase déterministe

À la création du tout premier cercle, CerclBackup dérive l'identité réseau
(PeerID) directement depuis la Master Key, et génère une **Recovery Phrase de
12 mots** (BIP39) permettant de la reconstruire à l'identique.

```
Mot de passe maître
        ↓ Argon2id
   Master Key
        ↓ HKDF "cerclbackup-identity-seed-v1"
   Seed Ed25519 (32 bytes)
        ↓ ed25519.NewKeyFromSeed()
   Paire de clés Ed25519 → PeerID
        ↓ encodage BIP39
   Recovery Phrase (12 mots)
```

```go
// internal/crypto/recovery.go

// GenerateRecoveryPhrase derives a deterministic Ed25519 keypair from the
// master key, encoded as a 12-word BIP39 phrase.
func GenerateRecoveryPhrase(masterKey []byte) (string, ed25519.PrivateKey) {
    seed := hkdfExpand(masterKey, "cerclbackup-identity-seed-v1", 32)
    priv := ed25519.NewKeyFromSeed(seed)
    phrase := bip39.Encode(seed)
    return phrase, priv
}

// RestoreIdentity recreates the exact same PeerID from the recovery phrase.
func RestoreIdentity(phrase string) (ed25519.PrivateKey, error) {
    seed, err := bip39.Decode(phrase)
    if err != nil {
        return nil, err
    }
    return ed25519.NewKeyFromSeed(seed), nil
}
```

### Flux de restauration (cas nominal)

```
1. Nouveau PC → installation CerclBackup
2. "As-tu déjà un cercle ?" → [Oui, je restaure]
3. Entre le mot de passe maître
4. Entre les 12 mots de récupération
        ↓
5. Reconstruction du MÊME PeerID que l'ancien appareil
        ↓
6. Les buddies reconnaissent automatiquement ce PeerID
   → connexion acceptée sans nouvelle invitation
        ↓
7. Récupération des manifests chiffrés (un par cercle)
        ↓
8. Restauration de tous les fichiers, tous cercles confondus
```

Aucune action requise de la part des buddies — la reconnaissance est automatique
car le PeerID est cryptographiquement identique à l'original.

### Onboarding — affichage obligatoire des 12 mots

```
Écran obligatoire à la création du premier cercle :

  "Voici tes 12 mots de récupération.
   Si tu perds ton PC, ils te permettent de tout récupérer.
   Si tu les perds aussi, demande l'aide de 3 de tes buddies."

   [MAISON] [SOLEIL] [RIVIÈRE] [MONTAGNE] [TIGRE] [BLEU]
   [FAUCON] [CHÊNE] [ÉTOILE] [VENT] [GRANIT] [AUBE]

   [✅ J'ai noté ces mots]   [📄 Exporter en PDF]
```

### Plan B — Récupération Sociale (si les 12 mots sont aussi perdus)

S'appuie sur le Trust Graph (section 12.8) plutôt que sur la cryptographie pure :

```
1. Le nouveau PC génère un NOUVEAU PeerID (identité différente de l'originale)
2. L'utilisateur contacte manuellement 2-3 buddies (téléphone, en personne)
3. Chaque buddy vérifie humainement l'identité ("c'est bien Olivier")
4. Chaque buddy confirmant signe le nouveau PeerID dans son Trust Graph local
5. Si le seuil configuré est atteint (ex : 3 confirmations sur 5 buddies)
   → le nouveau PeerID est accepté comme légitime par le réseau
   → les buddies acceptent de transmettre les shards qu'ils détiennent
6. ⚠️ Sans la Master Key, les shards récupérés restent illisibles
```

Modèle équivalent au "Social Recovery" des wallets crypto modernes (Argent, Safe),
mais limité ici à la récupération de l'**identité réseau** — pas du chiffrement.

### Récapitulatif des niveaux de récupération

| Perte | Récupération | Mécanisme |
|---|---|---|
| PC uniquement | Totale, automatique | Mot de passe + 12 mots → PeerID identique |
| PC + 12 mots (mot de passe conservé) | Totale, manuelle | Nouvelle identité + confirmation sociale (3 buddies) |
| PC + 12 mots + mot de passe | Aucune | Zero-knowledge assumé — limite documentée explicitement |

Cette troisième ligne n'est pas une faiblesse mais la garantie structurelle de
confidentialité du système : si CerclBackup pouvait restaurer sans le mot de
passe, cela signifierait qu'un tiers (ou un attaquant) le pourrait aussi.

---

## 10. Fonctionnalités Complémentaires

### 10.1 Bandwidth Scheduling

```
Paramètres réseau par cercle :
  ├── Bande passante max upload : [Auto / 1 Mbps / 5 Mbps / Illimité]
  ├── Plages horaires actives :
  │     Lun-Ven : 20h00 → 08h00 (hors heures de travail)
  │     Sam-Dim : toute la journée
  └── Réseau WiFi uniquement : [✅ Oui] (évite la 4G)
```

Sync lourde la nuit, impact nul pendant les heures de travail.

### 10.2 Mode Voyage

```
"Je pars 3 semaines, mon laptop sera offline"
  → [Mettre en pause jusqu'au] [date]
  → Aucune alerte de risque pendant cette période
  → Les autres buddies ne tentent pas de redistribuer ses shards
  → Reprise automatique à la date indiquée
```

Evite les fausses alertes et les redistributions inutiles pendant les absences planifiées.

### 10.3 Rapport Mensuel

Email automatique le 1er de chaque mois :

```
📊 Rapport CerclBackup — Mai 2026

Cercle "Famille"
  ✅ Disponibilité buddies : 94% du mois
  📁 47 fichiers sauvegardés — 12.3 Go
  🕐 Dernière sauvegarde : il y a 2 heures

Cercle "Travail"
  🟡 Bob hors ligne 6 jours — encore sécurisé
  📁 23 fichiers sauvegardés — 4.1 Go

Niveau de risque global : 🟢 Sécurisé
Prochain health check : dans 3 jours
```

Rassurant pour le grand public. Preuve que le système fonctionne.

### 10.4 Dry Run de Restauration

```
Mensuel automatique (optionnel, activé par défaut) :
  → Simule une restauration complète de 3 fichiers aléatoires
  → Vérifie que les shards sont disponibles et intègres
  → Vérifie que le déchiffrement fonctionne
  → N'écrit aucun fichier sur le disque
  → Rapport : "✅ Restauration testée avec succès le 01/06/2026"
```

Garantit que le backup est réellement utilisable, pas juste supposément intact.

### 10.5 Détection d'Activité Inhabituelle (Anti-Ransomware)

```
Seuils configurables :
  → Plus de 100 fichiers modifiés en moins de 5 minutes → alerte
  → Plus de 50% des fichiers d'un dossier modifiés → alerte
  → Extension de fichier inconnue apparue en masse → alerte

Actions automatiques :
  → Notification immédiate
  → [Pause de la sync] proposée
  → La dernière version saine reste accessible chez les buddies
```

---

## 11. Architecture Globale

```
┌──────────────────────────────────────────────────────────────────┐
│                        CLIENT WINDOWS                            │
│                                                                  │
│  ┌──────────┐  ┌──────────┐  ┌──────────┐  ┌────────────────┐  │
│  │ Watcher  │→ │ Chunker  │→ │Reed-Sol. │→ │  AES-256-GCM   │  │
│  │(fsnotify)│  │+ Dedup   │  │ Encoder  │  │  Encryptor     │  │
│  └──────────┘  └──────────┘  └──────────┘  └───────┬────────┘  │
│                                                     │           │
│  ┌──────────┐  ┌──────────┐  ┌──────────┐          │           │
│  │ Keystore │  │Manifests │  │ P2P Layer│←──────────┘           │
│  │(Argon2id)│  │(1/cercle)│  │(libp2p)  │                      │
│  └──────────┘  └──────────┘  └────┬─────┘                      │
│                                   │                             │
│  ┌──────────┐  ┌──────────┐  ┌────┴─────┐  ┌───────────────┐  │
│  │TrustGraph│  │  Risk    │  │Bandwidth │  │  Versioning   │  │
│  │(invisible│  │ Monitor  │  │Scheduler │  │  Manager      │  │
│  │à l'user) │  │          │  │          │  │               │  │
│  └──────────┘  └──────────┘  └──────────┘  └───────────────┘  │
│                                                                  │
│  ┌──────────────────────────────────────────────────────────┐   │
│  │                  Systray UI (Fyne)                        │   │
│  │  [Famille ✅] [Travail 🟡] [Privé ✅] [+ Cercle]         │   │
│  └──────────────────────────────────────────────────────────┘   │
└───────────────────────────────────┬──────────────────────────────┘
                                    │ libp2p
                    ┌───────────────┼───────────────┐
                    │               │               │
              ┌─────▼────┐   ┌──────▼───┐   ┌──────▼────┐
              │  Buddy A  │   │ Buddy B  │   │  Buddy C  │
              │ (shards   │   │ (shards  │   │ (shards   │
              │  cercle 1)│   │  cercle  │   │  cercle   │
              │           │   │  1 + 2)  │   │  2)       │
              └───────────┘   └──────────┘   └───────────┘
                                    │
                    ┌───────────────┴────────────────┐
                    │      Bootstrap Nodes           │
                    │   DHT Kademlia (fédéré)        │
                    │ + TURN Relay (fallback NAT)    │
                    └────────────────────────────────┘
```

---

## 12. Composants Détaillés

### 12.1 Watcher + Détection d'Activité Inhabituelle

- Surveillance récursive des dossiers configurés
- Debounce 2 secondes
- Exclusion fichiers temporaires
- Compteur d'événements sur fenêtre glissante 5 minutes → alerte ransomware

### 12.2 Chunker + Déduplication

- Taille de chunk : 4 MB (configurable)
- Hash SHA-256 par chunk
- Déduplication inter-versions : chunk identique = stocké une seule fois
- CDC (Content Defined Chunking) — Phase 4

### 12.3 Reed-Solomon Encoder

**Schémas adaptatifs — minimum 3 buddies requis, schéma 1/1 (miroir simple)
définitivement exclu :**

Décision validée (revue pré-lancement, juin 2026) : Reed-Solomon est
**toujours obligatoire**, y compris en Mode Cercle Personnel avec seulement
2 appareils. Un schéma 1/1 équivaudrait à une simple copie miroir, où un
buddy unique détiendrait l'intégralité reconstructible du fichier — exactement
la propriété que Reed-Solomon est censé empêcher (section 12.3 historique,
"aucun buddy ne détient suffisamment de chunks pour reconstruire seul"). Le
minimum technique est donc fixé à 3 buddies / appareils pour tout cercle,
mode personnel inclus.

| Buddies | Schéma | Perte tolérée | Overhead |
|---------|--------|---------------|----------|
| 3       | 2/1    | 1 buddy       | +50%     |
| 5       | 3/2    | 2 buddies     | +67%     |
| 8       | 5/3    | 3 buddies     | +60%     |
| 10      | 6/4    | 4 buddies     | +67%     |

### 12.4 Encryptor AES-256-GCM

```
Mot de passe + cercle_id
        ↓ Argon2id
   Master Key du cercle (256 bits)
        ↓ HKDF
   File Key → Shard Key (unique par shard)
        ↓ AES-256-GCM + nonce aléatoire
   Shard chiffré
```

### 12.5 Versioning Manager

- Index des versions par fichier dans le manifest du cercle
- Politique de rétention configurable par cercle
- Déduplication des chunks identiques entre versions
- Restauration par date ou numéro de version

### 12.6 Risk Monitor

- Tourne toutes les 5 minutes
- Calcule RiskLevel pour chaque fichier de chaque cercle
- Déclenche notifications systray + email selon le niveau
- Propose des actions correctives contextuelles
- Health check SHA-256 des shards distants toutes les 24h

### 12.7 Bandwidth Scheduler

- Plages horaires configurables par cercle
- Limite de bande passante par plage
- Restriction WiFi uniquement (évite la 4G)
- Priorité entre cercles configurable

### 12.8 Trust Graph (invisible pour l'utilisateur)

```go
type TrustEdge struct {
    From      string    // PeerID signataire
    To        string    // PeerID signé
    CircleID  string    // Cercle concerné
    Degree    int       // 1 = buddy direct, 2 = ami d'un ami
    Signature []byte    // Ed25519 — invisible pour l'user
    Quota     int64     // octets accordés dans ce cercle
}
```

Règle d'allocation :
- Degré 0 (mes appareils) → shards data prioritaires
- Degré 1 (buddies directs) → shards data + parity
- Degré 2 (amis d'amis) → shards parity uniquement

**Règle non négociable — le degré 2 n'est jamais automatique**

Décision validée (revue pré-lancement, juin 2026) : la découverte d'un buddy
de degré 2 (ami d'un buddy direct) ne doit **jamais** étendre silencieusement
le cercle de confiance. Un buddy direct n'a pas le pouvoir d'engager
l'utilisateur auprès de tiers qu'il n'a jamais rencontrés.

```
❌ Interdit : Bob invite Carol dans son cercle
            → Carol devient automatiquement buddy de parity pour Alice

✅ Obligatoire : "Bob connaît Carol sur CerclBackup.
                  Veux-tu l'ajouter à ton cercle ?"
                  [Oui, inviter Carol]   [Non merci]
```

Sans ce garde-fou, un utilisateur pourrait se retrouver à stocker des données
pour, ou dépendre du stockage de, des personnes qu'il n'a jamais choisies —
et la profondeur du graphe deviendrait un vecteur de Sybil attack (un
attaquant multipliant les identités degré 2 pour s'infiltrer dans des cercles
sans consentement direct). Aucune vérification d'identité réelle n'est requise
pour devenir buddy (pas de email confirmé, pas de KYC) — le seul mécanisme de
protection contre les identités multiples est donc social, et repose
entièrement sur ce consentement explicite à chaque saut du graphe.

**Règle d'or UX :**
> Tout ce qui est cryptographique est automatique et invisible.
> Tout ce qui est visible est humain et simple.

```
Invisible : Ed25519, SPAKE2, AES-256-GCM, RS, DHT, HKDF, TrustGraph
Visible   : cercle, buddy, inviter, espace, statut, risque, rapport
```

### 12.9 P2P Layer — Réseau libp2p

**Découverte des pairs — mécanismes par ordre de priorité :**

| Mécanisme | Scope | Usage |
|-----------|-------|-------|
| mDNS | LAN uniquement | Mode personnel, devices sur même réseau |
| Buddies déjà connus (n'importe quel cercle) | Internet | Point d'entrée privilégié — voir 12.10 |
| DHT Kademlia | Internet | Découverte via PeerID après invitation |
| Bootstrap nodes communautaires | Internet | Dernier recours, premier contact absolu seulement |

**Cas particulier — deux buddies sur le même réseau local :**

```
mDNS détecte le buddy en quelques millisecondes
→ connexion directe, aucun hole punching nécessaire
→ aucun passage par le DHT ou le relay TURN
→ débit réseau local (généralement bien supérieur à l'upload résidentiel)
```

C'est le scénario le plus rapide et le plus fiable. Un NAS et un PC sur le même
réseau domestique se synchronisent quasi-instantanément. La bascule vers le DHT
et le hole punching ne se produit que lorsqu'un buddy est sur un réseau distant
(chez un ami, sur un VPS).

```go
func (d *Discovery) discover(peerID string) (*Peer, error) {
    // 1. Tentative mDNS (LAN) — quelques millisecondes
    if peer := d.mdnsLookup(peerID); peer != nil {
        return peer, nil
    }
    // 2. Fallback DHT (Internet) — quelques secondes
    return d.dhtLookup(peerID)
}
```

**Connectivité NAT — trois tentatives automatiques (hors LAN) :**

```
Tentative 1 : Connexion directe (déjà couverte par mDNS si même réseau)
     ↓ échec
Tentative 2 : UDP Hole Punching (réussit ~70% des cas)
     ↓ échec (CG-NAT opérateur — Free Mobile, SFR...)
Tentative 3 : TURN Relay — voir stratégie d'auto-suffisance (12.10)
```

### 12.10 Auto-Suffisance Réseau — aucun serveur permanent obligatoire

Décision validée (revue pré-lancement, juin 2026) : CerclBackup ne doit
dépendre d'aucune infrastructure permanente opérée par un tiers unique
(y compris ses propres créateurs) pour continuer à fonctionner entre des
utilisateurs qui se connaissent déjà. Le test de validation :

> Si toute infrastructure opérée par l'éditeur de CerclBackup disparaît
> (VPS coupé, domaine expiré, société fermée), deux utilisateurs ayant
> déjà échangé leur PeerID une fois doivent pouvoir continuer à se
> synchroniser indéfiniment.

**Stratégie en cascade — priorité aux ressources déjà détenues par l'utilisateur :**

```go
// internal/p2p/selfsufficient.go
type BootstrapStrategy struct {
    // 1. PRIORITÉ ABSOLUE : tout buddy déjà connu, dans N'IMPORTE QUEL
    //    cercle de l'utilisateur, sert de point d'entrée DHT. Un buddy
    //    du cercle "Famille" peut servir à retrouver le DHT même pour
    //    une connexion concernant le cercle "Travail" — seule la
    //    DÉCOUVERTE réseau est mutualisée, jamais les données déchiffrées.
    KnownPeers []PeerID

    // 2. Si un membre d'un cercle dispose d'une IP publique stable
    //    (NAS avec IP fixe, VPS personnel) → il sert de relay TURN
    //    pour les AUTRES membres de SES PROPRES cercles uniquement.
    //    Pas de relay mutualisé entre cercles ou utilisateurs étrangers.
    SelfHostedRelay *PeerID

    // 3. DERNIER RECOURS — uniquement lors du tout premier contact
    //    entre deux personnes qui ne se sont jamais croisées dans
    //    aucun cercle existant (ex : tout premier appairage d'un
    //    nouvel utilisateur n'ayant encore aucun buddy). Liste
    //    communautaire, idéalement fédérée (plusieurs opérateurs
    //    indépendants), jamais un point de défaillance pour un
    //    utilisateur ayant déjà au moins un cercle fonctionnel.
    PublicBootstrapList []string
}
```

**Conséquence sur le relay TURN (fallback CG-NAT) :**

```
Ancien modèle : relay TURN opéré en permanence par l'éditeur (IONOS)
Nouveau modèle : n'importe quel buddy avec IP publique stable
                 (NAS, VPS personnel) sert de relay pour SON PROPRE
                 cercle — la dépendance externe disparaît dès qu'au
                 moins un membre du cercle a une IP stable
```

**Ce qui reste optionnel et non bloquant :**

Une liste de bootstrap nodes communautaires peut continuer d'exister pour
fluidifier l'expérience des tout premiers utilisateurs (l'équivalent des
trackers BitTorrent publics), mais elle est :
- Désactivable dans les paramètres avancés
- Idéalement fédérée entre plusieurs opérateurs volontaires (pas un point
  unique opéré exclusivement par l'éditeur de CerclBackup)
- Jamais nécessaire pour un utilisateur ayant déjà au moins un cercle actif

---

## 13. Pipelines

### Sauvegarde

```
fichier.docx modifié
        ↓
[Watcher] détecte + vérifie seuil anti-ransomware
        ↓
[Chunker] découpe + déduplication vs versions antérieures
        ↓
[RS Encoder] schéma adaptatif au cercle
        ↓
[Encryptor] AES-256-GCM avec clé dérivée du cercle
        ↓
[Versioning] nouvelle version créée dans le manifest
        ↓
[TrustGraph] sélection buddies par degré
        ↓
[Bandwidth Scheduler] mise en file d'attente selon plage horaire
        ↓
[P2P Layer] distribution des shards
        ↓
[Risk Monitor] recalcul du niveau de risque
```

### Restauration après perte totale du PC

```
Nouveau PC → installe CerclBackup → entre mot de passe
        ↓
Master Key dérivée par cercle → contacte buddies via DHT
        ↓
Récupère manifest chiffré de chaque cercle
        ↓
Pour chaque fichier (version choisie) :
    Récupère N shards disponibles sur N+M
    RS Decoder → reconstruit les shards manquants
    Decryptor → déchiffre avec clé du cercle
    Reassembler → fichier original ✅
```

---

## 14. Considérations de Sécurité

| Menace | Mitigation | Niveau |
|--------|-----------|--------|
| Buddy lit les données | AES-256-GCM + RS partiel | ✅ Fort |
| Collusion de buddies | RS : N buddies nécessaires | ✅ Fort |
| MITM invitation | Engagement SHA-256 + canal SMS séparé (obligatoire) | ✅ Fort |
| Ransomware | Versioning + détection activité inhabituelle | ✅ Fort |
| Suppression accidentelle | Versions conservées chez buddies | ✅ Fort |
| Corruption silencieuse | Health check SHA-256 quotidien | ✅ Fort |
| Compromission d'un cercle | Clés distinctes par cercle | ✅ Fort |
| Buddy supprime les shards | Health check + redistribution auto | ✅ Fort |
| Fichier verrouillé au backup | VSS (Windows) / snapshot (NAS) | ✅ Fort |
| Conflit de modification concurrente | Détection + copie de conflit (non destructif) | ✅ Fort |
| Extension non consentie du cercle | Degré 2 toujours explicite, jamais automatique | ✅ Fort |
| Suppression accidentelle de cercle | Double confirmation par saisie du nom | ✅ Fort |
| Perte du mot de passe | Irrécupérable — documenté clairement | ⚠️ Documenté |
| Perte simultanée mot de passe + 12 mots | Irrécupérable par design (zero-knowledge) | ⚠️ Documenté |
| Métadonnées sociales | PeerID pseudonymes — limitation connue | ⚠️ Phase 4 |
| Sybil attack (identités multiples) | Pas de vérification d'identité réelle — mitigé socialement par le consentement explicite degré 2 | ⚠️ Limitation assumée |
| Triche sur health check (buddy malveillant) | Hash simple actuellement — vérification aléatoire à nonce prévue | ⚠️ Phase 4 |
| Hébergement involontaire de contenu tiers | Limitation structurelle du P2P chiffré | ⚠️ CGU + avis juridique avant release publique |

---

## 15. Décisions de Conception Validées — Revue Pré-Lancement

Cette section consolide les arbitrages tranchés lors de la revue critique du
projet (juin 2026), couvrant détournements d'usage, failles comportementales,
faiblesses fonctionnelles et de sécurité. Chaque ligne correspond à une
décision actée, pas à une question encore ouverte. Certaines lignes ont été
révisées lors d'un second passage de revue (voir notes).

| Sujet | Décision | Raison |
|---|---|---|
| Réciprocité sociale | Aucune preuve cryptographique (proof of storage) | Système de confiance assumé, cohérent avec l'absence d'identité vérifiée formellement |
| Reed-Solomon | **Toujours obligatoire, jamais de schéma 1/1 même en mode personnel** | Garantit qu'aucun buddy, y compris en mode personnel, ne détient jamais un fichier reconstructible seul — voir révision ci-dessous |
| Hébergement de contenu tiers via shards chiffrés | **Révisé** — risque largement réduit, pas une faiblesse de conception | Avec RS obligatoire, aucun buddy ne détient de fragment exploitable seul ; CerclBackup ne permet pas la publication ni l'accès par des tiers non invités, contrairement à Freenet/Tahoe-LAFS. Mention dans les CGU conservée par prudence, mais retiré des points bloquants |
| Extension du Trust Graph au degré 2 | **Toujours explicite, jamais automatique** | Un buddy direct ne peut pas engager l'utilisateur auprès de tiers inconnus |
| Vérification d'identité des buddies | **Le MFA d'invitation (code 6 mots hors-bande) constitue la vérification d'identité** | Vérification sociale et relative (Alice confirme que c'est bien son Bob) plutôt qu'un KYC absolu ; évite de réintroduire un tiers de confiance centralisé ou de la collecte de pièces d'identité |
| MFA sur l'invitation (code 6 mots) | **Obligatoire par défaut**, non contournable sauf Niveau 3 (QR présentiel) | Évite la fausse impression de sécurité et le contournement par flemme ; sert aussi de vérification d'identité (voir ligne ci-dessus) |
| Suppression d'un cercle | **Double confirmation par saisie du nom** | Action destructive et irréversible (suppression chez tous les buddies) |
| Bootstrap nodes DHT / TURN relay permanent | **Révisé — auto-suffisance totale exigée** | Plus aucune dépendance à un serveur permanent opéré par l'éditeur pour des utilisateurs ayant déjà échangé leur PeerID. Les buddies déjà connus servent de point d'entrée DHT ; tout buddy à IP stable sert de relay pour son propre cercle. Liste communautaire conservée uniquement comme dernier recours optionnel pour le tout premier contact absolu |
| Volume Shadow Copy (Windows) | **À implémenter avant toute release** | Sans VSS, les fichiers verrouillés échouent silencieusement ou se corrompent |
| Conflits de modification concurrente | **À implémenter** — copie de conflit façon Syncthing | Évite l'écrasement silencieux entre appareils personnels modifiés hors ligne |
| Intégrité des shards distants | **Révisé — scrub à preuve fraîche (nonce) au lieu d'un hash déclaré** | Un buddy malveillant ne peut plus mentir indéfiniment sur l'intégrité d'un shard ; principe ZFS scrub adapté au contexte distribué non fiable |
| Réparation après corruption détectée | **Silent Revive automatique** | Reed-Solomon reconstruit et redistribue le shard corrompu sans intervention utilisateur, tant que le seuil RS reste respecté ; principe ZFS resilver |
| Modification en place des shards | **Interdite par principe (Copy-on-Write)** | Garantit l'atomicité des écritures et simplifie la résolution de conflits — aucun état intermédiaire visible en cas d'interruption |
| Fatigue d'alerte (buddy intermittent) | Reconnu comme risque, seuils adaptatifs à concevoir | Pas de solution figée — à itérer après usage réel en Phase 3 |
| Statut RGPD du PeerID et des logs relay | Question ouverte, risque réduit par l'auto-suffisance réseau | Avis juridique requis avant toute release publique au-delà du cercle proche ; moins critique sans relay permanent opéré par l'éditeur |
| Mineurs comme buddies | Question ouverte, pas de blocage technique prévu | À couvrir dans les CGU, pas de vérification d'âge fiable possible sans collecte de données |
| Choix du langage Go | **Confirmé, pas de changement** | libp2p est nativement écrit en Go (origine IPFS), meilleure maturité DHT/hole punching/relay pour ce cas d'usage précis ; compilation cross-platform triviale (Windows/ARM NAS) ; vitesse de développement adaptée à un projet solo |

---

## 16. Structure du Projet Go

```
cerclbackup/
├── cmd/
│   ├── cerclbackup/        # Point d'entrée client Windows
│   └── relay/              # Bootstrap node / TURN relay
├── internal/
│   ├── watcher/            # fsnotify + anti-ransomware
│   ├── vss/                # Volume Shadow Copy (Windows) / snapshot (NAS)
│   ├── chunker/            # Découpage + déduplication
│   ├── codec/              # Reed-Solomon
│   ├── crypto/             # AES-GCM, Argon2id, HKDF, keystore
│   ├── compress/           # Compression sélective (zstd)
│   ├── versioning/         # Gestion des versions de fichiers
│   ├── conflict/           # Détection et résolution de conflits concurrents
│   ├── manifest/           # Index chiffré par cercle
│   ├── risk/               # Risk Monitor + health check
│   ├── scheduler/          # Bandwidth scheduling
│   ├── trust/              # Trust Graph (interne, consentement explicite degré 2)
│   ├── scrub/               # Scrub périodique (preuve fraîche à nonce) + Silent Revive
│   ├── p2p/
│   │   ├── host.go
│   │   ├── discovery.go    # mDNS + DHT
│   │   ├── protocols.go
│   │   ├── invite.go       # Email + SPAKE2
│   │   └── nat.go          # Hole punching + TURN
│   ├── storage/            # Store local des shards
│   └── ui/
│       ├── systray.go
│       ├── circles.go      # Vue multi-cercles
│       ├── risk.go         # Alertes de risque
│       ├── versions.go     # Historique des versions
│       └── settings.go
├── pkg/
│   ├── protocol/           # Types partagés
│   └── invite/             # Commitment + BIP39
├── go.mod
├── ARCHITECTURE.md
├── THREAT_MODEL.md
└── README.md
```

---

## 17. Dépendances Go Principales

| Package | Usage |
|---------|-------|
| `github.com/libp2p/go-libp2p` | Stack P2P |
| `github.com/libp2p/go-libp2p-kad-dht` | DHT Kademlia |
| `github.com/libp2p/go-libp2p-relay` | TURN relay |
| `github.com/klauspost/reedsolomon` | Reed-Solomon |
| `github.com/klauspost/compress/zstd` | Compression sélective des chunks |
| `golang.org/x/crypto` | AES-GCM, Argon2id, HKDF, SPAKE2 |
| `github.com/fsnotify/fsnotify` | Surveillance fichiers |
| `fyne.io/fyne/v2` | UI Windows |
| `github.com/google/uuid` | Identifiants |
| `filippo.io/cpace` | PAKE invitation |

**Compilation cross-platform :**
```bash
GOOS=windows GOARCH=amd64 go build ./cmd/cerclbackup  # Windows
GOOS=linux   GOARCH=arm64 go build ./cmd/cerclbackup  # NAS Synology
GOOS=linux   GOARCH=amd64 go build ./cmd/relay        # VPS relay
```

---

## 18. Roadmap

### Phase 1 — Pipeline local ✅ LIVRÉ (révisé)
- [x] Chunker + Reed-Solomon + AES-GCM + Keystore + Manifest + Store
- [x] Restauration complète avec shard manquant simulé
- [x] Tests unitaires + intégration
- [x] Reed-Solomon rendu obligatoire — suppression du fallback miroir 1/1,
      minimum 3 buddies/appareils imposé par `protocol.BestScheme`

### Phase 2 — P2P Internet
- [ ] libp2p + DHT Kademlia
- [ ] UDP Hole Punching
- [ ] Stratégie d'auto-suffisance réseau (buddies connus comme bootstrap
      prioritaire, relay porté par tout buddy à IP stable au sein de son
      propre cercle, liste communautaire en dernier recours uniquement)
- [ ] mDNS LAN discovery (connexion directe buddies même réseau)
- [ ] Invitation email + engagement SHA-256 + BIP39 (MFA obligatoire par
      défaut, sert aussi de vérification d'identité sociale)
- [ ] Deep link Windows `cerclbackup://`
- [ ] Manifest distribué chez les buddies (immuable, principe Copy-on-Write)
- [ ] Trust Graph + Risk Monitor v1 (degré 2 toujours en confirmation explicite)
- [ ] Recovery Phrase (identité déterministe) + restauration nouveau PC
- [ ] Social Recovery (fallback si phrase perdue)
- [ ] Scrub Manager (preuve fraîche à nonce, anti-triche health check)
- [ ] Silent Revive (auto-réparation via RS dès divergence détectée)
- [ ] Historique de fiabilité par buddy (réputation locale, non partagée)

### Phase 2b — Mode Appareils Personnels
- [ ] Master Device Key
- [ ] Appairage QR code
- [ ] Quota asymétrique
- [ ] Binaire ARM64 NAS
- [ ] Minimum 3 appareils imposé (même règle RS qu'en mode social)

### Phase 3 — Grand Public Windows
- [ ] Cercles multiples (UI + isolation crypto)
- [ ] Suppression de cercle avec double confirmation (saisie du nom)
- [ ] Versioning des fichiers + déduplication inter-versions (CoW garanti)
- [ ] Volume Shadow Copy (Windows) / snapshot (NAS) pour fichiers verrouillés
- [ ] Détection et gestion des conflits de modification concurrente
- [ ] Compression sélective (zstd, détection de gain réel)
- [ ] Déduplication inter-fichiers même cercle (ChunkRef + RefCount)
- [ ] Alertes de risque complètes (avec seuils adaptatifs anti-fatigue)
- [ ] Bandwidth Scheduler
- [ ] Mode Voyage
- [ ] Rapport mensuel email
- [ ] Dry Run de restauration mensuel
- [ ] Détection activité inhabituelle (anti-ransomware)
- [ ] UI Systray complète (Fyne)
- [ ] Installeur Windows (WiX)
- [ ] Auto-update
- [ ] CGU (hébergement involontaire de contenu tiers — risque réduit par RS
      obligatoire, mention conservée par prudence)
- [ ] THREAT_MODEL.md public
- [ ] Liste de bootstrap communautaire optionnelle (fédérée si possible,
      jamais un point de défaillance unique opéré par l'éditeur)

### Phase 4 — Hardening post-release
- [ ] Onion routing (protection métadonnées sociales)
- [ ] Audit de sécurité tiers
- [ ] Package Synology DSM officiel
- [ ] CDC (Content Defined Chunking)

---

*CerclBackup — sauvegarde P2P distribuée entre personnes de confiance*
*AGPL-3.0 — 2026*
