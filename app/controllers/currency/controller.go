package currency

import (
	"github.com/gofiber/fiber/v2"
	"gorm.io/gorm"

	"omega/app/models"
	presenter "omega/app/presenters/currency"
	request "omega/app/requests/currency"
	jwt "omega/internal/auth"
	"omega/internal/currency"
	"omega/internal/validation"
)

type Controller struct {
	db       *gorm.DB
	exchange *currency.Exchange
	guard    *jwt.Guard
}

func NewController(db *gorm.DB, exchange *currency.Exchange, guard *jwt.Guard) *Controller {
	return &Controller{db: db, exchange: exchange, guard: guard}
}

// Catalogue answers with what may be chosen and the day's rates. It is public:
// a price list is shown before anyone signs in, and the rates are a central
// bank's, not ours to keep.
func (currencyc *Controller) Catalogue(c *fiber.Ctx) error {
	return c.JSON(fiber.Map{"data": presenter.NewCatalogue(c.UserContext(), currencyc.exchange)})
}

// Choose records the currency the caller reads amounts in.
//
// Whatever arrives is narrowed to the catalogue rather than refused outright
// only when it is malformed: a code nobody quotes converts nothing, so storing
// it would leave the account reading figures that never change.
func (currencyc *Controller) Choose(c *fiber.Ctx) error {
	body, err := validation.Bind[request.Choose](c)
	if err != nil {
		return err
	}

	user, err := currencyc.caller(c)
	if err != nil {
		return err
	}

	if !currencyc.exchange.Supports(body.Currency) {
		return validation.Failed("currency", "This currency is not one of those on offer.")
	}

	chosen := currencyc.exchange.Normalise(body.Currency)
	if err := currencyc.db.WithContext(c.UserContext()).
		Model(&models.User{}).
		Where("id = ?", user.ID).
		Update("currency", chosen).Error; err != nil {
		return err
	}
	user.Currency = chosen

	rate, _ := currencyc.exchange.Rate(c.UserContext(), chosen)

	return c.JSON(fiber.Map{"data": presenter.Choice{
		Currency: chosen,
		Base:     currencyc.exchange.Base(),
		Rate:     rate,
	}})
}

func (currencyc *Controller) caller(c *fiber.Ctx) (*models.User, error) {
	user, ok := currencyc.guard.User(c).(*models.User)
	if !ok {
		return nil, fiber.NewError(fiber.StatusUnauthorized, "Authentication required.")
	}
	return user, nil
}
