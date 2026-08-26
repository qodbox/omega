package database

import (
	"fmt"
	"sync"

	"github.com/brianvoe/gofakeit/v7"
	"gorm.io/gorm"
)

var (
	fakerOnce sync.Once
	faker     *gofakeit.Faker
)

func Faker() *gofakeit.Faker {
	fakerOnce.Do(func() { faker = gofakeit.New(0) })
	return faker
}

func SeedFaker(seed uint64) {
	fakerOnce.Do(func() {})
	faker = gofakeit.New(seed)
}

type Factory[T any] struct {
	definition func(f *gofakeit.Faker) T
	states     []func(*T)
}

func NewFactory[T any](definition func(f *gofakeit.Faker) T) *Factory[T] {
	return &Factory[T]{definition: definition}
}

func (f *Factory[T]) State(mutate func(*T)) *Factory[T] {
	states := make([]func(*T), len(f.states), len(f.states)+1)
	copy(states, f.states)
	states = append(states, mutate)
	return &Factory[T]{definition: f.definition, states: states}
}

func (f *Factory[T]) Make() T {
	model := f.definition(Faker())
	for _, mutate := range f.states {
		mutate(&model)
	}
	return model
}

func (f *Factory[T]) MakeMany(n int) []T {
	models := make([]T, 0, n)
	for i := 0; i < n; i++ {
		models = append(models, f.Make())
	}
	return models
}

func (f *Factory[T]) Create(db *gorm.DB) (T, error) {
	model := f.Make()
	if err := db.Create(&model).Error; err != nil {
		return model, fmt.Errorf("factory: create %T: %w", model, err)
	}
	return model, nil
}

func (f *Factory[T]) CreateMany(db *gorm.DB, n int) ([]T, error) {
	models := f.MakeMany(n)
	if len(models) == 0 {
		return models, nil
	}
	if err := db.Create(&models).Error; err != nil {
		return models, fmt.Errorf("factory: create %d rows: %w", n, err)
	}
	return models, nil
}
