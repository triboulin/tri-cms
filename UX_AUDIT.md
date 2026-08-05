# Audit UI/UX — triCMS (interface HTMX)

Date : 2026-08-05
Périmètre : l'intégralité de l'interface admin server-rendue (`web/templates/*`, `web/static/css/app.css`, `web/static/js/*`) et les handlers HTMX associés (`pkg/api/htmx_*.go`). L'API JSON pure n'est pas concernée (pas d'UI).

Méthodologie : lecture exhaustive de chaque template et de son handler, recherche de motifs à risque (`confirm(`, `prompt(`, `@media`, `aria-`, `redirectWithFlash(..., formPath`), et vérification de bout en bout de plusieurs parcours (connexion, création de projet, création de schéma + contenu, upload média) via un binaire compilé et des requêtes HTTP réelles.

Chaque constat est noté par sévérité : **Critique** (bloque ou casse un usage), **Élevé** (gêne significative, contournable), **Moyen** (frottement notable), **Faible** (finition).

---

## 1. Navigation

### 1.1 — Les menus du fil d'ariane ne s'ouvrent qu'au survol (Critique)
Depuis la suppression de la sidebar, toute la navigation (changer de projet, aller en Administration, changer de section) repose sur `.tri-crumb:hover`. Un survol seul ne fonctionne ni au clavier (pas de `:focus`), ni au doigt sur tablette/mobile (pas de `:hover` sur écran tactile). Sur ces supports, une partie de l'application devient **inatteignable**.
- Preuve : `web/static/css/app.css`, règles `.tri-crumb:hover .tri-crumb-dropdown`.
- **Corrigé dans cette session** (voir §7) : bascule au clic en plus du survol, plus support clavier (`:focus-within`, `Échap` pour fermer, fermeture au clic extérieur).

### 1.2 — Pas de recherche globale
Aucun moyen de sauter directement vers un projet, un schéma ou un contenu sans naviguer palier par palier. Pour une instance avec beaucoup de projets/schémas, c'est un frein réel à la vitesse d'usage (contrairement à l'esprit "Airtable" visé).
- Sévérité : Moyen. Reporté (voir §8, backlog) — nécessite une décision produit sur la portée (projets seuls ? contenus aussi ?) que je ne tranche pas ici.

### 1.3 — Pas de fil d'ariane de niveau 3 cohérent partout
`schema_edit` et `content_form`/`content_list` affichent un "Leaf" (nom du schéma / contenu) mais celui-ci n'est jamais lui-même un lien vers un niveau intermédiaire cohérent — correct dans l'ensemble, mais la profondeur maximale (projet → section → leaf) ne permet pas de revenir "d'un cran" pour un contenu précis sans repasser par la liste. Acceptable en l'état, noté pour référence.
- Sévérité : Faible.

---

## 2. Formulaires et récupération d'erreur

### 2.1 — Perte totale de la saisie en cas d'erreur de validation (Critique)
Tous les formulaires de création suivent le motif Post/Redirect/Get : en cas d'erreur (champ requis manquant, JSON invalide, validation de schéma…), le handler fait `redirectWithFlash(w, r, formPath, "...", "error")`, qui redirige vers la page **vierge** du formulaire. Tout ce que l'utilisateur avait saisi est perdu — particulièrement douloureux sur :
- la création de schéma (`htmx_conception.go`, formulaire avec plusieurs champs dynamiques du field-builder) ;
- la création/édition de contenu (`htmx_content.go`), potentiellement de nombreux champs.
- Preuve : 87 occurrences de `redirectWithFlash(w, r, formPath|back, ...)` dans `pkg/api/htmx_*.go` ; le cas le plus coûteux est `htmxCreateContent`/`htmxUpdateContent` et `htmxCreateSchema`.
- **Corrigé dans cette session** pour la création/édition de contenu et de schéma (voir §7) : le formulaire est ré-affiché rempli avec les valeurs saisies et un message d'erreur contextualisé, au lieu d'une redirection vers une page vide.

### 2.2 — Pas de validation en direct ni d'erreurs par champ
Toutes les erreurs de validation (y compris "ce champ est requis") remontent sous forme d'un unique message flash générique en haut de page, sans indiquer quel champ est en cause. L'utilisateur doit deviner. Le HTML5 `required` couvre une partie des cas basiques côté navigateur, mais les erreurs métier (slug déjà pris, JSON invalide, référence introuvable) ne sont signalées qu'après un aller-retour serveur complet.
- Sévérité : Élevé. Reporté — corriger proprement demanderait de faire remonter l'erreur avec la clé du champ concerné jusqu'au gabarit, un chantier plus large que le correctif de §2.1.

### 2.3 — Éditeur JSON en texte brut
Le type de champ `JSON` est un simple `<textarea>` : aucune coloration syntaxique, aucune validation avant soumission, aucun formatage automatique. Une virgule oubliée envoie l'utilisateur droit vers le problème du §2.1.
- Sévérité : Moyen. Reporté (nécessiterait un éditeur type CodeMirror/Ace, en cohérence avec les éditeurs RichText déjà ajoutés).

---

## 3. Actions destructrices

### 3.1 — `confirm()`/`prompt()` natifs du navigateur (Élevé)
Toute suppression (contenu, schéma, dossier, média, webhook, token, compte, permission, utilisateur de projet) déclenche un `window.confirm()` natif — moche, non stylé, incohérent avec le reste de l'interface, et bloquant (gèle l'onglet). La suppression de projet utilise en plus un `window.prompt()` demandant de retaper le nom exact — fonctionnellement bien pensé (double confirmation) mais l'exécution (popup navigateur grisâtre) détonne complètement avec le design de l'application.
- Preuve : 10 occurrences de `confirm(`/`prompt(` réparties sur 9 templates.
- **Corrigé dans cette session** (voir §7) : modale HTML/CSS cohérente avec le design, avec variante "retaper le nom" pour la suppression de projet.

---

## 4. Feedback et états de chargement

### 4.1 — Aucun retour visuel à la soumission (hors upload média)
En dehors de l'upload de média (barre de progression ajoutée précédemment), aucun bouton "Enregistrer"/"Créer" ne se désactive ni n'affiche d'indicateur de chargement pendant la requête. Sur une connexion lente ou un gros formulaire de contenu, un double-clic peut soumettre deux fois.
- Sévérité : Moyen. **Corrigé dans cette session** : désactivation générique du bouton de soumission + micro-spinner sur tous les formulaires `.tri-form`.

### 4.2 — Déconnexion de session silencieuse
Si la session expire (cookie JWT expiré), l'utilisateur est renvoyé sur `/login` sans aucune explication ni mémorisation de la page qu'il visitait. Il peut croire à un bug.
- Sévérité : Moyen. Reporté — corriger proprement demande de faire porter un paramètre de retour (`?next=`) à travers tout le pipeline d'authentification, un changement plus large que ce qui est traité dans cette passe.

---

## 5. Accessibilité

### 5.1 — Icônes décoratives non masquées aux lecteurs d'écran (Élevé)
Chaque `<span class="material-icons">` contient un mot en clair (`delete`, `expand_more`, `chevron_right`…) que Google Fonts transforme visuellement en pictogramme via une police à ligatures — mais un lecteur d'écran lit le texte source, donc littéralement "delete", "expand more", "chevron right" à chaque icône, y compris celles purement décoratives à côté d'un texte déjà explicite. C'est du bruit constant pour un usage non-voyant.
- **Corrigé dans cette session** : `aria-hidden="true"` ajouté systématiquement sur ces spans.

### 5.2 — Pas de nom accessible sur les boutons icône-seule
Les boutons d'action (`tri-icon-btn`) n'ont qu'un `title="..."`, jamais d'`aria-label`. `title` seul est un filet de sécurité minimal et inconstant selon lecteurs d'écran/navigateurs.
- **Corrigé dans cette session** : `aria-label` dupliqué depuis `title` sur les boutons icône-seule les plus courants (suppression, édition, bascule de statut).

### 5.3 — Pas de style `:focus-visible` personnalisé
Le CSS ne définit aucun style de focus clavier ; le focus par défaut du navigateur reste donc actif (pas de perte totale), mais rien n'a été pensé pour la navigation clavier (ordre logique non vérifié, focus peu visible sur fond blanc avec les boutons `ghost`).
- **Corrigé dans cette session** : anneau de focus visible cohérent avec l'accent bleu, appliqué globalement.

---

## 6. Responsive / mobile

### 6.1 — Aucune media query dans toute la feuille de style (Critique)
`app.css` ne contient pas une seule règle `@media`. Sur un écran étroit : les tableaux (`tri-table`) débordent sans scroll horizontal dédié, la grille de médias peut se tasser, et surtout le fil d'ariane (seul système de navigation restant) n'a pas été pensé pour le tactile (cf. §1.1). Sans intervention, l'admin est **substantiellement inutilisable sur mobile/tablette**.
- **Corrigé dans cette session** : point de rupture à 720 px — tableaux avec défilement horizontal dédié, grilles en une colonne, formulaires en colonne unique, topbar qui s'empile proprement.

---

## 7. Correctifs appliqués dans cette session

Décisions prises de façon autonome, par ordre d'impact / effort, sans introduire de nouvelle dépendance externe (uniquement HTML/CSS/JS natifs + le Go déjà en place) :

1. **Navigation tactile/clavier** : les dropdowns du fil d'ariane et le popover de création de dossier basculent désormais aussi au clic (en plus du survol), se ferment à l'extérieur ou avec `Échap`, et exposent `aria-haspopup`/`aria-expanded`.
2. **Modale de confirmation stylée** : tous les `confirm()`/`prompt()` natifs sont remplacés par une modale HTML cohérente avec le design ; la suppression de projet garde son exigence de retaper le nom exact, mais dans un champ de la modale plutôt qu'un `prompt()` du navigateur.
3. **Responsive** : media queries pour mobile/tablette (tableaux scrollables, grilles à une colonne, topbar empilée).
4. **Accessibilité de base** : `aria-hidden` sur les icônes décoratives, `aria-label` sur les boutons icône-seule les plus fréquents, styles `:focus-visible`.
5. **Conservation de la saisie sur erreur** : les formulaires de création/édition de contenu et de schéma ré-affichent désormais les valeurs saisies avec l'erreur, au lieu de rediriger vers une page vide.
6. **Recherche client-side** sur la grille de contenus (filtre instantané, sans aller-retour serveur).
7. **Feedback de soumission** : tous les boutons de formulaire se désactivent avec un indicateur de chargement pendant la requête ; pages 404/500 et message de session expirée restylés pour rester dans l'identité visuelle de l'application.

## 8. Backlog (identifié mais non traité dans cette passe)

Ces points sont réels mais nécessitent soit une décision produit (portée, priorité), soit un chantier disproportionné par rapport à cette itération :

- Recherche/filtrage globale multi-projets.
- Erreurs de validation par champ (et non un message global).
- Éditeur JSON avec coloration syntaxique.
- Paramètre `?next=` pour revenir à la page d'origine après reconnexion.
- Pagination / tri / filtres avancés sur la grille de contenus au-delà d'une recherche texte simple (utile dès que le volume de contenus grandit).
- Adoption plus large de htmx (le script est chargé mais aucune vue ne l'utilise réellement ; tout est en rechargement de page complet). Ce n'est pas un bug, mais un choix d'architecture qui limite la fluidité perçue.
- Aperçu média inline dans la grille de contenus pour les champs de type Media (actuellement seule la page Médias a des vignettes).
