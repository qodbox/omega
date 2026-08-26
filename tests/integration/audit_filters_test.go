package integration

import (
	"net/http"
	"testing"
)

func TestEveryFilterOperatorIsHonoured(t *testing.T) {
	s := boot(t)
	s.signUp("alpha@integration.test")
	s.token = ""
	s.signUp("beta@integration.test")
	s.token = ""
	s.signUpAdmin("gamma@integration.test")

	cases := []struct {
		query string
		want  int
	}{
		{"email=alpha@integration.test", 1},
		{"email__ne=alpha@integration.test", 2},
		{"email__like=%25beta%25", 1},
		{"id__gt=1", 2},
		{"id__gte=1", 3},
		{"id__lt=3", 2},
		{"id__lte=3", 3},
		{"id__in=1,3", 2},
		{"name__like=%25Integration%25", 3},
	}

	for _, tc := range cases {
		answer := s.get("/api/users?" + tc.query)
		if answer.status != http.StatusOK {
			t.Errorf("%s: status = %d — %s", tc.query, answer.status, truncate(answer.raw))
			continue
		}
		if got := len(answer.list()); got != tc.want {
			t.Errorf("%s: %d ligne(s), want %d", tc.query, got, tc.want)
		}
	}
}

func TestFilterOperatorsRejectAnUnknownColumn(t *testing.T) {
	s := boot(t)
	s.signUp("inconnu@integration.test")

	for _, query := range []string{
		"bogus=1", "bogus__gt=1", "bogus__in=1,2", "password__like=%a%", "deleted_at__ne=x",
	} {
		if answer := s.get("/api/users?" + query); answer.status != http.StatusUnprocessableEntity {
			t.Errorf("%s: status = %d, want 422", query, answer.status)
		}
	}
}

func TestFiltersCombineWithPaginationAndSort(t *testing.T) {
	s := boot(t)
	s.signUp("combo@integration.test")

	answer := s.get("/api/users?id__gte=1&sort=-id&page=1&per_page=1")
	if answer.status != http.StatusOK {
		t.Fatalf("status = %d — %s", answer.status, truncate(answer.raw))
	}
	if got := len(answer.list()); got != 1 {
		t.Fatalf("%d ligne(s), want 1", got)
	}
}
