package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"omega"
)

func TestToPascalNormalisesEverySeparator(t *testing.T) {
	cases := map[string]string{
		"user":            "User",
		"user_profile":    "UserProfile",
		"user-profile":    "UserProfile",
		"user profile":    "UserProfile",
		"user.profile":    "UserProfile",
		"UserProfile":     "UserProfile",
		"order_line_item": "OrderLineItem",
		"":                "",
	}

	for raw, want := range cases {
		if got := toPascal(raw); got != want {
			t.Errorf("toPascal(%q) = %q, attendu %q", raw, got, want)
		}
	}
}

func TestToSnakeSplitsAcronymsAndCase(t *testing.T) {
	cases := map[string]string{
		"User":         "user",
		"UserProfile":  "user_profile",
		"APIToken":     "api_token",
		"OAuthClient":  "o_auth_client",
		"user-profile": "user_profile",
		"user profile": "user_profile",
		"user.profile": "user_profile",
		"":             "",
	}

	for raw, want := range cases {
		if got := toSnake(raw); got != want {
			t.Errorf("toSnake(%q) = %q, attendu %q", raw, got, want)
		}
	}
}

func TestCaseHelpersLeaveEmptyAlone(t *testing.T) {
	if upperFirst("") != "" {
		t.Error("upperFirst(\"\") doit rendre une chaine vide")
	}
	if lowerFirst("") != "" {
		t.Error("lowerFirst(\"\") doit rendre une chaine vide")
	}
	if got := upperFirst("état"); got != "État" {
		t.Errorf("upperFirst sur un accent = %q", got)
	}
	if got := lowerFirst("État"); got != "état" {
		t.Errorf("lowerFirst sur un accent = %q", got)
	}
}

func TestParseNamesFillsEveryField(t *testing.T) {
	n := parseNames("order_line")

	if n.Name != "OrderLine" {
		t.Errorf("Name = %q", n.Name)
	}
	if n.Var != "orderLine" {
		t.Errorf("Var = %q", n.Var)
	}
	if n.Snake != "order_line" {
		t.Errorf("Snake = %q", n.Snake)
	}
	if n.Plural != "order_lines" {
		t.Errorf("Plural = %q", n.Plural)
	}
	if n.Title != "Order line" {
		t.Errorf("Title = %q", n.Title)
	}
	if len(n.Timestamp) != 14 {
		t.Errorf("Timestamp = %q, attendu 14 chiffres", n.Timestamp)
	}
}

func TestParseNamesPluralisesIrregulars(t *testing.T) {
	cases := map[string]string{"person": "people", "category": "categories", "box": "boxes"}

	for raw, want := range cases {
		if got := parseNames(raw).Plural; got != want {
			t.Errorf("pluriel de %q = %q, attendu %q", raw, got, want)
		}
	}
}

func TestMigrationPathCarriesTheTimestamp(t *testing.T) {
	n := parseNames("user")
	got := migrationPath(n, "create_users_table")
	want := filepath.Join("database", "migrations", n.Timestamp+"_create_users_table.go")

	if got != want {
		t.Errorf("migrationPath = %q, attendu %q", got, want)
	}
}

func TestGenerateWritesFormattedGo(t *testing.T) {
	dir := t.TempDir()
	dest := filepath.Join(dir, "app", "models", "user.go")

	if err := generate("model", dest, parseNames("user"), false); err != nil {
		t.Fatalf("generate: %v", err)
	}

	raw, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("lecture: %v", err)
	}
	if !strings.HasPrefix(string(raw), "package models") {
		t.Errorf("le paquet doit venir du dossier de destination, obtenu %.40q", raw)
	}
}

func TestGenerateRefusesToOverwriteWithoutForce(t *testing.T) {
	dir := t.TempDir()
	dest := filepath.Join(dir, "app", "models", "user.go")

	if err := generate("model", dest, parseNames("user"), false); err != nil {
		t.Fatalf("premiere generation: %v", err)
	}

	err := generate("model", dest, parseNames("user"), false)
	if err == nil {
		t.Fatal("une seconde generation sans --force doit echouer")
	}
	if !strings.Contains(err.Error(), "already exists") {
		t.Errorf("message inattendu: %v", err)
	}

	if err := generate("model", dest, parseNames("user"), true); err != nil {
		t.Fatalf("--force doit ecraser: %v", err)
	}
}

