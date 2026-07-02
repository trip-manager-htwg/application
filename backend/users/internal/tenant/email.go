package tenant

import "github.com/trip-manager-htwg/application/backend/shared/email"

type EmailService = email.Service

func NewEmailService(apiKey string) *EmailService {
	return email.NewService(apiKey)
}
