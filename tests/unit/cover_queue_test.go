package unit

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/rs/zerolog"
	"gorm.io/gorm"

	"omega/app/models"
	"omega/internal/queue"
)

var aqsCovQueueBoom = errors.New("aqs: panne de la file")

type aqsCovQueueStore struct {
	mu        sync.Mutex
	pending   []*queue.Entry
	claimErr  error
	onClaim   func()
	reclaim   int64
	onReclaim func()
	failed    []string
	released  []time.Time
	done      []uint
}

func (s *aqsCovQueueStore) Push(context.Context, string, string, string, time.Time, int) error {
	return nil
}

func (s *aqsCovQueueStore) Claim(_ context.Context, _ []string, _ time.Time) (*queue.Entry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.onClaim != nil {
		s.onClaim()
	}
	if s.claimErr != nil {
		return nil, s.claimErr
	}
	if len(s.pending) == 0 {
		return nil, nil
	}
	entry := s.pending[0]
	s.pending = s.pending[1:]
	return entry, nil
}

func (s *aqsCovQueueStore) Release(_ context.Context, _ uint, runAt time.Time, _ string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.released = append(s.released, runAt)
	return nil
}

func (s *aqsCovQueueStore) Fail(_ context.Context, _ uint, failure string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.failed = append(s.failed, failure)
	return nil
}

func (s *aqsCovQueueStore) Complete(_ context.Context, id uint) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.done = append(s.done, id)
	return nil
}

func (s *aqsCovQueueStore) Failed(context.Context, int) ([]queue.Entry, error) { return nil, nil }
func (s *aqsCovQueueStore) Retry(context.Context, uint) (int64, error)         { return 0, nil }
func (s *aqsCovQueueStore) Purge(context.Context, string) (int64, error)       { return 0, nil }
func (s *aqsCovQueueStore) Pending(context.Context) (int64, error)             { return 0, nil }

func (s *aqsCovQueueStore) Reclaim(context.Context, time.Time) (int64, error) {
	if s.onReclaim != nil {
		s.onReclaim()
	}
	return s.reclaim, nil
}

func (s *aqsCovQueueStore) Sweep(context.Context, string, time.Time) (int64, error) { return 0, nil }

func (s *aqsCovQueueStore) snapshot() ([]string, []time.Time, []uint) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.failed...), append([]time.Time(nil), s.released...), append([]uint(nil), s.done...)
}

func TestAqsCovPushRefusesWithoutStore(t *testing.T) {
	q := queue.New(nil)
	q.Handle("aqs.cover", func(context.Context, []byte) error { return nil })

	if err := q.Push(context.Background(), "aqs.cover", nil); !errors.Is(err, queue.ErrNoStore) {
		t.Fatalf("Push sans store = %v, want ErrNoStore", err)
	}
	if q.Store() != nil {
		t.Fatal("Store() = non nil, want nil")
	}
}

func TestAqsCovPushRefusesAnUnserialisablePayload(t *testing.T) {
	q := queue.New(&aqsCovQueueStore{})
	q.Handle("aqs.cover", func(context.Context, []byte) error { return nil })

	if err := q.Push(context.Background(), "aqs.cover", make(chan int)); err == nil {
		t.Fatal("Push = nil pour une charge non serialisable, want une erreur")
	}
}

func TestAqsCovPushHonoursTheExplicitQueueName(t *testing.T) {
	db := aqsCovJobsDB(t)
	q := queue.New(queue.NewStore(db))
	q.Handle("aqs.cover", func(context.Context, []byte) error { return nil })

	err := q.Push(context.Background(), "aqs.cover", map[string]int{"n": 1},
		queue.Options{Queue: "mails", MaxTries: 9, Delay: time.Minute})
	if err != nil {
		t.Fatalf("Push: %v", err)
	}

	var job models.Job
	if err := db.First(&job).Error; err != nil {
		t.Fatalf("relecture: %v", err)
	}
	if job.Queue != "mails" || job.MaxTries != 9 {
		t.Fatalf("job = (%s, %d), want (mails, 9)", job.Queue, job.MaxTries)
	}
	if !job.RunAt.After(time.Now().Add(30 * time.Second)) {
		t.Fatalf("run_at = %v, want un delai d'environ une minute", job.RunAt)
	}
}

func TestAqsCovWorkRefusesWithoutStore(t *testing.T) {
	if err := queue.New(nil).Work(context.Background(), queue.WorkerOptions{}); !errors.Is(err, queue.ErrNoStore) {
		t.Fatalf("Work sans store = %v, want ErrNoStore", err)
	}
}

func TestAqsCovWorkAppliesItsDefaultsThenStopsOnAClosedContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := queue.New(&aqsCovQueueStore{}).Work(ctx, queue.WorkerOptions{}); err != nil {
		t.Fatalf("Work sur un contexte clos = %v, want nil", err)
	}
}

func TestAqsCovWorkStopsWhenTheContextDiesDuringTheClaimBackoff(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	store := &aqsCovQueueStore{claimErr: aqsCovQueueBoom, onClaim: cancel}

	q := queue.New(store)
	if err := q.Work(ctx, queue.WorkerOptions{Poll: time.Minute, Log: zerolog.Nop()}); err != nil {
		t.Fatalf("Work = %v, want nil", err)
	}
}

