package repositories

import (
	"itii-assist/config"
	"itii-assist/utils"
)

func ValidateSessionPrincipal(claims *utils.TokenClaims) error {
	return config.ValidateSessionPrincipal(claims)
}
