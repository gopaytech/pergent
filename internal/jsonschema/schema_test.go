package jsonschema

import (
	"fmt"
	"strings"
	"testing"
)

func mustDecode(t *testing.T, s string) any {
	t.Helper()
	v, err := Decode([]byte(s), 1<<20, 64)
	if err != nil {
		t.Fatalf("decode %q: %v", s, err)
	}
	return v
}

func mustCompile(t *testing.T, s string) *Schema {
	t.Helper()
	sc, err := Compile([]byte(s))
	if err != nil {
		t.Fatalf("compile %q: %v", s, err)
	}
	return sc
}

func TestCompileRefusesUnknownKeyword(t *testing.T) {
	t.Parallel()
	_, err := Compile([]byte(`{"type":"string","maxContains":1}`))
	if err == nil || !strings.Contains(err.Error(), "maxContains") {
		t.Fatalf("want error naming maxContains, got %v", err)
	}
}

func TestCompileRefusesRemoteRef(t *testing.T) {
	t.Parallel()
	if _, err := Compile([]byte(`{"$ref":"https://x/y.json"}`)); err == nil {
		t.Fatal("want error for remote $ref")
	}
	if _, err := Compile([]byte(`{"$ref":"#/definitions/missing"}`)); err == nil {
		t.Fatal("want error for a $ref that points nowhere")
	}
	if _, err := Compile([]byte(`{"$ref":"#/definitions/a","definitions":{"a":{"$ref":"#/definitions/a"}}}`)); err == nil {
		t.Fatal("want error for a $ref cycle")
	}
}

func TestValidateKeywords(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name, schema, ok, bad string
	}{
		{"type integer", `{"type":"integer"}`, `1`, `1.5`},
		{"type list", `{"type":["string","null"]}`, `null`, `1`},
		{"required", `{"required":["a"]}`, `{"a":1}`, `{}`},
		{"additionalProperties false", `{"properties":{"a":{}},"additionalProperties":false}`, `{"a":1}`, `{"a":1,"b":2}`},
		{"additionalProperties schema", `{"additionalProperties":{"type":"string"}}`, `{"a":"x"}`, `{"a":1}`},
		{"properties", `{"properties":{"a":{"type":"string"}}}`, `{"a":"x"}`, `{"a":1}`},
		{"items", `{"items":{"type":"string"}}`, `["a"]`, `[1]`},
		{"minItems", `{"minItems":1}`, `[1]`, `[]`},
		{"uniqueItems", `{"uniqueItems":true}`, `[{"a":1},{"a":2}]`, `[{"a":1},{"a":1}]`},
		{"enum", `{"enum":["a","b"]}`, `"a"`, `"c"`},
		{"const", `{"const":"a"}`, `"a"`, `"b"`},
		{"pattern", `{"pattern":"^a+$"}`, `"aaa"`, `"b"`},
		{"minLength runes", `{"minLength":2}`, `"ab"`, `"é"`},
		{"minimum", `{"minimum":3}`, `3`, `2`},
		{"maximum", `{"maximum":3}`, `3`, `4`},
		{"oneOf", `{"oneOf":[{"type":"integer"},{"minimum":0}]}`, `-1`, `1`},
		{"anyOf", `{"anyOf":[{"type":"string"},{"type":"null"}]}`, `null`, `1`},
		{"allOf", `{"allOf":[{"type":"integer"},{"minimum":2}]}`, `3`, `1`},
		{"ref", `{"$ref":"#/definitions/x","definitions":{"x":{"type":"string"}}}`, `"a"`, `1`},
		{"annotations", `{"$schema":"x","$id":"y","id":"z","title":"t","description":"d","default":1,"$comment":"c","format":"uri"}`, `1`, ``},
	}
	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			s := mustCompile(t, c.schema)
			if errs := s.Validate(mustDecode(t, c.ok)); len(errs) != 0 {
				t.Fatalf("want pass, got %v", errs)
			}
			if c.bad == "" {
				return
			}
			errs := s.Validate(mustDecode(t, c.bad))
			if len(errs) == 0 {
				t.Fatal("want failure, got none")
			}
			for _, e := range errs {
				if !strings.Contains(e, ": ") {
					t.Fatalf("error %q has no pointer prefix", e)
				}
			}
		})
	}
}

func TestVisibleContent(t *testing.T) {
	t.Parallel()
	s := mustCompile(t, `{"type":"string","visibleContent":true}`)
	if errs := s.Validate("a"); len(errs) != 0 {
		t.Fatalf("a: %v", errs)
	}
	for name, v := range map[string]string{
		"empty":         "",
		"space tab":     " \t",
		"zero width":    "​",
		"variation sel": "️",
		"hangul filler": "ㅤ",
	} {
		if errs := s.Validate(v); len(errs) == 0 {
			t.Fatalf("%s should fail", name)
		}
	}
}