func TestGenerateRejectsUnknownStub(t *testing.T) {
	err := generate("il-n-existe-pas", filepath.Join(t.TempDir(), "x.go"), parseNames("user"), false)

	if err == nil {
		t.Fatal("un stub inconnu doit echouer")
	}
	if !strings.Contains(err.Error(), "unknown stub") {
		t.Errorf("message inattendu: %v", err)
	}
}

func TestManagerNamesCoversEveryManager(t *testing.T) {
	got := managerNames()

	for _, want := range []string{"bun", "pnpm", "yarn", "npm"} {
		if !contains(got, want) {
			t.Errorf("%s absent de %v", want, got)
		}
	}
	if len(got) != len(managers) {
		t.Errorf("managerNames rend %d noms pour %d managers", len(got), len(managers))
	}
}

func contains(haystack []string, needle string) bool {
	for _, item := range haystack {
		if item == needle {
			return true
		}
	}
	return false
}

func TestByNameFindsEachManager(t *testing.T) {
	for _, name := range managerNames() {
		found, err := byName(name)
		if err != nil {
			t.Errorf("byName(%q): %v", name, err)
			continue
		}
		if found.name != name {
			t.Errorf("byName(%q) rend %q", name, found.name)
		}
		if len(found.install) == 0 || len(found.run) == 0 || len(found.dlx) == 0 {
			t.Errorf("%s: commandes incompletes", name)
		}
	}
}

func TestByNameListsTheChoicesWhenWrong(t *testing.T) {
	_, err := byName("cargo")

	if err == nil {
		t.Fatal("un gestionnaire inconnu doit echouer")
	}
	for _, name := range managerNames() {
		if !strings.Contains(err.Error(), name) {
			t.Errorf("le message doit citer %s, obtenu: %v", name, err)
		}
	}
}

func TestAdjustOnlyTouchesYarn(t *testing.T) {
	for _, candidate := range managers {
		if candidate.name == "yarn" {
			continue
		}
		if got := adjust(candidate); got.dlx[0] != candidate.dlx[0] {
			t.Errorf("adjust a modifie %s: %v", candidate.name, got.dlx)
		}
	}
}

func TestAdjustFallsBackToNpxOnYarnClassic(t *testing.T) {
	yarn, err := byName("yarn")
	if err != nil {
		t.Fatal(err)
	}

	got := adjust(yarn)
	if got.dlx[0] != "npx" && got.dlx[0] != "yarn" {
		t.Errorf("dlx inattendu apres adjust: %v", got.dlx)
	}
	if !installed(yarn) && got.dlx[0] != "npx" {
		t.Errorf("yarn absent: npx attendu, obtenu %v", got.dlx)
	}
}

func TestPickManagerRejectsAnUnknownChoice(t *testing.T) {
	if _, err := pickManager("cargo", t.TempDir()); err == nil {
		t.Fatal("un gestionnaire inconnu doit echouer")
	}
}

func TestPickManagerFollowsTheLockfile(t *testing.T) {
	for _, candidate := range managers {
		if !installed(candidate) {
			continue
		}

		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, candidate.lock), []byte("{}"), 0o644); err != nil {
			t.Fatal(err)
		}

		got, err := pickManager("", dir)
		if err != nil {
			t.Fatalf("pickManager avec %s: %v", candidate.lock, err)
		}
		if got.name != candidate.name {
			t.Errorf("%s present mais %s choisi", candidate.lock, got.name)
		}
	}
}

func TestPickManagerAcceptsTheLegacyBunLock(t *testing.T) {
	if !installed(managers[0]) {
		t.Skip("bun absent")
	}

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "bun.lockb"), []byte{0}, 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := pickManager("", dir)
	if err != nil {
		t.Fatalf("pickManager: %v", err)
	}
	if got.name != "bun" {
		t.Errorf("bun.lockb present mais %s choisi", got.name)
	}
}

func TestPickManagerFallsBackToWhatIsInstalled(t *testing.T) {
	got, err := pickManager("", t.TempDir())

	if err != nil {
		t.Skipf("aucun gestionnaire installe: %v", err)
	}
	if !installed(got) {
		t.Errorf("pickManager a choisi %s qui n'est pas installe", got.name)
	}
}

func TestPickManagerRefusesAManagerThatIsNotInstalled(t *testing.T) {
	for _, candidate := range managers {
		if installed(candidate) {
			continue
		}

		_, err := pickManager(candidate.name, t.TempDir())
		if err == nil {
			t.Errorf("%s absent mais accepte", candidate.name)
			continue
		}
		if !strings.Contains(err.Error(), "not installed") {
			t.Errorf("message inattendu pour %s: %v", candidate.name, err)
		}
	}
}