func TestAqsCovWorkFailsAJobWithoutHandler(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	store := &aqsCovQueueStore{pending: []*queue.Entry{{ID: 1, Name: "aqs.inconnu", Queue: "default", MaxTries: 3}}}

	var once sync.Once
	store.onClaim = func() {
		if len(store.pending) == 0 {
			once.Do(func() { time.AfterFunc(30*time.Millisecond, cancel) })
		}
	}

	q := queue.New(store)
	if err := q.Work(ctx, queue.WorkerOptions{Poll: time.Millisecond, Log: zerolog.Nop()}); err != nil {
		t.Fatalf("Work = %v, want nil", err)
	}

	failed, _, _ := store.snapshot()
	if len(failed) != 1 || failed[0] != queue.ErrUnknownJob.Error() {
		t.Fatalf("echecs = %v, want une entree ErrUnknownJob", failed)
	}
}

func TestAqsCovWorkCapsTheRetryBackoffAtTenMinutes(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	store := &aqsCovQueueStore{
		pending: []*queue.Entry{{ID: 1, Name: "aqs.cover", Queue: "default", Attempts: 12, MaxTries: 40}},
	}

	var once sync.Once
	store.onClaim = func() {
		if len(store.pending) == 0 {
			once.Do(func() { time.AfterFunc(30*time.Millisecond, cancel) })
		}
	}

	q := queue.New(store)
	q.Handle("aqs.cover", func(context.Context, []byte) error { panic("aqs: le job explose") })

	if err := q.Work(ctx, queue.WorkerOptions{Poll: time.Millisecond, Log: zerolog.Nop()}); err != nil {
		t.Fatalf("Work = %v, want nil", err)
	}

	_, released, _ := store.snapshot()
	if len(released) != 1 {
		t.Fatalf("reprogrammations = %d, want 1", len(released))
	}
	wait := time.Until(released[0])
	if wait < 9*time.Minute || wait > 10*time.Minute {
		t.Fatalf("attente = %v, want environ 10 minutes", wait)
	}
}

func TestAqsCovWorkReclaimsStuckJobs(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	seen := make(chan struct{}, 4)
	store := &aqsCovQueueStore{reclaim: 2, onReclaim: func() {
		select {
		case seen <- struct{}{}:
		default:
		}
	}}

	stopped := make(chan error, 1)
	go func() {
		stopped <- queue.New(store).Work(ctx, queue.WorkerOptions{
			Poll: time.Millisecond, StuckAfter: 10 * time.Millisecond, Log: zerolog.Nop(),
		})
	}()

	select {
	case <-seen:
	case <-time.After(3 * time.Second):
		t.Fatal("le reclaim periodique ne s'est jamais declenche")
	}

	cancel()
	if err := <-stopped; err != nil {
		t.Fatalf("Work = %v, want nil", err)
	}
}

func aqsCovJobsDB(t *testing.T) *gorm.DB {
	t.Helper()

	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "jobs.db")), &gorm.Config{})
	if err != nil {
		t.Fatalf("ouverture: %v", err)
	}
	if err := db.AutoMigrate(&models.Job{}); err != nil {
		t.Fatalf("migration: %v", err)
	}
	return db
}

func TestAqsCovClaimSurfacesAnUpdateFailure(t *testing.T) {
	db := aqsCovJobsDB(t)
	store := queue.NewStore(db)
	ctx := context.Background()

	if err := store.Push(ctx, "aqs.cover", "default", "{}", time.Now().Add(-time.Minute), 3); err != nil {
		t.Fatalf("Push: %v", err)
	}
	if err := db.Exec(`CREATE TRIGGER aqs_cov_bloque BEFORE UPDATE ON jobs BEGIN SELECT RAISE(ABORT, 'aqs'); END;`).Error; err != nil {
		t.Fatalf("trigger: %v", err)
	}

	entry, err := store.Claim(ctx, []string{"default"}, time.Now())
	if err == nil {
		t.Fatalf("Claim = (%v, nil), want une erreur de mise a jour", entry)
	}
}

func TestAqsCovFailedSurfacesAReadFailure(t *testing.T) {
	db := aqsCovJobsDB(t)
	store := queue.NewStore(db)

	if err := db.Migrator().DropTable(&models.Job{}); err != nil {
		t.Fatalf("suppression de la table: %v", err)
	}

	if _, err := store.Failed(context.Background(), 10); err == nil {
		t.Fatal("Failed = nil alors que la table a disparu, want une erreur")
	}
}

func TestAqsCovRetryTargetsASingleJob(t *testing.T) {
	db := aqsCovJobsDB(t)
	store := queue.NewStore(db)
	ctx := context.Background()

	jobs := []models.Job{
		{Queue: "default", Name: "a", Status: models.JobFailed, RunAt: time.Now()},
		{Queue: "default", Name: "b", Status: models.JobFailed, RunAt: time.Now()},
	}
	if err := db.Create(&jobs).Error; err != nil {
		t.Fatalf("insertion: %v", err)
	}

	count, err := store.Retry(ctx, jobs[0].ID)
	if err != nil {
		t.Fatalf("Retry: %v", err)
	}
	if count != 1 {
		t.Fatalf("Retry(id) = %d, want 1", count)
	}

	failed, err := store.Failed(ctx, 10)
	if err != nil {
		t.Fatalf("Failed: %v", err)
	}
	if len(failed) != 1 || failed[0].Name != "b" {
		t.Fatalf("restants = %v, want le seul job b", failed)
	}
}