func TestDecodeLimits(t *testing.T) {
	t.Parallel()
	if _, err := Decode([]byte(`{"a":"xxxxxxxx"}`), 5, 64); err == nil {
		t.Fatal("want size error")
	}
	deep := strings.Repeat("[", 65) + strings.Repeat("]", 65)
	if _, err := Decode([]byte(deep), 1<<20, 64); err == nil {
		t.Fatal("want depth error")
	}
	ok := strings.Repeat("[", 64) + strings.Repeat("]", 64)
	if _, err := Decode([]byte(ok), 1<<20, 64); err != nil {
		t.Fatalf("depth 64 should pass: %v", err)
	}
	if _, err := Decode([]byte(`{"a":1,"a":2}`), 1<<20, 64); err == nil {
		t.Fatal("want duplicate key error")
	}
	if _, err := Decode([]byte(`{"a":{"b":1,"b":2}}`), 1<<20, 64); err == nil {
		t.Fatal("want nested duplicate key error")
	}
	if _, err := Decode([]byte(`{"a":1} x`), 1<<20, 64); err == nil {
		t.Fatal("want trailing data error")
	}
}

func TestErrorsHoldNoValues(t *testing.T) {
	t.Parallel()
	s := mustCompile(t, `{"properties":{"a":{"pattern":"^x$","enum":["y"],"const":"y"}}}`)
	errs := s.Validate(mustDecode(t, `{"a":"SECRET-12345"}`))
	if len(errs) == 0 {
		t.Fatal("want errors")
	}
	for _, e := range errs {
		if strings.Contains(e, "SECRET") {
			t.Fatalf("error holds the value: %q", e)
		}
	}
}

func TestValidateCapsErrors(t *testing.T) {
	t.Parallel()
	s := mustCompile(t, `{"items":{"type":"string"}}`)
	items := make([]string, 150)
	for i := range items {
		items[i] = fmt.Sprint(i)
	}
	errs := s.Validate(mustDecode(t, "["+strings.Join(items, ",")+"]"))
	if len(errs) != 100 {
		t.Fatalf("want 100 errors, got %d", len(errs))
	}
}

func TestDecodeRefusesHugeNumbers(t *testing.T) {
	t.Parallel()
	for _, in := range []string{`1e1000000`, `{"a":[1e-999]}`, `1` + strings.Repeat("0", 401)} {
		_, err := Decode([]byte(in), 1<<20, 64)
		if err == nil {
			t.Fatalf("%.20s: want error", in)
		}
		if strings.Contains(err.Error(), "000") {
			t.Fatalf("error holds the number: %v", err)
		}
	}
	_, err := Decode([]byte(`{"a":[1,1e1000000]}`), 1<<20, 64)
	if err == nil || !strings.Contains(err.Error(), "/a/1") {
		t.Fatalf("want pointer /a/1, got %v", err)
	}
	if _, err := Decode([]byte(`[1e400,1.5E-400,123456789012345678901234567890]`), 1<<20, 64); err != nil {
		t.Fatalf("bounded numbers should pass: %v", err)
	}
}

func TestUniqueItemsIsFast(t *testing.T) {
	t.Parallel()
	s := mustCompile(t, `{"uniqueItems":true}`)
	var b strings.Builder
	b.WriteString("[")
	for i := 0; i < 5000; i++ {
		if i > 0 {
			b.WriteString(",")
		}
		fmt.Fprintf(&b, `{"a":[%d,"x"],"b":{"c":%d.0}}`, i, i)
	}
	b.WriteString("]")
	v := mustDecode(t, b.String())
	if errs := s.Validate(v); len(errs) != 0 {
		t.Fatalf("distinct items: %v", errs)
	}
	// 1 and 1.0 are equal, and key order does not matter.
	if errs := s.Validate(mustDecode(t, `[{"a":1,"b":2},{"b":2,"a":1.0}]`)); len(errs) != 1 {
		t.Fatalf("want 1 error, got %v", errs)
	}
}

func TestVisibleContentNeedsStringType(t *testing.T) {
	t.Parallel()
	for _, s := range []string{`{"visibleContent":true}`, `{"type":"object","visibleContent":true}`, `{"type":["string"],"visibleContent":true}`} {
		if _, err := Compile([]byte(s)); err == nil {
			t.Fatalf("%s: want compile error", s)
		}
	}
}

func TestCompileRefusesNoProgressCycleThroughCombiners(t *testing.T) {
	t.Parallel()
	bad := []string{
		`{"definitions":{"x":{"allOf":[{"$ref":"#/definitions/x"}]}},"$ref":"#/definitions/x"}`,
		`{"definitions":{"x":{"anyOf":[{"type":"string"},{"$ref":"#/definitions/y"}]},"y":{"oneOf":[{"$ref":"#/definitions/x"}]}},"$ref":"#/definitions/x"}`,
	}
	for _, s := range bad {
		if _, err := Compile([]byte(s)); err == nil {
			t.Fatalf("%s: want cycle error", s)
		}
	}
	// A loop that goes down into the value is fine.
	ok := `{"definitions":{"x":{"properties":{"next":{"$ref":"#/definitions/x"}}}},"$ref":"#/definitions/x"}`
	s := mustCompile(t, ok)
	if errs := s.Validate(mustDecode(t, `{"next":{"next":{}}}`)); len(errs) != 0 {
		t.Fatalf("recursive data: %v", errs)
	}
}
