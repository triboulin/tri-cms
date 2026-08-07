# triCMS — Spécifications Techniques

> CMS headless multi-tenant écrit en Go, exposant une IHM d'administration rendue côté serveur (HTMX) et une API pour la gestion de projets, schémas de contenu, contenus, médias, tokens API et webhooks.

## Sommaire

1. [Matrice des Rôles & Modèle de Sécurité (RBAC)](#1-matrice-des-rôles--modèle-de-sécurité-rbac)
2. [Architecture & Base de Données](#2-architecture--base-de-données)
3. [Typage des Champs & Cardinalité (Simple / Collection)](#3-typage-des-champs--support-simple--collection)
4. [Spécification Front-End (Structure HTMX)](#4-spécification-front-end-structure-htmx)
5. [Exigence de Qualité Logicielle : 90 % de Code Coverage](#5-exigence-de-qualité-logicielle--90--de-code-coverage)

---

## 1. Matrice des Rôles & Modèle de Sécurité (RBAC)

Le système distingue deux niveaux de privilèges :

- Le rôle global **ADMIN**, géré dans `system.db`, sans notion de projet.
- Les rôles locaux par projet (**CONCEPTEUR**, **GESTIONNAIRE**, **REDACTEUR**), géré via la table d'habilitations `project_permissions`.

Les rôles projet sont **cumulatifs par hiérarchie** : chaque rôle hérite implicitement des droits du rôle immédiatement inférieur (REDACTEUR ⊂ GESTIONNAIRE ⊂ CONCEPTEUR), et **ADMIN** a systématiquement accès à toutes les portées projet en plus de la portée globale.

| Rôle | Portée | Périmètre d'actions |
|---|---|---|
| **ADMIN** | Global | Accès total : gestion des comptes globalement, création/suppression de projets, gestion des tokens API, des webhooks, et des audit logs globaux. |
| **CONCEPTEUR** | Projet | Tous les droits du Gestionnaire + création, modification et suppression des schémas/collections de contenu et de la structure des dossiers. |
| **GESTIONNAIRE** | Projet | Tous les droits du Rédacteur + création et affectation des comptes utilisateurs (rôles GESTIONNAIRE ou REDACTEUR) rattachés à son projet. |
| **REDACTEUR** | Projet | Création, édition, suppression et gestion des états (`draft`/`published`) des contenus (objets), ainsi que téléversement/gestion des médias. |

> **Précision** : un même utilisateur peut posséder des rôles différents selon les projets (une ligne `project_permissions` par couple `user_id`/`project_id`). Un utilisateur `is_global_admin = 1` n'a pas besoin d'entrée dans `project_permissions` pour accéder à un projet.

---

## 2. Architecture & Base de Données

L'isolation multi-tenant repose sur une base SQLite globale (`system.db`) et une base SQLite dédiée par projet (`client.db`), stockée sous `./data/projects/{project_id}/`. Cette séparation physique garantit qu'aucune requête ne peut fuiter d'un projet vers un autre.

### 2.1 Base Globale (`./data/system.db`)

```sql
CREATE TABLE users (
    id TEXT PRIMARY KEY,
    email TEXT UNIQUE NOT NULL,
    password_hash TEXT NOT NULL,
    is_global_admin BOOLEAN NOT NULL DEFAULT 0,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE projects (
    id TEXT PRIMARY KEY, -- Ex: "projAAA"
    name TEXT NOT NULL,
    folder_path TEXT NOT NULL,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE project_permissions (
    user_id TEXT REFERENCES users(id) ON DELETE CASCADE,
    project_id TEXT REFERENCES projects(id) ON DELETE CASCADE,
    role TEXT CHECK(role IN ('CONCEPTEUR', 'GESTIONNAIRE', 'REDACTEUR')) NOT NULL,
    PRIMARY KEY (user_id, project_id)
);

CREATE TABLE api_tokens (
    id TEXT PRIMARY KEY,
    project_id TEXT REFERENCES projects(id) ON DELETE CASCADE,
    token_hash TEXT NOT NULL,
    name TEXT NOT NULL,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE webhooks (
    id TEXT PRIMARY KEY,
    project_id TEXT REFERENCES projects(id) ON DELETE CASCADE,
    kind TEXT NOT NULL DEFAULT 'generic', -- 'generic' | 'github_dispatch'
    url TEXT NOT NULL DEFAULT '',         -- kind=generic uniquement
    secret TEXT NOT NULL DEFAULT '',      -- kind=generic uniquement (HMAC)
    config TEXT,                          -- kind=github_dispatch uniquement, JSON {owner, repo, token}
    events TEXT NOT NULL, -- Liste JSON des évènements (ex: ["content.create", "content.update"])
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE global_logs (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id TEXT,
    project_id TEXT,
    action TEXT NOT NULL,
    details JSON,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);
```

> **Précisions** :
> - `users.id` et `projects.id` doivent être des identifiants opaques non énumérables (UUID v4 ou équivalent) pour éviter les attaques par énumération sur les endpoints API.
> - `password_hash` doit être généré avec un algorithme de hachage lent (bcrypt ou argon2id), jamais MD5/SHA seul.
> - `api_tokens.token_hash` : le token en clair n'est affiché qu'une seule fois à la création ; seul son hash est persisté.
> - `global_logs.action` suit une convention `<ressource>.<verbe>` (ex : `project.create`, `user.suspend`) cohérente avec `webhooks.events`.
> - `webhooks.kind` distingue deux mécanismes de livraison, tous deux couverts par la même politique de retry/backoff (`pkg/webhooks.Dispatcher`) :
>   - `generic` (défaut) : POST signé (HMAC-SHA256, header `X-TriCMS-Signature: sha256=<hex>`) vers `webhooks.url`, avec `webhooks.secret` en clair.
>   - `github_dispatch` : déclenche `POST https://api.github.com/repos/{owner}/{repo}/dispatches` (`repository_dispatch`) au lieu d'une URL arbitraire — pensé pour republier un site statique (ex: SvelteKit/Cloudflare Pages) dès qu'un contenu est publié, sans relais intermédiaire. `webhooks.config` est un JSON `{"owner", "repo", "token"}` ; `token` (PAT GitHub) y est chiffré au repos (AES-256-GCM, `pkg/auth.Encryptor`, clé dérivée de `TRICMS_ENCRYPTION_KEY`) et n'est jamais renvoyé en clair par l'API ou l'IHM une fois stocké.

### 2.2 Base Projet (`./data/projects/{project_id}/client.db`)

```sql
-- Organisation hiérarchique des collections
CREATE TABLE _folders (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    parent_id TEXT REFERENCES _folders(id) ON DELETE CASCADE,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

-- Registre des schémas de contenu
CREATE TABLE _schemas (
    slug TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    folder_id TEXT REFERENCES _folders(id) ON DELETE SET NULL,
    definition JSON NOT NULL, -- Inclus la structure des champs et leurs placeholders
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

-- Stockage des instances d'objets
CREATE TABLE _contents (
    id TEXT PRIMARY KEY,
    schema_slug TEXT REFERENCES _schemas(slug) ON DELETE CASCADE,
    data JSON NOT NULL,
    status TEXT CHECK(status IN ('draft', 'published')) DEFAULT 'draft',
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

-- Section isolée des Média/Assets
CREATE TABLE _medias (
    id TEXT PRIMARY KEY,
    filename TEXT NOT NULL,
    mime_type TEXT NOT NULL,
    size INTEGER NOT NULL,
    file_path TEXT NOT NULL, -- Chemin relatif vers ./media/
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);
```

> **Précisions** :
> - Le préfixe `_` sur les tables système (`_folders`, `_schemas`, `_contents`, `_medias`) permet de les distinguer visuellement de futures tables générées dynamiquement.
> - `_folders.parent_id` nul désigne un dossier racine ; la suppression en cascade (`ON DELETE CASCADE`) doit être appliquée avec prudence côté API (confirmation explicite requise pour éviter la perte accidentelle de sous-dossiers et schémas associés).
> - `_schemas.slug` sert d'identifiant stable utilisé dans les URLs d'API (ex : `/api/v1/contents/{slug}`) ; il doit être immuable après création (renommage possible uniquement sur `name`).
> - `_medias.file_path` est relatif à `./data/projects/{project_id}/media/`, jamais à un chemin absolu, afin de conserver la portabilité entre environnements.

---

## 3. Typage des Champs & Support "Simple" / "Collection"

Chaque champ dans la définition d'un schéma suit une structure homogène permettant la définition de placeholders pour les rédacteurs et le basculement en mode **Collection** (liste ordonnée / tableau de valeurs) plutôt que **Simple** (valeur unique).

### Types pris en charge

| Type | Description | Stockage en `data` |
|---|---|---|
| `Text` | Chaîne courte (titre, libellé) | `string` |
| `RichText_MD` | Contenu riche au format Markdown | `string` (Markdown brut) |
| `RichText_HTML` | Contenu riche au format HTML (WYSIWYG) | `string` (HTML sanitisé) |
| `Float` | Nombre décimal | `number` |
| `Int` | Nombre entier | `number` |
| `Date` | Date ou date-heure ISO 8601 | `string` (`YYYY-MM-DD` ou RFC 3339) |
| `Media` | Référence vers un enregistrement `_medias` | `string` (id du média) |
| `Boolean` | Valeur booléenne (oui/non, actif/inactif) | `boolean` |
| `Enum` | Valeur unique choisie parmi une liste fermée définie dans le champ (`options`) | `string` (une des valeurs de `options`) |
| `Reference` | Relation vers un contenu d'un autre schéma (`_contents.id`), le schéma cible étant précisé via `targetSchema` | `string` (id du contenu référencé) |
| `Slug` | Identifiant URL-friendly, généralement dérivé d'un autre champ `Text` | `string` (minuscules, sans accents/espaces) |
| `URL` | Chaîne validée au format URL | `string` |
| `Color` | Couleur au format hexadécimal | `string` (ex: `#1E90FF`) |
| `JSON` | Bloc de données arbitraire non structuré | `object` ou `array` JSON libre |
| `GeoPoint` | Coordonnées géographiques | `object` (`{ "lat": number, "lng": number }`) |

> **Précision sécurité** : le contenu `RichText_HTML` doit être passé par un sanitizer HTML côté serveur avant persistance et avant rendu, afin de prévenir toute injection XSS stockée.
>
> **Précisions complémentaires** :
> - `Enum` : la définition du champ doit inclure un tableau `options` (ex: `"options": ["draft", "review", "approved"]`) ; toute valeur hors de cette liste doit être rejetée à la validation.
> - `Reference` : la définition du champ doit inclure `targetSchema` (le `slug` du schéma référencé) ; l'API doit vérifier l'existence du contenu cible avant écriture et interdire la suppression d'un contenu encore référencé sans confirmation explicite.
> - `Slug` : doit être unique au sein du `schema_slug` concerné ; une contrainte d'unicité applicative (vérifiée en base) est requise en plus de la validation de format.

### Structure JSON de la définition d'un Schéma

```json
{
  "fields": [
    {
      "key": "title",
      "label": "Titre de l'article",
      "type": "Text",
      "cardinality": "Simple",
      "placeholder": "Ex: Lancement de la nouvelle ligne de production",
      "required": true
    },
    {
      "key": "gallery",
      "label": "Galerie d'images",
      "type": "Media",
      "cardinality": "Collection",
      "placeholder": "Sélectionnez ou déposez une liste d'images",
      "required": false
    },
    {
      "key": "specifications",
      "label": "Valeurs numériques de mesure",
      "type": "Float",
      "cardinality": "Collection",
      "placeholder": "Ex: 12.5",
      "required": false
    }
  ]
}
```

> **Note** : lors de l'enregistrement de l'objet dans `_contents.data`, les champs auto-générés `created_at` et `updated_at` sont ajoutés systématiquement au niveau racine (en plus des colonnes homonymes de la table `_contents`, utilisées pour l'indexation et le tri).
>
> **Précision** : quand `cardinality` vaut `Collection`, la valeur du champ dans `data` doit être un tableau JSON d'éléments du `type` déclaré (ex : `"specifications": [12.5, 8.2]`), même vide (`[]`) si aucune valeur n'a été saisie. Quand `cardinality` vaut `Simple`, la valeur est stockée directement (pas de tableau).

---

## 4. Spécification Front-End (Structure HTMX)

L'IHM est entièrement rendue par le binaire Go via des templates HTML dynamisés par HTMX (rendu serveur, mises à jour partielles du DOM sans framework JS lourd).

### 4.1 Zone Supérieure (Navigation globale)

- **Fil d'Ariane dynamique** : `[Projet / Administration] > [Nom de la Page]`.
- **Sélecteur de Projet** :
  - Présent si l'utilisateur a accès à plus d'1 projet.
  - Si l'utilisateur n'a accès qu'à 1 seul projet, sélection automatique et masquage du composant `<select>`.

### 4.2 Menu Latéral / Sous-menu d'un Projet

```text
├── [Conception]    --> Visible uniquement si rôle == CONCEPTEUR (ou ADMIN)
├── [Collections]   --> Visible par tous (CONCEPTEUR, GESTIONNAIRE, REDACTEUR)
├── [Médias]        --> Visible par tous (Section dédiée à la gestion des fichiers)
├── [Utilisateurs]  --> Visible uniquement si rôle == GESTIONNAIRE (ou ADMIN)
├── [API]           --> Visible uniquement si rôle == ADMIN
├── [Webhooks]      --> Visible uniquement si rôle == ADMIN
└── [Logs]          --> Visible uniquement si rôle == ADMIN
```

### 4.3 Vue Administration (Rôles ADMIN globaux)

Accessible depuis le fil d'Ariane via l'élément `[Administration]` :

- **Gestion des Projets** : création/suppression de projets (déclenche la création/suppression du dossier `./data/projects/{id}`).
- **Gestion des Comptes Globaux** : création, modification et suspension des utilisateurs du système.
- **Matrice Globale des Droits** : affectation directe des rôles (CONCEPTEUR, GESTIONNAIRE, REDACTEUR) par projet et par utilisateur.

> **Précision** : la suppression d'un projet doit être une action à double confirmation (ex : saisie du nom du projet), car elle entraîne la suppression irréversible du dossier `client.db` et des médias associés.

### 4.4 Vue Utilisateurs Projet (Rôle GESTIONNAIRE)

- Liste des utilisateurs affectés au projet courant.
- Formulaire de création rapide / ajout d'un utilisateur existant : attribution restreinte aux rôles **GESTIONNAIRE** ou **REDACTEUR** (un GESTIONNAIRE ne peut pas s'auto-attribuer ou attribuer le rôle **CONCEPTEUR**).

---

## 5. Exigence de Qualité Logicielle : 90 % de Code Coverage

Afin d'assurer la robustesse du binaire Go, la fiabilité de la manipulation SQLite et la sécurité de l'isolation multi-tenant, une couverture de tests minimale de **90 %** est imposée.

```text
                   GO TEST COVERAGE TARGET: >= 90%
┌───────────────────────────────┬─────────────────────────────────┐
│ Package                       │ Stratégie de Test               │
├───────────────────────────────┼─────────────────────────────────┤
│ /pkg/auth                     │ Tests unitaires (Hash/JWT/RBAC)  │
│ /pkg/storage (SQLite/Driver)  │ Tests d'intégration SQLite DB    │
│ /pkg/schema                   │ Validation des payloads JSON     │
│ /pkg/api (Handlers & Router)  │ Tests HTTP End-to-End (httptest) │
│ /pkg/webhooks                 │ Tests de dispatch & retries      │
└───────────────────────────────┴─────────────────────────────────┘
```

### Directives d'implémentation des tests

- **Tests unitaires BDD** : utilisation de bases SQLite en mémoire (`:memory:`) pour valider les requêtes et transactions sans persistance disque durant le cycle de build.
- **Tests d'intégration HTTP (HTMX & REST API)** : utilisation du package Go `net/http/httptest` pour simuler l'ensemble des requêtes HTTP (authentification, vérification des rôles, gestion des erreurs 403, création de contenus).
- **Contrôle CI/CD** : injection d'une étape de contrôle dans le workflow de build qui interrompt le déploiement si le rapport de couverture baisse sous le seuil requis :

```bash
go test -coverprofile=coverage.out ./...
go tool cover -func=coverage.out | grep total | awk '{print $3}'
# Doit échouer si < 90.0%
```

> **Précision** : les tests de `/pkg/webhooks` doivent couvrir explicitement les cas d'échec de livraison (timeout, code HTTP 4xx/5xx du récepteur) et la politique de retry (nombre de tentatives, backoff), pas uniquement le cas nominal de succès.