func TestExcludedDriverTagsNamesWhatIsDropped(t *testing.T) {
	cases := map[string]string{
		"":                      "",
		"sqlite":                "nopostgres,nomysql",
		"sqlite,postgres":       "nomysql",
		"sqlite,postgres,mysql": "",
		" SQLite , MySQL ":      "nopostgres",
		"postgres":              "nosqlite,nomysql",
	}

	for kept, want := range cases {
		var drivers []string
		if kept != "" {
			drivers = strings.Split(kept, ",")
		}
		if got := excludedDriverTags(drivers); got != want {
			t.Errorf("excludedDriverTags(%q) = %q, attendu %q", kept, got, want)
		}
	}
}

func TestExcludedDriverTagsIgnoresAnUnknownName(t *testing.T) {
	got := excludedDriverTags([]string{"sqlite", "oracle"})

	if got != "nopostgres,nomysql" {
		t.Errorf("un driver inconnu ne doit rien garder de plus, obtenu %q", got)
	}
}

func TestUnpackWritesEveryTemplateFile(t *testing.T) {
	for _, target := range stacks {
		dir := t.TempDir()

		written, err := unpack(target, dir, defaultAPI)
		if err != nil {
			t.Fatalf("unpack %s: %v", target.name, err)
		}
		if len(written) == 0 {
			t.Fatalf("%s: aucun fichier ecrit", target.name)
		}

		for _, path := range written {
			if _, err := os.Stat(path); err != nil {
				t.Errorf("%s: %s annonce mais absent", target.name, path)
			}
		}
		if _, err := os.Stat(filepath.Join(dir, "package.json")); err != nil {
			t.Errorf("%s: package.json manquant", target.name)
		}
	}
}

func TestUnpackSubstitutesTheRealPort(t *testing.T) {
	for _, target := range stacks {
		dir := t.TempDir()

		if _, err := unpack(target, dir, "http://127.0.0.1:3300"); err != nil {
			t.Fatalf("unpack %s: %v", target.name, err)
		}

		leftover := []string{}
		err := filepath.WalkDir(dir, func(path string, entry fs.DirEntry, err error) error {
			if err != nil || entry.IsDir() {
				return err
			}
			raw, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			if bytes.Contains(raw, []byte(defaultAPI)) {
				leftover = append(leftover, path)
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		if len(leftover) > 0 {
			t.Errorf("%s: port 3000 non substitue dans %v", target.name, leftover)
		}
	}
}

func TestUnpackBringsTheSharedFiles(t *testing.T) {
	for _, target := range stacks {
		if len(target.shared) == 0 {
			continue
		}

		dir := t.TempDir()
		if _, err := unpack(target, dir, defaultAPI); err != nil {
			t.Fatalf("unpack %s: %v", target.name, err)
		}

		for _, destination := range target.shared {
			if _, err := os.Stat(filepath.Join(dir, destination)); err != nil {
				t.Errorf("%s: fichier partage %s absent", target.name, destination)
			}
		}
	}
}

func TestApiURLFallsBackWhenNothingSaysOtherwise(t *testing.T) {
	dir := t.TempDir()
	previous, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(previous) })

	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}

	restore, had := os.LookupEnv("APP_PORT")
	if err := os.Unsetenv("APP_PORT"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if had {
			_ = os.Setenv("APP_PORT", restore)
		}
	})

	if got := apiURL(); got != defaultAPI {
		t.Errorf("sans config ni APP_PORT, apiURL = %q, attendu %q", got, defaultAPI)
	}
}

func TestApiURLFollowsTheProjectPort(t *testing.T) {
	insideProject(t)
	t.Setenv("APP_PORT", "3999")

	if got := apiURL(); got != "http://127.0.0.1:3999" {
		t.Errorf("apiURL = %q, attendu http://127.0.0.1:3999", got)
	}
}

func TestProjectRootRecognisesTheProject(t *testing.T) {
	root, ok := projectRoot()

	if !ok {
		t.Fatal("la suite tourne dans le projet, projectRoot doit le trouver")
	}
	for _, marker := range []string{filepath.Join("config", "app.yaml"), "go.mod"} {
		if _, err := os.Stat(filepath.Join(root, marker)); err != nil {
			t.Errorf("%s absent de la racine trouvee %s", marker, root)
		}
	}
}

