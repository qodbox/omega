package seeds

import (
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"omega/app/models"
	"omega/database/factories"
	"omega/internal/database"
)

func init() {
	database.RegisterSeeder(database.SeederFunc{
		SeederName: "UserSeed",
		Fn:         seedUsers,
	}, 10)
}

func seedUsers(db *gorm.DB) error {
	admin := models.User{
		Name:  "Admin Omega",
		Email: "admin@omega.test",
		Role:  "admin",
	}
	if err := admin.SetPassword("password"); err != nil {
		return err
	}

	if err := db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "email"}},
		DoNothing: true,
	}).Create(&admin).Error; err != nil {
		return err
	}

	var count int64
	if err := db.Model(&models.User{}).Count(&count).Error; err != nil {
		return err
	}
	if count >= 25 {
		return nil
	}

	_, err := factories.User.CreateMany(db, 25-int(count))
	return err
}
