package api

import (
	"encoding/json"

	"github.com/graphql-go/graphql/language/ast"
	"github.com/graphql-go/graphql/language/parser"
	"github.com/graphql-go/graphql/language/source"
)

func IntrospectionOnly(body []byte) bool {
	var request struct {
		Query string `json:"query"`
	}
	if err := json.Unmarshal(body, &request); err != nil || request.Query == "" {
		return false
	}

	document, err := parser.Parse(parser.ParseParams{
		Source: source.NewSource(&source.Source{Body: []byte(request.Query)}),
	})
	if err != nil {
		return false
	}

	sawOperation := false
	for _, definition := range document.Definitions {
		operation, ok := definition.(*ast.OperationDefinition)
		if !ok {
			if _, isFragment := definition.(*ast.FragmentDefinition); isFragment {
				continue
			}
			return false
		}

		if operation.Operation != ast.OperationTypeQuery {
			return false
		}
		sawOperation = true

		if operation.SelectionSet == nil {
			return false
		}
		for _, selection := range operation.SelectionSet.Selections {
			field, ok := selection.(*ast.Field)
			if !ok || field.Name == nil {
				return false
			}

			if len(field.Name.Value) < 2 || field.Name.Value[:2] != "__" {
				return false
			}
		}
	}
	return sawOperation
}