func TestProjectRootStopsOutsideAProject(t *testing.T) {
	dir := t.TempDir()
	previous, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(previous) })

	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	if _, ok := projectRoot(); ok {
		t.Error("un dossier vide ne doit pas passer pour un projet")
	}
}

func TestCopyTreeLeavesBehindWhatMustNotTravel(t *testing.T) {
	source := t.TempDir()
	target := filepath.Join(t.TempDir(), "copie")

	for _, relative := range []string{
		"omega.go",
		filepath.Join("app", "models", "user.go"),
		filepath.Join("node_modules", "left-pad", "index.js"),
		filepath.Join("tmp", "build"),
		filepath.Join("bin", "omega"),
		".env",
		"app.db",
		"app.db-wal",
		"server.log",
		filepath.Join("web", "package.json"),
	} {
		full := filepath.Join(source, relative)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	if err := copyTree(source, target, false); err != nil {
		t.Fatalf("copyTree: %v", err)
	}

	for _, kept := range []string{"omega.go", filepath.Join("app", "models", "user.go")} {
		if _, err := os.Stat(filepath.Join(target, kept)); err != nil {
			t.Errorf("%s aurait du etre copie", kept)
		}
	}
	for _, dropped := range []string{
		filepath.Join("node_modules", "left-pad", "index.js"),
		filepath.Join("tmp", "build"),
		filepath.Join("bin", "omega"),
		".env",
		"app.db",
		"app.db-wal",
		"server.log",
		filepath.Join("web", "package.json"),
	} {
		if _, err := os.Stat(filepath.Join(target, dropped)); err == nil {
			t.Errorf("%s n'aurait pas du etre copie", dropped)
		}
	}
}

func TestCopyTreeKeepsWebWhenAsked(t *testing.T) {
	source := t.TempDir()
	target := filepath.Join(t.TempDir(), "copie")

	full := filepath.Join(source, "web", "package.json")
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := copyTree(source, target, true); err != nil {
		t.Fatalf("copyTree: %v", err)
	}
	if _, err := os.Stat(filepath.Join(target, "web", "package.json")); err != nil {
		t.Error("web demande mais absent de la copie")
	}
}

func TestRenameRewritesEveryImportForm(t *testing.T) {
	target := t.TempDir()

	files := map[string]string{
		"go.mod":   "module omega\n",
		"main.go":  "package main\n\nimport (\n\t\"omega/internal/kernel\"\n\t\"omega\"\n)\n",
		"a.stub":   "import \"omega/app/models\"\n",
		"keep.txt": "omega reste ici\n",
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(target, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	if err := rename(target, "acme/api"); err != nil {
		t.Fatalf("rename: %v", err)
	}

	got := map[string]string{}
	for name := range files {
		raw, err := os.ReadFile(filepath.Join(target, name))
		if err != nil {
			t.Fatal(err)
		}
		got[name] = string(raw)
	}

	if !strings.Contains(got["go.mod"], "module acme/api") {
		t.Errorf("go.mod = %q", got["go.mod"])
	}
	if !strings.Contains(got["main.go"], `"acme/api/internal/kernel"`) {
		t.Errorf("import prefixe non reecrit: %q", got["main.go"])
	}
	if !strings.Contains(got["main.go"], `"acme/api"`) {
		t.Errorf("import nu non reecrit: %q", got["main.go"])
	}
	if !strings.Contains(got["a.stub"], `"acme/api/app/models"`) {
		t.Errorf("stub non reecrit: %q", got["a.stub"])
	}
	if got["keep.txt"] != files["keep.txt"] {
		t.Errorf("un fichier hors .go/go.mod/.stub a ete touche: %q", got["keep.txt"])
	}
}

func TestRenameIsANoOpForTheDefaultModule(t *testing.T) {
	target := t.TempDir()
	path := filepath.Join(target, "go.mod")

	if err := os.WriteFile(path, []byte("module omega\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := rename(target, "omega"); err != nil {
		t.Fatalf("rename: %v", err)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != "module omega\n" {
		t.Errorf("go.mod modifie alors que le module est inchange: %q", raw)
	}
}

func TestWriteEnvPlantsAFreshSecret(t *testing.T) {
	target := t.TempDir()

	if err := os.WriteFile(filepath.Join(target, ".env.example"),
		[]byte("APP_PORT=3000\nAUTH_SECRET=\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := writeEnv(target); err != nil {
		t.Fatalf("writeEnv: %v", err)
	}

	raw, err := os.ReadFile(filepath.Join(target, ".env"))
	if err != nil {
		t.Fatalf("lecture du .env: %v", err)
	}

	secret := ""
	for _, line := range strings.Split(string(raw), "\n") {
		if after, found := strings.CutPrefix(line, "AUTH_SECRET="); found {
			secret = after
		}
	}
	if len(secret) < 40 {
		t.Errorf("secret trop court: %q", secret)
	}

	info, err := os.Stat(filepath.Join(target, ".env"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("le .env porte un secret, permissions = %v", info.Mode().Perm())
	}
}

func TestWriteEnvGivesEachProjectItsOwnSecret(t *testing.T) {
	read := func() string {
		target := t.TempDir()
		if err := os.WriteFile(filepath.Join(target, ".env.example"),
			[]byte("AUTH_SECRET=\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := writeEnv(target); err != nil {
			t.Fatal(err)
		}
		raw, err := os.ReadFile(filepath.Join(target, ".env"))
		if err != nil {
			t.Fatal(err)
		}
		return string(raw)
	}

	if read() == read() {
		t.Error("deux projets partagent le meme AUTH_SECRET")
	}
}

func TestWriteEnvStaysSilentWithoutExample(t *testing.T) {
	target := t.TempDir()

	if err := writeEnv(target); err != nil {
		t.Fatalf("sans .env.example, writeEnv doit se taire: %v", err)
	}
	if _, err := os.Stat(filepath.Join(target, ".env")); err == nil {
		t.Error(".env cree sans modele")
	}
}

func TestTextTrimsAndTolerates(t *testing.T) {
	args := map[string]any{"path": "  routes/api.go\n", "count": 3, "empty": ""}

	if got := text(args, "path"); got != "routes/api.go" {
		t.Errorf("text = %q", got)
	}
	if got := text(args, "count"); got != "" {
		t.Errorf("une valeur non textuelle doit rendre \"\", obtenu %q", got)
	}
	if got := text(args, "absent"); got != "" {
		t.Errorf("une cle absente doit rendre \"\", obtenu %q", got)
	}
	if got := text(nil, "path"); got != "" {
		t.Errorf("des arguments nuls doivent rendre \"\", obtenu %q", got)
	}
}

func TestMigrationCommandsExposeTheLaravelSet(t *testing.T) {
	want := []string{"migrate", "migrate:rollback", "migrate:reset", "migrate:fresh", "migrate:status", "db:seed"}

	got := []string{}
	for _, command := range migrationCommands() {
		got = append(got, strings.Fields(command.Use)[0])
	}

	for _, name := range want {
		if !contains(got, name) {
			t.Errorf("%s absent de %v", name, got)
		}
	}
}

func TestMigrateRollbackTakesAStepFlag(t *testing.T) {
	for _, command := range migrationCommands() {
		if strings.Fields(command.Use)[0] != "migrate:rollback" {
			continue
		}
		if command.Flags().Lookup("step") == nil {
			t.Error("migrate:rollback doit accepter --step")
		}
		return
	}
	t.Fatal("migrate:rollback introuvable")
}

func TestQueueCommandsExposeTheLaravelSet(t *testing.T) {
	want := []string{"queue:work", "queue:failed", "queue:retry", "queue:flush"}

	got := []string{}
	for _, command := range queueCommands() {
		got = append(got, strings.Fields(command.Use)[0])
	}

	for _, name := range want {
		if !contains(got, name) {
			t.Errorf("%s absent de %v", name, got)
		}
	}
}

func TestQueueWorkTakesItsTuningFlags(t *testing.T) {
	command := queueWorkCommand()

	for _, flag := range []string{"queue", "concurrency", "poll"} {
		if command.Flags().Lookup(flag) == nil {
			t.Errorf("queue:work doit accepter --%s", flag)
		}
	}
}

func TestScheduleCommandsAreRegistered(t *testing.T) {
	got := []string{}
	for _, command := range scheduleCommands() {
		got = append(got, strings.Fields(command.Use)[0])
	}

	if !contains(got, "schedule:run") {
		t.Errorf("schedule:run absent de %v", got)
	}
}

func TestWebCommandsCoverEveryStack(t *testing.T) {
	got := []string{}
	for _, command := range webCommands() {
		got = append(got, strings.Fields(command.Use)[0])
	}

	for _, target := range stacks {
		if !contains(got, "web:"+target.name) {
			t.Errorf("web:%s absent de %v", target.name, got)
		}
	}
	if len(got) != len(stacks) {
		t.Errorf("%d commandes web pour %d piles", len(got), len(stacks))
	}
}

func TestWebCommandTakesItsFlags(t *testing.T) {
	for _, command := range webCommands() {
		for _, flag := range []string{"force", "install", "pm", "dir"} {
			if command.Flags().Lookup(flag) == nil {
				t.Errorf("%s doit accepter --%s", strings.Fields(command.Use)[0], flag)
			}
		}
	}
}

func TestMcpToolsAnnounceEveryCapability(t *testing.T) {
	tools := mcpTools(&cobra.Command{Use: "omega"})

	want := []string{
		"omega_overview", "omega_routes", "omega_models", "omega_schema",
		"omega_openapi", "omega_commands", "omega_read", "omega_search",
	}
	got := []string{}
	for _, tool := range tools {
		got = append(got, tool.Name)

		if tool.Title == "" {
			t.Errorf("%s: titre manquant", tool.Name)
		}
		if tool.Description == "" {
			t.Errorf("%s: description manquante", tool.Name)
		}
		if tool.Run == nil {
			t.Errorf("%s: pas d'implementation", tool.Name)
		}
	}

	for _, name := range want {
		if !contains(got, name) {
			t.Errorf("%s absent de %v", name, got)
		}
	}
}

func TestMcpCmdIsWiredForStdio(t *testing.T) {
	command := mcpCmd()

	if strings.Fields(command.Use)[0] != "mcp" {
		t.Errorf("Use = %q", command.Use)
	}
	if !command.SilenceUsage {
		t.Error("le protocole passe par stdout, l'usage ne doit pas s'y melanger")
	}
	if !strings.Contains(command.Long, "claude mcp add") {
		t.Error("l'aide doit montrer comment enregistrer le serveur")
	}
}

func TestCommandTreeListsWhatTheCliOffers(t *testing.T) {
	root := &cobra.Command{Use: "omega"}
	root.AddCommand(&cobra.Command{Use: "migrate", Short: "Run the pending migrations"})
	root.AddCommand(&cobra.Command{Use: "make:model <name>", Short: "Create a model"})

	got := commandTree(root)

	for _, want := range []string{"migrate", "make:model", "Create a model"} {
		if !strings.Contains(got, want) {
			t.Errorf("%q absent de l'arbre:\n%s", want, got)
		}
	}
}

func TestAliasCommandForwardsToItsTarget(t *testing.T) {
	root := &cobra.Command{Use: "omega", SilenceUsage: true, SilenceErrors: true}

	seen := []string{}
	root.AddCommand(&cobra.Command{
		Use:  "make:resource",
		Args: cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			seen = args
			return nil
		},
	})
	root.AddCommand(aliasCommand("make:presenter", "make:resource"))

	root.SetArgs([]string{"make:presenter", "Invoice"})
	if err := root.Execute(); err != nil {
		t.Fatalf("execution de l'alias: %v", err)
	}
	if len(seen) != 1 || seen[0] != "Invoice" {
		t.Errorf("la cible a recu %v, attendu [Invoice]", seen)
	}
}

func TestServeCommandsTakeTheirFlags(t *testing.T) {
	cases := []struct {
		command *cobra.Command
		flags   []string
	}{
		{serveCmd(), []string{"port"}},
		{buildCmd(), []string{"output", "drivers"}},
		{testCmd(), []string{"coverage"}},
	}

	for _, item := range cases {
		name := strings.Fields(item.command.Use)[0]
		for _, flag := range item.flags {
			if item.command.Flags().Lookup(flag) == nil {
				t.Errorf("%s doit accepter --%s", name, flag)
			}
		}
	}
}

func TestSimpleCommandsAnnounceThemselves(t *testing.T) {
	for _, command := range []*cobra.Command{serveCmd(), devCmd(), buildCmd(), testCmd(), routesCmd(), newCommand()} {
		name := strings.Fields(command.Use)[0]

		if command.Short == "" {
			t.Errorf("%s: pas de description courte", name)
		}
		if command.RunE == nil {
			t.Errorf("%s: pas d'implementation", name)
		}
	}
}

func TestNewCommandTakesOneNameAndItsFlags(t *testing.T) {
	command := newCommand()

	for _, flag := range []string{"module", "web", "no-git"} {
		if command.Flags().Lookup(flag) == nil {
			t.Errorf("new doit accepter --%s", flag)
		}
	}
	if err := command.Args(command, []string{}); err == nil {
		t.Error("new sans nom doit echouer")
	}
	if err := command.Args(command, []string{"a", "b"}); err == nil {
		t.Error("new avec deux noms doit echouer")
	}
	if err := command.Args(command, []string{"a"}); err != nil {
		t.Errorf("new avec un nom: %v", err)
	}
}

func TestStaleBinaryComparesAgainstTheSources(t *testing.T) {
	age, ok := staleBinary()

	if !ok {
		return
	}
	if age == 0 {
		t.Error("staleBinary annonce un ecart mais le chiffre a zero")
	}
}

func TestStaleBinaryStaysSilentOutsideAProject(t *testing.T) {
	dir := t.TempDir()
	previous, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(previous) })

	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	if _, ok := staleBinary(); ok {
		t.Error("sans source Go, staleBinary ne doit rien annoncer")
	}
}

func TestPrintRoutesNamesTheResource(t *testing.T) {
	out := captureStdout(t, func() { printRoutes(parseNames("invoice_line")) })

	for _, want := range []string{"invoice_lines", "InvoiceLine", "omega migrate", "routes/api.go"} {
		if !strings.Contains(out, want) {
			t.Errorf("%q absent de la sortie:\n%s", want, out)
		}
	}
}

func TestPrintWebNextShowsTheChosenManager(t *testing.T) {
	chosen, err := byName("npm")
	if err != nil {
		t.Fatal(err)
	}

	for _, target := range stacks {
		out := captureStdout(t, func() { printWebNext(target, "web", chosen) })

		for _, want := range []string{"npm install", "npm run dev", "web:" + target.name, target.cli} {
			if !strings.Contains(out, want) {
				t.Errorf("%s: %q absent de la sortie:\n%s", target.name, want, out)
			}
		}
	}
}

func TestScaffoldResourceWritesEveryLayer(t *testing.T) {
	dir := t.TempDir()
	previous, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(previous) })

	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}

	n := parseNames("invoice")
	out := captureStdout(t, func() {
		if err := scaffoldResource(n, false); err != nil {
			t.Errorf("scaffoldResource: %v", err)
		}
	})
	if out == "" {
		t.Error("scaffoldResource doit rappeler les routes a ajouter")
	}

	for _, expected := range []string{
		filepath.Join("app", "models", "invoice.go"),
		filepath.Join("app", "requests", "invoice", "requests.go"),
		filepath.Join("app", "services", "invoice", "service.go"),
		filepath.Join("app", "presenters", "invoice", "presenter.go"),
		filepath.Join("app", "controllers", "invoice", "controller.go"),
		migrationPath(n, "create_invoices_table"),
	} {
		if _, err := os.Stat(filepath.Join(dir, expected)); err != nil {
			t.Errorf("%s manquant", expected)
		}
	}
}

func TestScaffoldResourceStopsOnCollision(t *testing.T) {
	dir := t.TempDir()
	previous, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(previous) })

	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}

	model := filepath.Join(dir, "app", "models", "invoice.go")
	if err := os.MkdirAll(filepath.Dir(model), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(model, []byte("package models\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	err = scaffoldResource(parseNames("invoice"), false)
	if err == nil {
		t.Fatal("un fichier existant doit arreter le scaffolding")
	}
	if !strings.Contains(err.Error(), "already exists") {
		t.Errorf("message inattendu: %v", err)
	}
}

func captureStdout(t *testing.T, fn func()) string {
	t.Helper()

	read, write, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	previous := os.Stdout
	os.Stdout = write

	done := make(chan string, 1)
	go func() {
		var buf bytes.Buffer
		_, _ = buf.ReadFrom(read)
		done <- buf.String()
	}()

	fn()

	os.Stdout = previous
	_ = write.Close()
	out := <-done
	_ = read.Close()

	return out
}

func insideProject(t *testing.T) {
	t.Helper()

	root, ok := projectRoot()
	if !ok {
		t.Skip("hors du projet")
	}

	previous, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(previous) })

	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}

	previousConfig := configDir
	configDir = "config"
	t.Cleanup(func() { configDir = previousConfig })

	t.Setenv("DB_CONNECTION", "sqlite")
	t.Setenv("DB_SQLITE_PATH", filepath.Join(t.TempDir(), "boot.db"))
	t.Setenv("AUTH_SECRET", "un-secret-de-test-assez-long-pour-signer-correctement")
	t.Setenv("LOG_LEVEL", "error")
}

