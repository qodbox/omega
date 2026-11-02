package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/rs/zerolog/log"
)

// decodeBody reads a JSON object keeping every number as json.Number, so an
// amount never goes through a float64.
func decodeBody(raw []byte) (map[string]any, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()

	var payload map[string]any
	if err := decoder.Decode(&payload); err != nil || payload == nil {
		return nil, errInvalidJSON
	}
	if decoder.More() {
		return nil, errInvalidJSON
	}
	return payload, nil
}

var errInvalidJSON = fmt.Errorf("invalid JSON body")

// payload turns the client's values into the columns a write may touch.
//
// Mass assignment: a key that is not a writable field — unknown, read-only,
// hidden, guarded for this actor — is ignored, as it always was, and logged at
// debug level. Rejecting it would break clients that send back what they read
// (id, created_at); ignoring it cannot write anything the resource does not
// allow. Values of the fields kept are validated and normalised.
func (r *Resource) payload(ctx context.Context, granted *access, raw map[string]any, partial bool, digits int) (map[string]any, error) {
	privileged := granted.privileged()
	payload := map[string]any{}
	used := map[string]bool{}

	for _, field := range r.Fields {
		value, sent := raw[field.Name]
		if !sent {
			continue
		}
		used[field.Name] = true

		if !r.writableColumn(field, privileged) {
			continue
		}
		if partial && field.Name == "id" {
			continue
		}
		if r.Owner != "" && field.Column == r.Owner && !granted.unscoped {
			continue
		}

		normalised, err := normalise(field, value, digits)
		if err != nil {
			return nil, err
		}
		payload[field.Column] = normalised
	}

	if ignored := ignoredKeys(raw, used, payload, r); len(ignored) > 0 {
		log.Ctx(ctx).Debug().Str("resource", r.Plural).Strs("fields", ignored).
			Msg("api: fields ignored on write")
	}

	if len(payload) == 0 && !partial {
		return nil, invalid("no writable field in the body")
	}
	if !partial && r.Owner != "" && !granted.unscoped {
		payload[r.Owner] = granted.ownerID
	}
	return payload, nil
}

func ignoredKeys(raw map[string]any, used map[string]bool, payload map[string]any, r *Resource) []string {
	ignored := []string{}
	for key := range raw {
		field, known := r.field(key)
		if !known || !used[key] {
			ignored = append(ignored, key)
			continue
		}
		if _, kept := payload[field.Column]; !kept {
			ignored = append(ignored, key)
		}
	}
	sort.Strings(ignored)
	return ignored
}

var decimalPattern = regexp.MustCompile(`^[+-]?(\d+)(?:\.(\d+))?$`)

// normalise validates one value. digits, when positive, is the number of
// significant digits the database stores exactly in a decimal column.
func normalise(field Field, value any, digits int) (any, error) {
	if value == nil {
		return nil, nil
	}

	switch value.(type) {
	case map[string]any, []any:
		if field.Type == "object" || field.Type == "array" {
			encoded, err := json.Marshal(value)
			if err != nil {
				return nil, invalid("%s: cannot be encoded", field.Name)
			}
			return string(encoded), nil
		}
		return nil, invalid("%s must be a scalar value", field.Name)
	}

	switch {
	case field.Decimal:
		return decimalValue(field, value, digits)
	case field.Type == "integer":
		return integerValue(field, value)
	case field.Type == "number":
		return numberValue(field, value)
	case field.Type == "boolean":
		if typed, ok := value.(bool); ok {
			return typed, nil
		}
		return nil, invalid("%s must be a boolean", field.Name)
	}

	text, isText := value.(string)
	if number, isNumber := value.(json.Number); isNumber {
		text, isText = number.String(), true
	}
	if !isText {
		return value, nil
	}
	if field.Size > 0 && utf8.RuneCountInString(text) > field.Size {
		return nil, invalid("%s is longer than %d characters", field.Name, field.Size)
	}
	return text, nil
}

func decimalValue(field Field, value any, digits int) (any, error) {
	var text string
	switch typed := value.(type) {
	case json.Number:
		text = typed.String()
	case string:
		text = strings.TrimSpace(typed)
	case int:
		text = strconv.Itoa(typed)
	case int64:
		text = strconv.FormatInt(typed, 10)
	default:
		// A float64 here would already have lost digits: GraphQL declares these
		// fields as String, REST decodes with UseNumber.
		return nil, invalid("%s must be a decimal number written as a string or a JSON number", field.Name)
	}

	match := decimalPattern.FindStringSubmatch(text)
	if match == nil {
		return nil, invalid("%s must be a decimal number without exponent", field.Name)
	}
	if field.Precision > 0 {
		integer := strings.TrimLeft(match[1], "0")
		if len(integer) > field.Precision-field.Scale {
			return nil, invalid("%s has more than %d integer digits", field.Name, field.Precision-field.Scale)
		}
		if len(strings.TrimRight(match[2], "0")) > field.Scale {
			return nil, invalid("%s has more than %d decimals", field.Name, field.Scale)
		}
	}
	if digits > 0 && significantDigits(match[1], match[2]) > digits {
		// SQLite stores a NUMERIC column as REAL as soon as the text looks like
		// a number, keeping 15 significant digits: refuse rather than round.
		return nil, invalid("%s has more than %d significant digits, which this database cannot store exactly", field.Name, digits)
	}
	return text, nil
}

func significantDigits(integer, fraction string) int {
	digits := strings.TrimLeft(integer+strings.TrimRight(fraction, "0"), "0")
	if digits == "" {
		return 1
	}
	return len(digits)
}

func integerValue(field Field, value any) (any, error) {
	switch typed := value.(type) {
	case int:
		return int64(typed), nil
	case int64:
		return typed, nil
	case json.Number:
		parsed, err := strconv.ParseInt(typed.String(), 10, 64)
		if err != nil {
			return nil, invalid("%s must be an integer", field.Name)
		}
		return parsed, nil
	case string:
		parsed, err := strconv.ParseInt(strings.TrimSpace(typed), 10, 64)
		if err != nil {
			return nil, invalid("%s must be an integer", field.Name)
		}
		return parsed, nil
	case float64:
		if typed == math.Trunc(typed) && math.Abs(typed) < 1<<53 {
			return int64(typed), nil
		}
	}
	return nil, invalid("%s must be an integer", field.Name)
}

func numberValue(field Field, value any) (any, error) {
	var text string
	switch typed := value.(type) {
	case float64:
		return typed, nil
	case bool:
		// SQLite declares booleans NUMERIC: a discovered boolean is a number.
		return typed, nil
	case int:
		return float64(typed), nil
	case json.Number:
		text = typed.String()
	case string:
		text = strings.TrimSpace(typed)
	default:
		return nil, invalid("%s must be a number", field.Name)
	}
	parsed, err := strconv.ParseFloat(text, 64)
	if err != nil || math.IsInf(parsed, 0) || math.IsNaN(parsed) {
		return nil, invalid("%s must be a number", field.Name)
	}
	return parsed, nil
}
