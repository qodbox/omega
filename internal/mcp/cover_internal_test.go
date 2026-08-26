package mcp

import (
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"
)

type covBrokenWriter struct{}

func (covBrokenWriter) Write([]byte) (int, error) { return 0, errors.New("tuyau ferme") }

type covBrokenReader struct{}

func (covBrokenReader) Read([]byte) (int, error) { return 0, errors.New("lecture impossible") }

func covTools() []Tool {
	return []Tool{
		{
			Name:        "cov_ok",
			Title:       "Rend un texte",
			Description: "Toujours content.",
			InputSchema: Schema(nil),
			Run:         func(map[string]any) (string, error) { return "bonjour", nil },
		},
		{
			Name:        "cov_ko",
			Title:       "Echoue",
			Description: "Toujours mecontent.",
			InputSchema: Schema(map[string]string{"path": "un chemin"}, "path"),
			Run:         func(map[string]any) (string, error) { return "", errors.New("refus de l'outil") },
		},
	}
}

func covExchange(t *testing.T, lines ...string) []map[string]any {
	t.Helper()

	var out strings.Builder
	if err := New("1.0.0", covTools()).Serve(strings.NewReader(strings.Join(lines, "\n")+"\n"), &out); err != nil {
		t.Fatalf("Serve: %v", err)
	}

	var replies []map[string]any
	for _, raw := range strings.Split(strings.TrimSpace(out.String()), "\n") {
		if raw == "" {
			continue
		}
		var reply map[string]any
		if err := json.Unmarshal([]byte(raw), &reply); err != nil {
			t.Fatalf("reponse illisible %q: %v", raw, err)
		}
		replies = append(replies, reply)
	}
	return replies
}

func TestServeAnswersInitialize(t *testing.T) {
	replies := covExchange(t, `{"jsonrpc":"2.0","id":1,"method":"initialize"}`)

	if len(replies) != 1 {
		t.Fatalf("%d reponses, attendu 1", len(replies))
	}

	result, ok := replies[0]["result"].(map[string]any)
	if !ok {
		t.Fatalf("resultat absent: %v", replies[0])
	}
	if result["protocolVersion"] != protocolVersion {
		t.Errorf("protocolVersion = %v", result["protocolVersion"])
	}

	info, ok := result["serverInfo"].(map[string]any)
	if !ok || info["version"] != "1.0.0" {
		t.Errorf("serverInfo = %v", result["serverInfo"])
	}
}

func TestServeAnswersPingAndToolsList(t *testing.T) {
	replies := covExchange(t,
		`{"jsonrpc":"2.0","id":1,"method":"ping"}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`,
	)

	if len(replies) != 2 {
		t.Fatalf("%d reponses, attendu 2", len(replies))
	}

	result := replies[1]["result"].(map[string]any)
	tools, ok := result["tools"].([]any)
	if !ok || len(tools) != 2 {
		t.Fatalf("tools = %v", result["tools"])
	}
}

func TestServeRunsAToolAndReturnsItsText(t *testing.T) {
	replies := covExchange(t, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"cov_ok"}}`)

	result := replies[0]["result"].(map[string]any)
	content := result["content"].([]any)
	first := content[0].(map[string]any)

	if first["text"] != "bonjour" {
		t.Errorf("texte = %v", first["text"])
	}
	if _, flagged := result["isError"]; flagged {
		t.Error("un succes ne doit pas etre marque en erreur")
	}
}

func TestServeMarksAToolFailureWithoutBreakingTheProtocol(t *testing.T) {
	replies := covExchange(t, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"cov_ko"}}`)

	result := replies[0]["result"].(map[string]any)
	if result["isError"] != true {
		t.Errorf("isError = %v, attendu true", result["isError"])
	}
	if _, isRpcError := replies[0]["error"]; isRpcError {
		t.Error("un echec d'outil n'est pas une erreur de protocole")
	}

	content := result["content"].([]any)
	if !strings.Contains(content[0].(map[string]any)["text"].(string), "refus de l'outil") {
		t.Errorf("le message de l'outil doit remonter: %v", content)
	}
}

func TestServeRejectsAnUnknownToolAndMethod(t *testing.T) {
	replies := covExchange(t,
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"cov_absent"}}`,
		`{"jsonrpc":"2.0","id":2,"method":"methode/inconnue"}`,
	)

	for i, want := range []int{-32602, -32601} {
		failure, ok := replies[i]["error"].(map[string]any)
		if !ok {
			t.Fatalf("reponse %d sans erreur: %v", i, replies[i])
		}
		if int(failure["code"].(float64)) != want {
			t.Errorf("code = %v, attendu %d", failure["code"], want)
		}
	}
}

func TestServeRejectsBrokenParameters(t *testing.T) {
	replies := covExchange(t, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":"pas-un-objet"}`)

	failure, ok := replies[0]["error"].(map[string]any)
	if !ok {
		t.Fatalf("erreur attendue: %v", replies[0])
	}
	if int(failure["code"].(float64)) != -32602 {
		t.Errorf("code = %v", failure["code"])
	}
}

func TestServeIgnoresNoiseAndNotifications(t *testing.T) {
	replies := covExchange(t,
		`pas du json`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","id":1,"method":"ping"}`,
	)

	if len(replies) != 1 {
		t.Fatalf("%d reponses, attendu 1 — le bruit et les notifications ne se repondent pas", len(replies))
	}
}

func TestServeStopsOnAReadFailure(t *testing.T) {
	var out strings.Builder

	err := New("1.0.0", covTools()).Serve(covBrokenReader{}, &out)
	if err == nil {
		t.Fatal("une lecture impossible doit remonter")
	}
}

func TestServeSurvivesAWriteFailure(t *testing.T) {
	in := strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"ping"}` + "\n")

	if err := New("1.0.0", covTools()).Serve(in, covBrokenWriter{}); err != nil {
		t.Fatalf("une ecriture impossible ne doit pas arreter le serveur: %v", err)
	}
}

func TestServeStopsCleanlyAtEndOfInput(t *testing.T) {
	if err := New("1.0.0", covTools()).Serve(strings.NewReader(""), io.Discard); err != nil {
		t.Fatalf("une entree vide doit se terminer proprement: %v", err)
	}
}

func TestSchemaMarksTheRequiredFields(t *testing.T) {
	schema := Schema(map[string]string{"path": "un chemin", "query": "une recherche"}, "path")

	if schema["type"] != "object" {
		t.Errorf("type = %v", schema["type"])
	}

	props := schema["properties"].(map[string]any)
	if len(props) != 2 {
		t.Errorf("proprietes = %v", props)
	}
	path := props["path"].(map[string]any)
	if path["type"] != "string" || path["description"] != "un chemin" {
		t.Errorf("propriete path = %v", path)
	}

	required, ok := schema["required"].([]string)
	if !ok || len(required) != 1 || required[0] != "path" {
		t.Errorf("required = %v", schema["required"])
	}

	if _, has := Schema(nil)["required"]; has {
		t.Error("sans champ obligatoire, la cle required ne doit pas apparaitre")
	}
}
