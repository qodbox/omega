package unit

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"omega/internal/storage"
)

func aqsCovDisk(t *testing.T) *storage.Disk {
	t.Helper()

	disk, err := storage.New(t.TempDir(), "https://cdn.test")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return disk
}

func TestAqsCovNewRefusesARootBuriedUnderAFile(t *testing.T) {
	base := t.TempDir()
	blocker := filepath.Join(base, "fichier")
	if err := os.WriteFile(blocker, []byte("x"), 0o644); err != nil {
		t.Fatalf("ecriture: %v", err)
	}

	if _, err := storage.New(filepath.Join(blocker, "racine"), ""); err == nil {
		t.Fatal("New = nil pour une racine sous un fichier, want une erreur")
	}
}

func TestAqsCovARootAtTheFilesystemRootRejectsEveryName(t *testing.T) {
	disk, err := storage.New(string(os.PathSeparator), "")
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if _, err := disk.Get("etc/hostname"); !errors.Is(err, storage.ErrOutsideRoot) {
		t.Fatalf("Get = %v, want ErrOutsideRoot", err)
	}
	if disk.Exists("etc") {
		t.Fatal("Exists = true, want false")
	}
}

func TestAqsCovResolveFailsWhenTheRootDisappears(t *testing.T) {
	base := filepath.Join(t.TempDir(), "racine")

	disk, err := storage.New(base, "")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := os.RemoveAll(base); err != nil {
		t.Fatalf("suppression: %v", err)
	}

	if _, err := disk.Get("note.txt"); err == nil {
		t.Fatal("Get = nil alors que la racine a disparu, want une erreur")
	}
}

func TestAqsCovResolveSurfacesANonDirectoryAncestor(t *testing.T) {
	disk := aqsCovDisk(t)
	if err := disk.PutBytes("fichier", []byte("x")); err != nil {
		t.Fatalf("PutBytes: %v", err)
	}

	_, err := disk.Get("fichier/dedans.txt")
	if err == nil {
		t.Fatal("Get = nil pour un ancetre non repertoire, want une erreur")
	}
	if errors.Is(err, storage.ErrOutsideRoot) {
		t.Fatalf("Get = %v, want l'erreur systeme et non ErrOutsideRoot", err)
	}

	if disk.Exists("fichier/dedans.txt") {
		t.Fatal("Exists = true pour un ancetre non repertoire, want false")
	}
	if err := disk.Delete("fichier/dedans.txt"); err == nil {
		t.Fatal("Delete = nil pour un ancetre non repertoire, want une erreur")
	}
	if _, err := disk.Size("fichier/dedans.txt"); err == nil {
		t.Fatal("Size = nil pour un ancetre non repertoire, want une erreur")
	}
	if _, err := disk.Put("fichier/dedans.txt", strings.NewReader("x")); err == nil {
		t.Fatal("Put = nil pour un ancetre non repertoire, want une erreur")
	}
	if err := disk.PutBytes("fichier/dedans.txt", []byte("x")); err == nil {
		t.Fatal("PutBytes = nil pour un ancetre non repertoire, want une erreur")
	}
}

func TestAqsCovResolveRefusesASymlinkEscape(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "racine")
	outside := filepath.Join(base, "dehors")

	if err := os.MkdirAll(outside, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(outside, "secret.txt"), []byte("vole-moi"), 0o644); err != nil {
		t.Fatalf("ecriture: %v", err)
	}

	disk, err := storage.New(root, "")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "evasion")); err != nil {
		t.Skipf("liens symboliques indisponibles: %v", err)
	}

	if _, err := disk.Get("evasion/secret.txt"); !errors.Is(err, storage.ErrOutsideRoot) {
		t.Fatalf("Get = %v, want ErrOutsideRoot", err)
	}
	if err := disk.PutBytes("evasion/injecte.txt", []byte("x")); !errors.Is(err, storage.ErrOutsideRoot) {
		t.Fatalf("PutBytes = %v, want ErrOutsideRoot", err)
	}
	if _, err := disk.Put("evasion/injecte.txt", strings.NewReader("x")); !errors.Is(err, storage.ErrOutsideRoot) {
		t.Fatalf("Put = %v, want ErrOutsideRoot", err)
	}
	if _, err := disk.Size("evasion/secret.txt"); !errors.Is(err, storage.ErrOutsideRoot) {
		t.Fatalf("Size = %v, want ErrOutsideRoot", err)
	}
	if err := disk.Delete("evasion/secret.txt"); !errors.Is(err, storage.ErrOutsideRoot) {
		t.Fatalf("Delete = %v, want ErrOutsideRoot", err)
	}
	if disk.Exists("evasion/secret.txt") {
		t.Fatal("Exists = true a travers un lien qui sort de la racine, want false")
	}
}

func TestAqsCovDottedNamesStayInsideTheRoot(t *testing.T) {
	disk := aqsCovDisk(t)

	if err := disk.PutBytes("../../evade.txt", []byte("x")); err != nil {
		t.Fatalf("PutBytes: %v", err)
	}
	if _, err := os.Stat(filepath.Join(disk.Root(), "evade.txt")); err != nil {
		t.Fatalf("le fichier n'a pas ete ramene dans la racine: %v", err)
	}
}

func TestAqsCovWritesFailOnALockedDirectory(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignore les permissions de repertoire")
	}

	disk := aqsCovDisk(t)
	locked := filepath.Join(disk.Root(), "verrouille")
	if err := os.Mkdir(locked, 0o555); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(locked, 0o755) })

	if _, err := disk.Put("verrouille/sous/f.txt", strings.NewReader("x")); err == nil {
		t.Fatal("Put = nil dans un repertoire verrouille, want une erreur")
	}
	if err := disk.PutBytes("verrouille/sous/f.txt", []byte("x")); err == nil {
		t.Fatal("PutBytes = nil dans un repertoire verrouille, want une erreur")
	}
}

func TestAqsCovPutRefusesToOverwriteADirectory(t *testing.T) {
	disk := aqsCovDisk(t)
	if err := os.Mkdir(filepath.Join(disk.Root(), "dossier"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	if _, err := disk.Put("dossier", bytes.NewReader([]byte("x"))); err == nil {
		t.Fatal("Put = nil sur un repertoire existant, want une erreur")
	}
}

func TestAqsCovSizeReportsTheWrittenLength(t *testing.T) {
	disk := aqsCovDisk(t)

	written, err := disk.Put("sous/note.txt", strings.NewReader("douze octets"))
	if err != nil {
		t.Fatalf("Put: %v", err)
	}
	if written != 12 {
		t.Fatalf("Put = %d octets, want 12", written)
	}

	size, err := disk.Size("sous/note.txt")
	if err != nil {
		t.Fatalf("Size: %v", err)
	}
	if size != 12 {
		t.Fatalf("Size = %d, want 12", size)
	}
	if got := disk.URL("sous/note.txt"); got != "https://cdn.test/sous/note.txt" {
		t.Fatalf("URL = %q, want https://cdn.test/sous/note.txt", got)
	}
	if _, err := disk.Size("absent.txt"); err == nil {
		t.Fatal("Size = nil pour un fichier absent, want une erreur")
	}
}
