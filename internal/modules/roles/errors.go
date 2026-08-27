package roles

import "fmt"

// VersionConflictError is returned by UpdateRole/ReplacePermissions when the
// caller's submitted version no longer matches the row's current version —
// someone else wrote to this role between the caller's GET and this write.
// Carries CurrentVersion so the handler can hand it back to the front
// (§2: "409 avec la version courante, pour que le front propose de
// recharger"), which a plain sentinel error cannot do.
type VersionConflictError struct {
	CurrentVersion int
}

func (e *VersionConflictError) Error() string {
	return fmt.Sprintf("version conflict: current version is %d", e.CurrentVersion)
}

// RoleHasMembersError is returned by ArchiveRole when at least one
// users_rights row still references the role (enabled or not — see
// RoleHasMembersError's Count doc). Carries Count so the handler can report
// it (§2 G5: "Renvoie le nombre de porteurs dans l'erreur pour que le front
// puisse proposer la réaffectation").
type RoleHasMembersError struct {
	Count int
}

func (e *RoleHasMembersError) Error() string {
	return fmt.Sprintf("role still has %d member(s)", e.Count)
}