func TestBootWebCarriesTheApiRoutes(t *testing.T) {
	insideProject(t)

	app, err := bootWeb()
	if err != nil {
		t.Fatalf("bootWeb: %v", err)
	}
	defer func() { _ = app.Shutdown() }()

	if app.Fiber == nil {
		t.Fatal("bootWeb doit monter Fiber")
	}

	found := false
	for _, stack := range app.Fiber.Stack() {
		for _, route := range stack {
			if strings.HasPrefix(route.Path, "/api") {
				found = true
			}
		}
	}
	if !found {
		t.Error("aucune route /api enregistree")
	}
}

func TestBootWorkerLeavesHttpBehind(t *testing.T) {
	insideProject(t)

	app, err := bootWorker()
	if err != nil {
		t.Fatalf("bootWorker: %v", err)
	}
	defer func() { _ = app.Shutdown() }()

	if app.DB == nil {
		t.Error("un worker a besoin de la base")
	}
}

func TestBootCliOpensTheDatabase(t *testing.T) {
	insideProject(t)

	app, err := bootCLI()
	if err != nil {
		t.Fatalf("bootCLI: %v", err)
	}
	defer func() { _ = app.Shutdown() }()

	if app.DB == nil {
		t.Fatal("bootCLI doit ouvrir la base")
	}
}

