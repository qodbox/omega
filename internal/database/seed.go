package database

import (
	"fmt"
	"sort"
	"sync"

	"gorm.io/gorm"
)

type Seeder interface {
	Name() string
	Run(db *gorm.DB) error
}

type SeederFunc struct {
	SeederName string
	Fn         func(db *gorm.DB) error

	Order int
}

func (s SeederFunc) Name() string          { return s.SeederName }
func (s SeederFunc) Run(db *gorm.DB) error { return s.Fn(db) }

var (
	seedersMu sync.RWMutex
	seeders   []Seeder
	seedOrder = map[string]int{}
)

func RegisterSeeder(seeder Seeder, order ...int) {
	seedersMu.Lock()
	defer seedersMu.Unlock()

	position := 0
	if len(order) > 0 {
		position = order[0]
	}
	seedOrder[seeder.Name()] = position

	for i, existing := range seeders {
		if existing.Name() == seeder.Name() {
			seeders[i] = seeder
			return
		}
	}
	seeders = append(seeders, seeder)
}

func Seeders() []Seeder {
	seedersMu.RLock()
	defer seedersMu.RUnlock()

	ordered := make([]Seeder, len(seeders))
	copy(ordered, seeders)
	sort.SliceStable(ordered, func(i, j int) bool {
		return seedOrder[ordered[i].Name()] < seedOrder[ordered[j].Name()]
	})
	return ordered
}

func Seed(db *gorm.DB, names ...string) error {
	available := Seeders()

	if len(names) == 0 {
		for _, seeder := range available {
			if err := seeder.Run(db); err != nil {
				return fmt.Errorf("seeder %s: %w", seeder.Name(), err)
			}
		}
		return nil
	}

	byName := make(map[string]Seeder, len(available))
	for _, seeder := range available {
		byName[seeder.Name()] = seeder
	}

	for _, name := range names {
		seeder, ok := byName[name]
		if !ok {
			return fmt.Errorf("database: no seeder named %q", name)
		}
		if err := seeder.Run(db); err != nil {
			return fmt.Errorf("seeder %s: %w", name, err)
		}
	}
	return nil
}
