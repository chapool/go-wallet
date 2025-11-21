package auth

import (
	"time"

	"github/chapool/go-wallet/internal/data/dto"
)

type Result struct {
	Token      string
	User       *dto.User
	ValidUntil time.Time
	Scopes     []string
}
