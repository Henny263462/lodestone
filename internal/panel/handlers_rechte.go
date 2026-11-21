package panel

import "net/http"

// registerRechteRoutes hängt die Routen der Einzelrechte ein.
func (p *Panel) registerRechteRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/permissions", p.authenticate(p.listPermissions))
}

// permissionCatalogDTO ist der Katalog der Rechte und die Vorlagen der Rollen für die Oberfläche.
type permissionCatalogDTO struct {
	Permissions []PermissionInfo        `json:"permissions"`
	Roles       map[string][]Permission `json:"roles"`
}

func (p *Panel) listPermissions(w http.ResponseWriter, _ *http.Request) {
	result := permissionCatalogDTO{Permissions: make([]PermissionInfo, 0, len(permissionCatalog)), Roles: map[string][]Permission{}}
	for _, info := range permissionCatalog {
		result.Permissions = append(result.Permissions, permissionByID[info.ID])
	}
	for name, rank := range roleRanks {
		result.Roles[name] = accessFor(rank, nil).List()
	}
	writeJSON(w, http.StatusOK, result)
}

// MemberDTO ist ein Mitglied mit seinen wirksamen Rechten.
type MemberDTO struct {
	Member
	Permissions []Permission `json:"permissions"`
	// CustomPermissions ist wahr, wenn das Mitglied angepasste Rechte statt der Vorlage seiner Rolle hat.
	CustomPermissions bool `json:"customPermissions"`
}

func (p *Panel) memberDTOs(projectID string) ([]MemberDTO, error) {
	members, err := p.store.ListMembers(projectID)
	if err != nil {
		return nil, err
	}
	custom, err := p.store.MemberPermissions(projectID)
	if err != nil {
		return nil, err
	}
	result := make([]MemberDTO, len(members))
	for i, member := range members {
		access := accessFor(roleRanks[member.Role], custom[member.UserID])
		result[i] = MemberDTO{Member: member, Permissions: access.List(), CustomPermissions: access.Custom}
	}
	return result, nil
}

// checkGrant prüft, ob actor einem Mitglied die Rechte after geben darf, wenn es vorher before hatte: Alles, was neu
// dazukommt, muss actor selbst haben. So kann sich niemand (und niemandem) mehr Rechte geben, als er selbst hat.
func checkGrant(actor, before, after Access) error {
	var added []Permission
	for _, perm := range after.List() {
		if !before.perms[perm] {
			added = append(added, perm)
		}
	}
	if missing := actor.missing(added); len(missing) > 0 {
		labels := ""
		for i, perm := range missing {
			if i > 0 {
				labels += ", "
			}
			labels += "„" + permissionLabel(perm) + "“"
		}
		return fail(http.StatusForbidden, "du kannst keine Rechte vergeben, die du selbst nicht hast: "+labels)
	}
	return nil
}
