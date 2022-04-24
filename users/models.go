package users

import (
	"time"

	"gorm.io/gorm"
)

//GORM-compatible models
type (
	UserRole int

	User struct {
		ID           uint `gorm:"primaryKey"`
		Name         string
		LastName     string
		Email        string
		Login        string `gorm:"unique"`
		PasswordHash string
		Role         UserRole
		CreatedAt    time.Time
		UpdatedAt    time.Time
		DeletedAt    gorm.DeletedAt
	}

	Client struct {
		ID             uint   `gorm:"primaryKey"`
		Token          string `gorm:"unique"`
		Label          string
		RestrictIp     string
		UserId         uint
		TokenIssuedAt  time.Time
		TokenExpiresAt time.Time
		FirstConnectAt time.Time
		LastConnectAt  time.Time
		CreatedAt      time.Time
		UpdatedAt      time.Time
		DeletedAt      gorm.DeletedAt
	}
)

const (
	roles_beg UserRole = iota
	UserRoleBanned
	UserRoleRegular
	UserRoleAdmin
	UserRoleSuper
	roles_end
)

func (r UserRole) Int() int {
	return [...]int{0, 1, 2, 3, 4, 5}[r]
}

func (r UserRole) String() string {
	return [...]string{"Banned", "User", "Admin", "Super admin"}[r]
}

func (r UserRole) CheckRole() bool {
	if roles_beg < r && r < roles_end {
		return true
	}
	return false
}

func (r *UserRole) AssignRole(role int) bool {
	new := UserRole(role)
	if roles_beg < new && new < roles_end {
		*r = new
		return true
	}
	return false
}
