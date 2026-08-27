package billing

// Checkout asks for a hosted checkout page. The caller names a plan, never a
// price: price ids stay on the server, so a crafted body cannot subscribe
// someone to a price that is not on sale.
type Checkout struct {
	Plan string `json:"plan" validate:"required,max=64"`
}

// Cancel stops a subscription. By default it runs to the end of the period the
// customer has already paid for.
type Cancel struct {
	Immediately bool `json:"immediately"`
}
