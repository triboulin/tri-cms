<p align="center">
  <img src="web/static/img/logo.svg" alt="triCMS logo" width="128" height="128">
</p>

<h1 align="center">triCMS</h1>

CMS headless multi-tenant écrit en Go, implémentant la spécification `specs.md` :
RBAC à deux niveaux (ADMIN global + rôles projet cumulatifs), isolation SQLite
par projet, typage de champs Simple/Collection, IHM d'administration HTMX
rendue côté serveur, et webhooks avec retries (livraison signée générique, ou
`repository_dispatch` GitHub direct pour republier un site statique).

## Démarrage rapide

```bash
go build -o tricms ./cmd/tricms && TRICMS_JWT_SECRET="change-me-in-production" && TRICMS_ENCRYPTION_KEY="change-me-in-production" && ./tricms
```
ou sur Windows  :
```cmd
set TRICMS_JWT_SECRET="change-me-in-production" && set TRICMS_ENCRYPTION_KEY="change-me-in-production" && go build -o tricms.exe ./cmd/tricms && .\tricms.exe
```


Variables d'environnement :

| Variable | Défaut | Rôle |
|---|---|---|
| `TRICMS_ADDR` | `:8080` | Adresse d'écoute HTTP |
| `TRICMS_DATA_DIR` | `./data` | Racine de `system.db` et `./data/projects/{id}/` |
| `TRICMS_JWT_SECRET` | *(généré, éphémère)* | Clé de signature des sessions JWT — à fixer en production |
| `TRICMS_ENCRYPTION_KEY` | *(généré, éphémère)* | Clé de chiffrement (AES-256-GCM) des secrets stockés en base — actuellement le token GitHub des webhooks `kind=github_dispatch`. À fixer en production, sinon ces tokens deviennent indéchiffrables au redémarrage. |
| `TRICMS_BOOTSTRAP_EMAIL` | `admin@tricms.local` | Email du premier compte ADMIN créé automatiquement si `system.db` est vide |

Au premier démarrage sans utilisateur existant, un compte **ADMIN** est créé
automatiquement ; l'email et le mot de passe généré sont affichés une seule
fois dans les logs.

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
- Un token API (`Authorization: Bearer ...`) donne un accès équivalent
  REDACTEUR, strictement limité au projet pour lequel il a été émis.
- Chaque middleware RBAC (`pkg/api/middleware.go`) est couvert par des tests
  `httptest` vérifiant explicitement les 403/404, y compris les cas limites
  de la matrice de rôles (ex : un GESTIONNAIRE ne peut pas s'auto-attribuer
  ou attribuer le rôle CONCEPTEUR — seul ADMIN le peut).

## Tests & couverture

```bash
go test ./pkg/... -cover
```

Couverture par package (dernière exécution) :

| Package | Couverture |
|---|---|
| `pkg/webhooks` | ~93 % |
| `pkg/auth` | ~92 % |
| `pkg/schema` | ~89 % |
| `pkg/storage` | ~81 % |
| `pkg/api` | ~72 % |
| **Total `pkg/...`** | **~76 %** |

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

## Limites connues / pistes d'évolution

- L'IHM HTMX couvre la structure de navigation complète du §4 (fil d'Ariane,
  sélecteur de projet, sidebar conditionnelle par rôle, vues Administration
  et Utilisateurs Projet) en lecture ; les formulaires de création/édition
  avancés restent à construire par-dessus l'API JSON déjà fonctionnelle.
- La vérification d'unicité des champs `Slug` au niveau contenu est en
  O(n) sur le nombre de contenus du schéma (acceptable au vu du volume
  attendu d'un CMS mono-instance, à revoir si besoin d'indexation dédiée).