func TestWithDbHandsOverTheApp(t *testing.T) {
	insideProject(t)

	seen := false
	if err := withDB(func(app *omega.App) error {
		seen = app.DB != nil
		return nil
	}); err != nil {
		t.Fatalf("withDB: %v", err)
	}
	if !seen {
		t.Error("withDB doit passer une application connectee")
	}
}

func TestWithDbReturnsWhatTheCallbackRefuses(t *testing.T) {
	insideProject(t)

	sentinel := errors.New("refus du test")
	if err := withDB(func(*omega.App) error { return sentinel }); !errors.Is(err, sentinel) {
		t.Errorf("withDB doit rendre l'erreur du callback, obtenu %v", err)
	}
}

func TestRouteTableListsMethodPathAndName(t *testing.T) {
	insideProject(t)

	table, err := routeTable()
	if err != nil {
		t.Fatalf("routeTable: %v", err)
	}
	if table == "" {
		t.Fatal("table de routes vide")
	}
	if strings.Contains(table, "HEAD") {
		t.Error("les routes HEAD doublent les GET, elles ne doivent pas figurer")
	}

	for _, line := range strings.Split(strings.TrimSpace(table), "\n") {
		if len(strings.Fields(line)) < 3 {
			t.Errorf("ligne incomplete: %q", line)
		}
	}
}

