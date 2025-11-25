package auth

// UserRole represents the role of a user
type UserRole string

const (
	// RoleAdmin represents an admin user
	RoleAdmin UserRole = "admin"
	// RoleUser represents a regular user
	RoleUser UserRole = "user"
)

// IsAdmin checks if the role is admin
func (r UserRole) IsAdmin() bool {
	return r == RoleAdmin
}

// IsUser checks if the role is user
func (r UserRole) IsUser() bool {
	return r == RoleUser
}

// String returns the string representation of the role
func (r UserRole) String() string {
	return string(r)
}
