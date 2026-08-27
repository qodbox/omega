package currency

// Choose sets the currency a caller reads amounts in. It is a display
// preference: it changes nothing about what is stored, and nothing about what
// a card is charged.
type Choose struct {
	Currency string `json:"currency" validate:"required,len=3,alpha"`
}
