// Package events names the api service's event types and their payloads.
package events

// UserRegistered is published when a user registers.
const UserRegistered = "user.registered"

// UserRegisteredPayload identifies the new user; consumers load the rest.
type UserRegisteredPayload struct {
	UserID int64 `json:"user_id"`
}

// UserVerificationRequested is published when a user asks for the verification email again.
const UserVerificationRequested = "user.verification_requested"

// UserVerificationRequestedPayload identifies the user to email.
type UserVerificationRequestedPayload struct {
	UserID int64 `json:"user_id"`
}

// PasswordResetRequested is published when a user asks to reset a forgotten password.
const PasswordResetRequested = "password.reset_requested"

// PasswordResetRequestedPayload identifies the user to email.
type PasswordResetRequestedPayload struct {
	UserID int64 `json:"user_id"`
}
