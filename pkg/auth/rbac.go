package auth

import "tricms/pkg/storage"

// Section identifies a menu/API area gated by role, mirroring section 4.2
// of the spec (sidebar visibility rules).
type Section string

const (
	SectionConception  Section = "conception"  // schemas/folders structure
	SectionCollections Section = "collections" // content CRUD
	SectionMedias      Section = "medias"
	SectionUsers       Section = "users"    // project-scoped user management
	SectionAPI         Section = "api"      // global-admin only
	SectionWebhooks    Section = "webhooks" // global-admin only
	SectionLogs        Section = "logs"     // global-admin only
)

// sectionMinRole maps a section to the minimum *project* role required.
// Sections not present here (API, Webhooks, Logs) are global-admin only
// and have no project-role equivalent.
var sectionMinRole = map[Section]storage.Role{
	SectionConception:  storage.RoleConcepteur,
	SectionCollections: storage.RoleRedacteur,
	SectionMedias:      storage.RoleRedacteur,
	SectionUsers:       storage.RoleGestionnaire,
}

// globalAdminOnlySections lists sections visible only to ADMIN (global scope),
// regardless of any project role the user might hold.
var globalAdminOnlySections = map[Section]bool{
	SectionAPI:      true,
	SectionWebhooks: true,
	SectionLogs:     true,
}

// HasRole reports whether userRole meets or exceeds required, honoring the
// cumulative hierarchy REDACTEUR ⊂ GESTIONNAIRE ⊂ CONCEPTEUR.
func HasRole(userRole storage.Role, required storage.Role) bool {
	if !required.Valid() {
		return false
	}
	return userRole.Level() >= required.Level()
}

// CanAccessSection reports whether a principal (global-admin flag + optional
// project role, nil if the user has no relation to the project) may see/use
// a given section.
func CanAccessSection(isGlobalAdmin bool, projectRole *storage.Role, section Section) bool {
	if isGlobalAdmin {
		return true
	}
	if globalAdminOnlySections[section] {
		return false
	}
	min, ok := sectionMinRole[section]
	if !ok {
		return false
	}
	if projectRole == nil {
		return false
	}
	return HasRole(*projectRole, min)
}

// CanAccessProject reports whether a user may operate within a project at
// all: either as global admin, or by holding any project permission row.
func CanAccessProject(isGlobalAdmin bool, projectRole *storage.Role) bool {
	return isGlobalAdmin || projectRole != nil
}

// CanAssignRole reports whether an actor (identified by global-admin flag and
// their own project role, if any) may assign `target` role to someone else
// within a project, via the project "Utilisateurs" view (spec 4.4). That view
// -- and therefore role assignment through it -- is restricted to granting
// GESTIONNAIRE or REDACTEUR only, never CONCEPTEUR: assigning CONCEPTEUR is
// reserved for ADMIN via the global rights matrix (spec 4.3). This holds
// whether the actor reaches the view as GESTIONNAIRE or as CONCEPTEUR
// (CONCEPTEUR inherits GESTIONNAIRE's rights, not more, for this action).
func CanAssignRole(isGlobalAdmin bool, actorRole *storage.Role, target storage.Role) bool {
	if !target.Valid() {
		return false
	}
	if isGlobalAdmin {
		return true
	}
	if actorRole == nil {
		return false
	}
	if !HasRole(*actorRole, storage.RoleGestionnaire) {
		return false
	}
	return target == storage.RoleGestionnaire || target == storage.RoleRedacteur
}
