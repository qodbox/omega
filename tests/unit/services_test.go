package unit

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rs/zerolog"

	"omega/internal/cache"
	"omega/internal/events"
	"omega/internal/policy"
	"omega/internal/scheduler"
	"omega/internal/storage"
)

var quiet = zerolog.Nop()

func TestCacheHonoursItsTTL(t *testing.T) {
	store := cache.New()
	defer store.Close()

	if err := store.Put("alive", 42, time.Minute); err != nil {
		t.Fatalf("put: %v", err)
	}

	var held int
	if !store.Get("alive", &held) || held != 42 {
		t.Errorf("get returned %d", held)
	}

	if err := store.Put("gone", 1, time.Nanosecond); err != nil {
		t.Fatalf("put: %v", err)
	}
	time.Sleep(2 * time.Millisecond)

	var expired int
	if store.Get("gone", &expired) {
		t.Error("an expired entry was served")
	}
	if store.Has("gone") {
		t.Error("Has reported an expired entry as present")
	}
}

func TestRememberBuildsOnceThenServesTheCache(t *testing.T) {
	store := cache.New()
	defer store.Close()

	builds := 0
	build := func() (string, error) {
		builds++
		return "computed", nil
	}

	for range 3 {
		value, err := cache.Remember(store, "key", time.Minute, build)
		if err != nil || value != "computed" {
			t.Fatalf("value = %q, err = %v", value, err)
		}
	}

	if builds != 1 {
		t.Errorf("the builder ran %d times, want 1", builds)
	}
}

func TestRememberDoesNotCacheAFailure(t *testing.T) {
	store := cache.New()
	defer store.Close()

	_, err := cache.Remember(store, "key", time.Minute, func() (int, error) {
		return 0, errors.New("nope")
	})
	if err == nil {
		t.Fatal("the error was swallowed")
	}
	if store.Has("key") {
		t.Error("a failed build was cached")
	}
}

func TestListenersRunInOrderAndCanAbort(t *testing.T) {
	bus := events.New(quiet)
	seen := []string{}

	bus.Listen("order.placed", "first", func(ctx context.Context, payload any) error {
		seen = append(seen, "first")
		return nil
	})
	bus.Listen("order.placed", "second", func(ctx context.Context, payload any) error {
		seen = append(seen, "second")
		return errors.New("stop here")
	})
	bus.Listen("order.placed", "third", func(ctx context.Context, payload any) error {
		seen = append(seen, "third")
		return nil
	})

	if err := bus.Emit(context.Background(), "order.placed", nil); err == nil {
		t.Fatal("the failing listener did not abort the chain")
	}
	if strings.Join(seen, ",") != "first,second" {
		t.Errorf("listeners ran: %v", seen)
	}
}

func TestAPanickingListenerIsContained(t *testing.T) {
	bus := events.New(quiet)
	bus.Listen("boom", "panics", func(ctx context.Context, payload any) error {
		panic("inside the listener")
	})

	if err := bus.Emit(context.Background(), "boom", nil); err == nil {
		t.Error("the panic was not turned into an error")
	}
}

func TestDeferredListenersAreWaitedFor(t *testing.T) {
	bus := events.New(quiet)

	var mu sync.Mutex
	done := false

	bus.ListenAsync("slow", "worker", func(ctx context.Context, payload any) error {
		time.Sleep(30 * time.Millisecond)
		mu.Lock()
		done = true
		mu.Unlock()
		return nil
	})

	if err := bus.Emit(context.Background(), "slow", nil); err != nil {
		t.Fatalf("emit: %v", err)
	}
	if !bus.Wait(time.Second) {
		t.Fatal("Wait timed out")
	}

	mu.Lock()
	defer mu.Unlock()
	if !done {
		t.Error("the deferred listener had not finished")
	}
}

func TestEmitIgnoresAnEventNobodyListensTo(t *testing.T) {
	bus := events.New(quiet)
	if err := bus.Emit(context.Background(), "nobody.cares", nil); err != nil {
		t.Errorf("emit: %v", err)
	}
}

func TestScheduledTasksAreListedWithTheirNextRun(t *testing.T) {
	plan := scheduler.New(quiet)
	plan.Every("often", time.Minute, func(ctx context.Context) error { return nil })
	plan.DailyAt("nightly", "03:00", func(ctx context.Context) error { return nil })

	tasks := plan.Tasks()
	if len(tasks) != 2 {
		t.Fatalf("tasks = %d, want 2", len(tasks))
	}

	joined := strings.Join(tasks, "\n")
	if !strings.Contains(joined, "often") || !strings.Contains(joined, "daily at 03:00") {
		t.Errorf("the listing is missing something:\n%s", joined)
	}
}

