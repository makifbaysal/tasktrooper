package domain

// ComponentArea is the role area that owns a component's code: the area a
// developer role assignment is scoped by. A desktop app is built by the
// frontend developer; everything without an area of its own falls to backend.
func ComponentArea(role ComponentRole) string {
	switch role {
	case ComponentRoleFrontend, ComponentRoleDesktop:
		return RepoKindFrontend
	case ComponentRoleMobile:
		return RepoKindMobile
	case ComponentRoleData:
		return RepoKindData
	case ComponentRoleGame:
		return RepoKindGame
	default:
		return RepoKindBackend
	}
}

// RoleKeyDeveloper is the role every coding agent holds, scoped by area.
const RoleKeyDeveloper = "developer"

// RoleKeyRelease is the release engineer's role.
const RoleKeyRelease = "release"