func TestOpenApiDocumentIsValidJson(t *testing.T) {
	insideProject(t)

	raw, err := openAPIDocument()
	if err != nil {
		t.Fatalf("openAPIDocument: %v", err)
	}

	var document map[string]any
	if err := json.Unmarshal([]byte(raw), &document); err != nil {
		t.Fatalf("document illisible: %v", err)
	}
	for _, key := range []string{"openapi", "info", "paths"} {
		if _, ok := document[key]; !ok {
			t.Errorf("%q absent du document", key)
		}
	}
}

func TestDatabaseSchemaDescribesTheTables(t *testing.T) {
	insideProject(t)

	if err := withDB(func(app *omega.App) error {
		return app.DB.Exec(`CREATE TABLE couverture (id INTEGER PRIMARY KEY, libelle TEXT)`).Error
	}); err != nil {
		t.Fatalf("creation de la table: %v", err)
	}

	schema, err := databaseSchema()
	if err != nil {
		t.Fatalf("databaseSchema: %v", err)
	}

	if !strings.Contains(schema, "couverture") {
		t.Errorf("la table absente du schema:\n%s", schema)
	}
	if !strings.Contains(schema, "libelle") {
		t.Errorf("les colonnes absentes du schema:\n%s", schema)
	}
	if !strings.Contains(schema, "null=") {
		t.Errorf("la nullabilite absente du schema:\n%s", schema)
	}
}

func TestInitGitStartsARepositoryInTheTarget(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git absent")
	}

	target := t.TempDir()
	initGit(target)

	if _, err := os.Stat(filepath.Join(target, ".git")); err != nil {
		t.Errorf("aucun depot cree dans %s", target)
	}
}

func TestConfigDirIsUsableBeforeCobraParses(t *testing.T) {
	if configDir != "config" {
		t.Errorf("configDir = %q hors du binaire, attendu \"config\"", configDir)
	}
}
