package integration

import (
	"net/http"
	"testing"

	"golang.org/x/crypto/bcrypt"

	"omega/app/models"
)

func refreshWith(s *server, token string) response {
	return s.post("/api/auth/refresh", map[string]any{"refresh_token": token})
}

func TestReusingARotatedRefreshTokenRevokesTheWholeFamily(t *testing.T) {
	s := boot(t)
	first := s.signUp("family@integration.test")
	original, _ := first.tokens()["refresh_token"].(string)

	// Another login of the same account: a separate family that must survive.
	other := s.post("/api/auth/login", map[string]any{"email": "family@integration.test", "password": password})
	otherRefresh, _ := other.tokens()["refresh_token"].(string)

	rotated := refreshWith(s, original)
	if rotated.status != http.StatusOK {
		t.Fatalf("first rotation: status = %d — %s", rotated.status, rotated.raw)
	}
	child, _ := rotated.tokens()["refresh_token"].(string)

	// The thief (or the owner) replays the spent token.
	if replay := refreshWith(s, original); replay.status != http.StatusUnauthorized {
		t.Fatalf("replay: status = %d, want 401", replay.status)
	}

	// The legitimate child of the family died with it.
	if answer := refreshWith(s, child); answer.status != http.StatusUnauthorized {
		t.Errorf("the family survived the reuse: status = %d", answer.status)
	}

	// The other login is untouched.
	if answer := refreshWith(s, otherRefresh); answer.status != http.StatusOK {
		t.Errorf("an unrelated session was revoked: status = %d", answer.status)
	}
}

func TestReuseGraceRefusesWithoutRevoking(t *testing.T) {
	s := bootWith(t, map[string]string{"AUTH_REUSE_GRACE": "1m"})
	first := s.signUp("grace@integration.test")
	original, _ := first.tokens()["refresh_token"].(string)

	rotated := refreshWith(s, original)
	child, _ := rotated.tokens()["refresh_token"].(string)

	if replay := refreshWith(s, original); replay.status != http.StatusUnauthorized {
		t.Fatalf("a replay inside the grace must still be refused: %d", replay.status)
	}
	if answer := refreshWith(s, child); answer.status != http.StatusOK {
		t.Errorf("inside the grace the family must survive: status = %d", answer.status)
	}
}

func TestLogoutAllRefusesTheAccessTokensAlreadyIssued(t *testing.T) {
	s := boot(t)
	s.signUp("everywhere@integration.test")
	stolen := s.token
	refresh := s.post("/api/auth/login", map[string]any{"email": "everywhere@integration.test", "password": password})

	if answer := s.post("/api/auth/logout-all", nil); answer.status != http.StatusNoContent {
		t.Fatalf("logout-all: status = %d — %s", answer.status, answer.raw)
	}

	s.token = stolen
	if answer := s.get("/api/auth/me"); answer.status != http.StatusUnauthorized {
		t.Errorf("an access token survived logout-all: status = %d", answer.status)
	}
	token, _ := refresh.tokens()["refresh_token"].(string)
	if answer := refreshWith(s, token); answer.status != http.StatusUnauthorized {
		t.Errorf("a refresh token survived logout-all: status = %d", answer.status)
	}
}

func TestPasswordChangeRefusesTheOldAccessTokens(t *testing.T) {
	s := boot(t)
	s.signUp("version@integration.test")
	old := s.token

	answer := s.post("/api/auth/password", map[string]any{
		"current_password": password, "new_password": "a-brand-new-long-passphrase",
	})
	if answer.status != http.StatusOK {
		t.Fatalf("change: status = %d — %s", answer.status, answer.raw)
	}
	fresh, _ := answer.tokens()["access_token"].(string)

	s.token = old
	if s.get("/api/auth/me").status != http.StatusUnauthorized {
		t.Error("the access token signed before the change still works")
	}
	s.token = fresh
	if s.get("/api/auth/me").status != http.StatusOK {
		t.Error("the access token handed back by the change does not work")
	}
}

func TestADeletedAccountsTokenIsRefused(t *testing.T) {
	s := boot(t)
	s.signUp("gone@integration.test")
	if err := s.app.DB.Unscoped().Where("email = ?", "gone@integration.test").Delete(&models.User{}).Error; err != nil {
		t.Fatal(err)
	}
	if answer := s.get("/api/auth/me"); answer.status != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", answer.status)
	}
}

func TestLoginAnswersTheSameForUnknownEmailAndWrongPassword(t *testing.T) {
	s := boot(t)
	s.signUp("known@integration.test")
	s.token = ""

	unknown := s.post("/api/auth/login", map[string]any{"email": "nobody@integration.test", "password": "whatever-it-is"})
	wrong := s.post("/api/auth/login", map[string]any{"email": "known@integration.test", "password": "whatever-it-is"})

	if unknown.status != http.StatusUnauthorized || wrong.status != http.StatusUnauthorized {
		t.Fatalf("statuses = %d / %d, want 401 / 401", unknown.status, wrong.status)
	}
	if unknown.body["error"] != wrong.body["error"] || unknown.body["error"] != "Invalid credentials." {
		t.Errorf("messages differ: %v / %v", unknown.body["error"], wrong.body["error"])
	}
}

func TestLoginUpgradesAWeakHash(t *testing.T) {
	s := boot(t)
	s.signUp("legacy@integration.test")

	weak, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	s.app.DB.Model(&models.User{}).Where("email = ?", "legacy@integration.test").Update("password", string(weak))

	if answer := s.post("/api/auth/login", map[string]any{"email": "legacy@integration.test", "password": password}); answer.status != http.StatusOK {
		t.Fatalf("an old hash no longer verifies: %d", answer.status)
	}

	var user models.User
	s.app.DB.Where("email = ?", "legacy@integration.test").Take(&user)
	if cost, _ := bcrypt.Cost([]byte(user.Password)); cost < models.PasswordCost {
		t.Errorf("hash cost after sign-in = %d, want %d", cost, models.PasswordCost)
	}
}

func TestPasswordPolicy(t *testing.T) {
	s := boot(t)

	for name, candidate := range map[string]string{
		"11 characters":           "short-pass1",
		"over 72 bytes (accents)": "éééééééééééééééééééééééééééééééééééééééé", // 40 characters, 80 bytes
	} {
		answer := s.post("/api/auth/register", map[string]any{
			"name": "Policy", "email": "policy@integration.test", "password": candidate,
		})
		if answer.status != http.StatusUnprocessableEntity || answer.errors()["password"] == nil {
			t.Errorf("%s: status = %d — %s", name, answer.status, answer.raw)
		}
	}
}

func TestDemotionAppliesOnTheNextRequest(t *testing.T) {
	s := boot(t)
	s.signUpAdmin("demoted@integration.test")

	if answer := s.post("/api/users", map[string]any{"name": "Made", "email": "made@integration.test", "password": password}); answer.status == http.StatusForbidden {
		t.Fatalf("an admin may create users: %d", answer.status)
	}

	s.app.DB.Model(&models.User{}).Where("email = ?", "demoted@integration.test").Update("role", models.RoleUser)

	if answer := s.post("/api/users", map[string]any{"name": "Again", "email": "again@integration.test", "password": password}); answer.status != http.StatusForbidden {
		t.Errorf("a demoted admin kept the role: status = %d", answer.status)
	}
}
