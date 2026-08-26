package kernel

import "testing"

func TestContainerBindsLooksUpAndLists(t *testing.T) {
	c := &Container{}

	c.Bind("horloge", "midi")
	c.Bind("compteur", 7)

	if value, ok := c.Lookup("horloge"); !ok || value != "midi" {
		t.Fatalf("Lookup = %v, %v", value, ok)
	}
	if _, ok := c.Lookup("absent"); ok {
		t.Error("Lookup trouve un service inexistant")
	}

	names := c.Names()
	if len(names) != 2 || names[0] != "compteur" || names[1] != "horloge" {
		t.Fatalf("Names = %v — attendu trie", names)
	}
}

func TestResolveReportsTheWrongType(t *testing.T) {
	c := &Container{}
	c.Bind("horloge", "midi")

	if got, err := Resolve[string](c, "horloge"); err != nil || got != "midi" {
		t.Fatalf("Resolve = %q, %v", got, err)
	}
	if _, err := Resolve[int](c, "horloge"); err == nil {
		t.Error("Resolve accepte le mauvais type")
	}
	if _, err := Resolve[string](c, "absent"); err == nil {
		t.Error("Resolve accepte un nom inconnu")
	}
}

func TestMustResolveReturnsOrPanics(t *testing.T) {
	c := &Container{}
	c.Bind("horloge", "midi")

	if got := MustResolve[string](c, "horloge"); got != "midi" {
		t.Fatalf("MustResolve = %q", got)
	}

	defer func() {
		if recover() == nil {
			t.Error("MustResolve n'a pas panique sur un nom inconnu")
		}
	}()
	_ = MustResolve[string](c, "absent")
}

func TestGlobalContainerIsASingleton(t *testing.T) {
	if Global() != Global() {
		t.Error("Global() renvoie deux conteneurs differents")
	}
}
