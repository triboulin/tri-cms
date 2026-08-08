<p align="center">
  <img src="web/static/img/logo.svg" alt="triCMS logo" width="200" height="200">
</p>

CMS headless multi-tenant écrit en Go, implémentant la spécification `specs.md` :
RBAC à deux niveaux (ADMIN global + rôles projet cumulatifs), isolation SQLite
par projet, typage de champs Simple/Collection, IHM d'administration HTMX
rendue côté serveur, et webhooks avec retries (livraison signée générique, ou
`repository_dispatch` GitHub direct pour republier un site statique).

## Démarrage rapide

```bash
go build -o tricms ./cmd/tricms && export TRICMS_ENCRYPTION_KEY="changeme4prd" && ./tricms
```
ou sur Windows  :
```cmd
set TRICMS_ENCRYPTION_KEY="changeme4prd" && go build -o tricms.exe ./cmd/tricms && .\tricms.exe
```


Variables d'environnement :

| Variable | Défaut | Rôle |
|---|---|---|
| `TRICMS_ADDR` | `:8080` | Adresse d'écoute HTTP |
| `TRICMS_DATA_DIR` | `./data` | Racine de `system.db` et `./data/projects/{id}/` |
| `TRICMS_ENCRYPTION_KEY` | *(généré, éphémère)* | Clé de chiffrement (AES-256-GCM) des secrets stockés en base — actuellement le token GitHub des webhooks `kind=github_dispatch` — et source de dérivation de la clé de signature des sessions JWT. À fixer en production, sinon les sessions sont invalidées et ces tokens deviennent indéchiffrables à chaque redémarrage. |
| `TRICMS_BOOTSTRAP_EMAIL` | `admin@tricms.local` | Email du premier compte ADMIN créé automatiquement si `system.db` est vide |

Au premier démarrage sans utilisateur existant, un compte **ADMIN** est créé
automatiquement avec un mot de passe généré aléatoirement. Ce mot de passe est
affiché sur la page de connexion (et une seule fois dans les logs) jusqu'à la
première connexion réussie, moment auquel il est effacé et ne peut plus être
récupéré.

L'interface d'administration HTMX est servie sur `/` (redirige vers `/login`
si non authentifié) ; l'API JSON est sous `/api/v1/*`.

## Structure

```
cmd/tricms/       point d'entrée (wiring, bootstrap, config)
pkg/storage/      system.db + client.db par projet (migrations, repositories)
pkg/auth/         hash bcrypt, JWT de session, tokens API, RBAC (hiérarchie des rôles)
pkg/schema/       validation des définitions de schéma & des payloads de contenu
pkg/webhooks/     dispatch des évènements avec retries/backoff exponentiel
pkg/api/          router chi, middlewares RBAC, handlers REST + HTMX
web/              templates HTML (embed.FS) + assets statiques (CSS, HTMX via CDN)
```

## Modèle de sécurité

- **ADMIN** (global, `system.db`) : accès total, gestion des projets, comptes,
  tokens API, webhooks, logs.
- Rôles projet cumulatifs : `REDACTEUR ⊂ GESTIONNAIRE ⊂ CONCEPTEUR`, stockés
  dans `project_permissions`.
- Un token API (`Authorization: Bearer ...`) équivaut systématiquement à un
  compte **ADMIN global complet** : il n'existe pas de token restreint à un
  projet ou à un rôle inférieur. Les tokens sont générés exclusivement
  depuis Administration › API (jamais depuis un projet). Seule exception :
  la suppression d'un projet n'a **aucune route API**, même pour un token —
  c'est une action irréversible réservée à l'IHM HTMX (double confirmation
  par saisie du nom exact).
- Chaque middleware RBAC (`pkg/api/middleware.go`) est couvert par des tests
  `httptest` vérifiant explicitement les 403/404, y compris les cas limites
  de la matrice de rôles (ex : un GESTIONNAIRE ne peut pas s'auto-attribuer
  ou attribuer le rôle CONCEPTEUR — seul ADMIN le peut) et le fait qu'un
  token accède à n'importe quel projet, pas seulement celui pour lequel il
  a été créé à l'origine (il n'y a d'ailleurs plus de notion de projet
  "d'origine").

## Tests & couverture

```bash
go test ./pkg/... -cover
```

La cible de la spec (§5) est 90 %. L'essentiel des chemins fonctionnels et
de sécurité (RBAC, validation de schéma, isolation multi-tenant, retries
webhooks, chiffrement/déchiffrement des secrets de webhook) est testé ; le
reliquat sous 90 % correspond principalement à des branches défensives
(erreurs internes de stockage difficilement déclenchables sans mock
d'infrastructure) plutôt qu'à des chemins fonctionnels non couverts.
`pkg/api` a reculé par rapport à la mesure précédente (~79 %) avec l'ajout
du webhook `kind=github_dispatch` (validation, chiffrement, formulaire
HTMX) : le nouveau code est lui-même testé à 70-100 % selon les fonctions,
mais dilue la moyenne du paquet le temps que la couverture d'ensemble
rattrape sa taille. `cmd/tricms` (wiring pur, sans tests) est validé par un
test de fumée manuel (démarrage réel + requêtes HTTP).