func TestSchedulerStopsWithItsContext(t *testing.T) {
	plan := scheduler.New(quiet)
	ctx, cancel := context.WithCancel(context.Background())

	finished := make(chan error, 1)
	go func() { finished <- plan.Run(ctx) }()

	cancel()
	select {
	case err := <-finished:
		if err != nil {
			t.Errorf("Run returned %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not stop when its context was cancelled")
	}
}

func TestStorageRefusesToEscapeItsRoot(t *testing.T) {
	disk, err := storage.New(filepath.Join(t.TempDir(), "files"), "/storage")
	if err != nil {
		t.Fatalf("new: %v", err)
	}

	if err := disk.PutBytes("invoices/42.pdf", []byte("content")); err != nil {
		t.Fatalf("put: %v", err)
	}

	content, err := disk.Get("invoices/42.pdf")
	if err != nil || string(content) != "content" {
		t.Errorf("get returned %q, %v", content, err)
	}
	if !disk.Exists("invoices/42.pdf") {
		t.Error("Exists says no")
	}
	if url := disk.URL("invoices/42.pdf"); url != "/storage/invoices/42.pdf" {
		t.Errorf("url = %q", url)
	}

	for _, escape := range []string{"../../etc/passwd", "/absolute/secret", "a/../../../b"} {
		if err := disk.PutBytes(escape, []byte("x")); err != nil {
			t.Fatalf("put %q: %v", escape, err)
		}
	}

	var strayed []string
	_ = filepath.Walk(disk.Root(), func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		if !strings.HasPrefix(path, disk.Root()+string(os.PathSeparator)) {
			strayed = append(strayed, path)
		}
		return nil
	})
	if len(strayed) > 0 {
		t.Errorf("files landed outside the root: %v", strayed)
	}

	if _, err := os.Stat("/absolute/secret"); err == nil {
		t.Error("an absolute path escaped to the real filesystem")
	}
}

func TestStorageDeletes(t *testing.T) {
	disk, _ := storage.New(filepath.Join(t.TempDir(), "files"), "/storage")
	_ = disk.PutBytes("note.txt", []byte("hello"))

	if err := disk.Delete("note.txt"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if disk.Exists("note.txt") {
		t.Error("the file survived its deletion")
	}
	if _, err := disk.Size("note.txt"); !os.IsNotExist(errors.Unwrap(err)) && err == nil {
		t.Error("Size answered for a missing file")
	}
}

type actor struct {
	ID   uint
	Role string
}

func TestPolicyFallsBackToRoleGrants(t *testing.T) {
	gate := policy.New()
	gate.Grant("editor", "posts.list", "posts.view")
	gate.Grant("admin", "*")

	ctx := context.Background()
	editor := &actor{ID: 1, Role: "editor"}
	admin := &actor{ID: 2, Role: "admin"}

	if !gate.Allows(ctx, editor, "posts.list") {
		t.Error("editor was denied a granted ability")
	}
	if gate.Allows(ctx, editor, "posts.delete") {
		t.Error("editor was allowed an ability nobody granted")
	}
	if !gate.Allows(ctx, admin, "anything.at.all") {
		t.Error("the wildcard did not apply")
	}
	if gate.Allows(ctx, nil, "posts.list") {
		t.Error("a nil actor was allowed")
	}
}

func TestPolicyWildcardCoversAResource(t *testing.T) {
	gate := policy.New()
	gate.Grant("editor", "posts.*")

	if !gate.Allows(context.Background(), &actor{Role: "editor"}, "posts.delete") {
		t.Error("posts.* did not cover posts.delete")
	}
	if gate.Allows(context.Background(), &actor{Role: "editor"}, "users.delete") {
		t.Error("posts.* leaked onto another resource")
	}
}

func TestPolicyRuleBeatsTheRoleGrant(t *testing.T) {
	gate := policy.New()
	gate.Grant("member", "profile.update")
	gate.Define("profile.update", func(ctx context.Context, who, subject any) bool {
		owner, ok := who.(*actor)
		target, alsoOk := subject.(*actor)
		return ok && alsoOk && owner.ID == target.ID
	})

	ctx := context.Background()
	mine := &actor{ID: 7, Role: "member"}
	theirs := &actor{ID: 9, Role: "member"}

	if !gate.Allows(ctx, mine, "profile.update", mine) {
		t.Error("the owner was denied their own record")
	}
	if gate.Allows(ctx, mine, "profile.update", theirs) {
		t.Error("the rule let one member edit another")
	}
}

func TestPolicyBeforeHookShortCircuits(t *testing.T) {
	gate := policy.New()
	gate.Before(func(ctx context.Context, who any, ability string) (bool, bool) {
		person, ok := who.(*actor)
		if ok && person.Role == "root" {
			return true, true
		}
		return false, false
	})
	gate.Define("posts.delete", func(context.Context, any, any) bool { return false })

	if !gate.Allows(context.Background(), &actor{Role: "root"}, "posts.delete") {
		t.Error("the before hook did not short-circuit the rule")
	}
	if gate.Allows(context.Background(), &actor{Role: "member"}, "posts.delete") {
		t.Error("the rule was not consulted for a normal actor")
	}
}
