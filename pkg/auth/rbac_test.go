package auth

import (
	"testing"

	"tricms/pkg/storage"
)

func roleRef(r storage.Role) *storage.Role { return &r }

func TestHasRole_Hierarchy(t *testing.T) {
	if !HasRole(storage.RoleConcepteur, storage.RoleGestionnaire) {
		t.Fatal("CONCEPTEUR should satisfy GESTIONNAIRE requirement")
	}
	if !HasRole(storage.RoleConcepteur, storage.RoleRedacteur) {
		t.Fatal("CONCEPTEUR should satisfy REDACTEUR requirement")
	}
	if !HasRole(storage.RoleGestionnaire, storage.RoleRedacteur) {
		t.Fatal("GESTIONNAIRE should satisfy REDACTEUR requirement")
	}
	if HasRole(storage.RoleRedacteur, storage.RoleGestionnaire) {
		t.Fatal("REDACTEUR should NOT satisfy GESTIONNAIRE requirement")
	}
	if HasRole(storage.RoleRedacteur, storage.RoleConcepteur) {
		t.Fatal("REDACTEUR should NOT satisfy CONCEPTEUR requirement")
	}
	if HasRole(storage.RoleConcepteur, storage.Role("bogus")) {
		t.Fatal("invalid required role should never be satisfied")
	}
}

func TestCanAccessSection(t *testing.T) {
	cases := []struct {
		name      string
		isAdmin   bool
		role      *storage.Role
		section   Section
		wantAllow bool
	}{
		{"global admin sees everything incl. logs", true, nil, SectionLogs, true},
		{"redacteur sees collections", false, roleRef(storage.RoleRedacteur), SectionCollections, true},
		{"redacteur sees medias", false, roleRef(storage.RoleRedacteur), SectionMedias, true},
		{"redacteur cannot see conception", false, roleRef(storage.RoleRedacteur), SectionConception, false},
		{"redacteur cannot see users", false, roleRef(storage.RoleRedacteur), SectionUsers, false},
		{"gestionnaire sees users", false, roleRef(storage.RoleGestionnaire), SectionUsers, true},
		{"gestionnaire cannot see conception", false, roleRef(storage.RoleGestionnaire), SectionConception, false},
		{"concepteur sees conception", false, roleRef(storage.RoleConcepteur), SectionConception, true},
		{"concepteur sees users (inherits gestionnaire)", false, roleRef(storage.RoleConcepteur), SectionUsers, true},
		{"concepteur cannot see webhooks (admin only)", false, roleRef(storage.RoleConcepteur), SectionWebhooks, false},
		{"no relation to project sees nothing", false, nil, SectionCollections, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := CanAccessSection(c.isAdmin, c.role, c.section)
			if got != c.wantAllow {
				t.Fatalf("CanAccessSection(%v, %v, %s) = %v, want %v", c.isAdmin, c.role, c.section, got, c.wantAllow)
			}
		})
	}
}

func TestCanAccessProject(t *testing.T) {
	if !CanAccessProject(true, nil) {
		t.Fatal("global admin must access any project")
	}
	if CanAccessProject(false, nil) {
		t.Fatal("user without permission row must not access project")
	}
	if !CanAccessProject(false, roleRef(storage.RoleRedacteur)) {
		t.Fatal("user with a role must access project")
	}
}

func TestCanAssignRole(t *testing.T) {
	cases := []struct {
		name    string
		isAdmin bool
		actor   *storage.Role
		target  storage.Role
		want    bool
	}{
		{"admin can assign concepteur", true, nil, storage.RoleConcepteur, true},
		{"admin can assign gestionnaire", true, nil, storage.RoleGestionnaire, true},
		{"gestionnaire can assign gestionnaire", false, roleRef(storage.RoleGestionnaire), storage.RoleGestionnaire, true},
		{"gestionnaire can assign redacteur", false, roleRef(storage.RoleGestionnaire), storage.RoleRedacteur, true},
		{"gestionnaire cannot assign concepteur", false, roleRef(storage.RoleGestionnaire), storage.RoleConcepteur, false},
		{"concepteur cannot assign concepteur via users view", false, roleRef(storage.RoleConcepteur), storage.RoleConcepteur, false},
		{"concepteur can assign gestionnaire", false, roleRef(storage.RoleConcepteur), storage.RoleGestionnaire, true},
		{"redacteur cannot assign anything", false, roleRef(storage.RoleRedacteur), storage.RoleRedacteur, false},
		{"no role cannot assign", false, nil, storage.RoleRedacteur, false},
		{"invalid target role rejected even for admin", true, nil, storage.Role("bogus"), false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := CanAssignRole(c.isAdmin, c.actor, c.target)
			if got != c.want {
				t.Fatalf("CanAssignRole(%v, %v, %s) = %v, want %v", c.isAdmin, c.actor, c.target, got, c.want)
			}
		})
	}
}
