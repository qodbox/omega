package auth

// Passwords: at least 12 characters, and at most 72 bytes, the most bcrypt
// reads (a longer one would be silently truncated, so it is refused instead).

type Register struct {
	Name     string `json:"name" validate:"required,max=120"`
	Email    string `json:"email" validate:"required,email,max=255"`
	Password string `json:"password" validate:"required,min=12,maxbytes=72,notcommon"`
}

type Login struct {
	Email    string `json:"email" validate:"required,email"`
	Password string `json:"password" validate:"required"`
}

type Refresh struct {
	RefreshToken string `json:"refresh_token" validate:"required"`
}

type ChangePassword struct {
	CurrentPassword string `json:"current_password" validate:"required"`
	NewPassword     string `json:"new_password" validate:"required,min=12,maxbytes=72,notcommon,nefield=CurrentPassword"`
}
