// Package jsonschema is a small JSON Schema checker. It checks the files the
// security scan reads (SARIF and the skill's report-schema.json). It knows a
// short list of keywords and refuses any other, so a schema never passes by
// accident. It uses only the standard library.
package jsonschema

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	maxErrors     = 100
	maxSchemaSize = 1 << 20
	maxSchemaDeep = 64
	maxRecursion  = 256
	maxNumDigits  = 400
	maxNumExp     = 400
)

// Decode reads one JSON value. Objects become map[string]any, numbers become
// json.Number. It refuses a duplicate key, input over maxBytes, nesting deeper
// than maxDepth and any data after the value.
func Decode(data []byte, maxBytes, maxDepth int) (any, error) {
	if len(data) > maxBytes {
		return nil, fmt.Errorf("json is %d bytes, over the %d byte limit", len(data), maxBytes)
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	v, err := decodeValue(dec, 1, maxDepth, "")
	if err != nil {
		return nil, err
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return nil, errors.New("unexpected data after the json value")
	}
	return v, nil
}

func decodeValue(dec *json.Decoder, depth, maxDepth int, path string) (any, error) {
	tok, err := dec.Token()
	if err != nil {
		return nil, err
	}
	d, ok := tok.(json.Delim)
	if !ok {
		if num, isNum := tok.(json.Number); isNum && !numberInBounds(num.String()) {
			return nil, fmt.Errorf("%s: number is too large or too long to check", pointerOrRoot(path))
		}
		return tok, nil
	}
	if depth > maxDepth {
		return nil, fmt.Errorf("json is nested deeper than %d levels", maxDepth)
	}
	if d == '[' {
		arr := []any{}
		for dec.More() {
			v, err := decodeValue(dec, depth+1, maxDepth, fmt.Sprintf("%s/%d", path, len(arr)))
			if err != nil {
				return nil, err
			}
			arr = append(arr, v)
		}
		if _, err := dec.Token(); err != nil {
			return nil, err
		}
		return arr, nil
	}
	obj := map[string]any{}
	for dec.More() {
		kt, err := dec.Token()
		if err != nil {
			return nil, err
		}
		key, _ := kt.(string)
		if _, dup := obj[key]; dup {
			return nil, errors.New("json has a duplicate object key")
		}
		v, err := decodeValue(dec, depth+1, maxDepth, path+"/"+escapePointer(key))
		if err != nil {
			return nil, err
		}
		obj[key] = v
	}
	if _, err := dec.Token(); err != nil {
		return nil, err
	}
	return obj, nil
}

func pointerOrRoot(p string) string {
	if p == "" {
		return "/"
	}
	return p
}

// numberInBounds keeps big.Rat work small: at most maxNumDigits digits and an
// exponent no bigger than maxNumExp. The text is a number the JSON decoder
// already accepted.
func numberInBounds(s string) bool {
	mant, exp := s, ""
	if i := strings.IndexAny(s, "eE"); i >= 0 {
		mant, exp = s[:i], s[i+1:]
	}
	if len(mant) > maxNumDigits {
		return false
	}
	if exp != "" {
		e, err := strconv.Atoi(exp)
		if err != nil || e > maxNumExp || e < -maxNumExp {
			return false
		}
	}
	return true
}

// Schema is a compiled schema.
type Schema struct {
	root *node
}

type node struct {
	types      []string
	props      map[string]*node
	required   []string
	addAllowed bool // additionalProperties is false when set with addSchema nil
	addSet     bool
	addSchema  *node
	items      *node
	minItems   *int
	unique     bool
	minimum    *big.Rat
	maximum    *big.Rat
	minLength  *int
	enum       []any
	hasConst   bool
	konst      any
	pattern    *regexp.Regexp
	oneOf      []*node
	anyOf      []*node
	allOf      []*node
	ref        string
	target     *node
	visible    bool
}

type compiler struct {
	doc   any
	cache map[string]*node
}

var annotations = map[string]bool{
	"$schema": true, "$id": true, "id": true, "title": true,
	"description": true, "default": true, "$comment": true, "format": true,
}

// Compile checks a schema and prepares it. It refuses an unknown keyword, a
// $ref that is not local, a $ref to nothing and a $ref cycle with no progress.
func Compile(data []byte) (*Schema, error) {
	doc, err := Decode(data, maxSchemaSize, maxSchemaDeep)
	if err != nil {
		return nil, fmt.Errorf("schema: %w", err)
	}
	c := &compiler{doc: doc, cache: map[string]*node{}}
	root, err := c.ref("#")
	if err != nil {
		return nil, err
	}
	// Compile every definition now, so a bad keyword there is found too.
	if m, ok := doc.(map[string]any); ok {
		if defs, ok := m["definitions"].(map[string]any); ok {
			names := make([]string, 0, len(defs))
			for k := range defs {
				names = append(names, k)
			}
			sort.Strings(names)
			for _, k := range names {
				if _, err := c.ref("#/definitions/" + escapePointer(k)); err != nil {
					return nil, err
				}
			}
		}
	}
	if err := c.noProgressCycle(); err != nil {
		return nil, err
	}
	return &Schema{root: root}, nil
}

func escapePointer(s string) string {
	return strings.ReplaceAll(strings.ReplaceAll(s, "~", "~0"), "/", "~1")
}

// ref compiles the node a local reference points at, once.
func (c *compiler) ref(ref string) (*node, error) {
	if n, ok := c.cache[ref]; ok {
		return n, nil
	}
	if ref != "#" && !strings.HasPrefix(ref, "#/") {
		return nil, fmt.Errorf("schema: $ref %q is not a local reference", ref)
	}
	raw := c.doc
	if ref != "#" {
		for _, part := range strings.Split(ref[2:], "/") {
			part = strings.ReplaceAll(strings.ReplaceAll(part, "~1", "/"), "~0", "~")
			m, ok := raw.(map[string]any)
			if !ok {
				return nil, fmt.Errorf("schema: $ref %q points at nothing", ref)
			}
			if raw, ok = m[part]; !ok {
				return nil, fmt.Errorf("schema: $ref %q points at nothing", ref)
			}
		}
	}
	n := &node{}
	c.cache[ref] = n // set before the build, so a recursive schema ends
	if err := c.build(n, raw, ref); err != nil {
		return nil, err
	}
	return n, nil
}

func (c *compiler) sub(raw any, where string) (*node, error) {
	n := &node{}
	if err := c.build(n, raw, where); err != nil {
		return nil, err
	}
	return n, nil
}

func (c *compiler) subList(raw any, where string) ([]*node, error) {
	arr, ok := raw.([]any)
	if !ok || len(arr) == 0 {
		return nil, fmt.Errorf("schema: %s must be a non-empty array", where)
	}
	out := make([]*node, 0, len(arr))
	for i, r := range arr {
		n, err := c.sub(r, fmt.Sprintf("%s/%d", where, i))
		if err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, nil
}

func (c *compiler) build(n *node, raw any, where string) error {
	m, ok := raw.(map[string]any)
	if !ok {
		return fmt.Errorf("schema: %s must be an object", where)
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		v := m[k]
		var err error
		switch k {
		case "type":
			err = buildType(n, v, where)
		case "properties":
			pm, ok := v.(map[string]any)
			if !ok {
				return fmt.Errorf("schema: %s/properties must be an object", where)
			}
			n.props = map[string]*node{}
			for pk, pv := range pm {
				if n.props[pk], err = c.sub(pv, where+"/properties/"+escapePointer(pk)); err != nil {
					return err
				}
			}
		case "required":
			arr, ok := v.([]any)
			if !ok {
				return fmt.Errorf("schema: %s/required must be an array", where)
			}
			for _, r := range arr {
				s, ok := r.(string)
				if !ok {
					return fmt.Errorf("schema: %s/required must hold strings", where)
				}
				n.required = append(n.required, s)
			}
		case "additionalProperties":
			n.addSet = true
			if b, ok := v.(bool); ok {
				n.addAllowed = b
			} else if n.addSchema, err = c.sub(v, where+"/additionalProperties"); err != nil {
				return err
			}
		case "items":
			if n.items, err = c.sub(v, where+"/items"); err != nil {
				return err
			}
		case "minItems":
			n.minItems, err = intKeyword(v, where, k)
		case "minLength":
			n.minLength, err = intKeyword(v, where, k)
		case "uniqueItems":
			b, ok := v.(bool)
			if !ok {
				return fmt.Errorf("schema: %s/uniqueItems must be a boolean", where)
			}
			n.unique = b
		case "minimum":
			num, ok := v.(json.Number)
			if !ok {
				return fmt.Errorf("schema: %s/minimum must be a number", where)
			}
			r, ok := new(big.Rat).SetString(num.String())
			if !ok {
				return fmt.Errorf("schema: %s/minimum is not a number", where)
			}
			n.minimum = r
		case "maximum":
			num, ok := v.(json.Number)
			if !ok {
				return fmt.Errorf("schema: %s/maximum must be a number", where)
			}
			r, ok := new(big.Rat).SetString(num.String())
			if !ok {
				return fmt.Errorf("schema: %s/maximum is not a number", where)
			}
			n.maximum = r
		case "enum":
			arr, ok := v.([]any)
			if !ok || len(arr) == 0 {
				return fmt.Errorf("schema: %s/enum must be a non-empty array", where)
			}
			n.enum = arr
		case "const":
			n.hasConst, n.konst = true, v
		case "pattern":
			s, ok := v.(string)
			if !ok {
				return fmt.Errorf("schema: %s/pattern must be a string", where)
			}
			if n.pattern, err = regexp.Compile(s); err != nil {
				return fmt.Errorf("schema: %s/pattern: %w", where, err)
			}
		case "oneOf":
			n.oneOf, err = c.subList(v, where+"/oneOf")
		case "anyOf":
			n.anyOf, err = c.subList(v, where+"/anyOf")
		case "allOf":
			n.allOf, err = c.subList(v, where+"/allOf")
		case "$ref":
			s, ok := v.(string)
			if !ok {
				return fmt.Errorf("schema: %s/$ref must be a string", where)
			}
			n.ref = s
			// Lazy: the target is built when first named, and a cycle ends
			// at the cache. A chain of bare $refs that loops is refused below.
			if n.target, err = c.ref(s); err != nil {
				return err
			}
		case "definitions":
			if _, ok := v.(map[string]any); !ok {
				return fmt.Errorf("schema: %s/definitions must be an object", where)
			}
		case "visibleContent":
			b, ok := v.(bool)
			if !ok {
				return fmt.Errorf("schema: %s/visibleContent must be a boolean", where)
			}
			n.visible = b
			if b && !(len(m) > 0 && isStringType(m["type"])) {
				return fmt.Errorf("schema: %s uses visibleContent, which needs \"type\": \"string\" in the same schema", where)
			}
		default:
			if !annotations[k] {
				return fmt.Errorf("schema: %s uses the keyword %q, which is not supported", where, k)
			}
		}
		if err != nil {
			return err
		}
	}
	return nil
}

func isStringType(v any) bool {
	s, ok := v.(string)
	return ok && s == "string"
}

func buildType(n *node, v any, where string) error {
	valid := map[string]bool{"string": true, "number": true, "integer": true, "boolean": true, "object": true, "array": true, "null": true}
	var names []any
	switch t := v.(type) {
	case string:
		names = []any{t}
	case []any:
		names = t
	default:
		return fmt.Errorf("schema: %s/type must be a name or a list", where)
	}
	for _, x := range names {
		s, ok := x.(string)
		if !ok || !valid[s] {
			return fmt.Errorf("schema: %s/type has an unknown type name", where)
		}
		n.types = append(n.types, s)
	}
	return nil
}

func intKeyword(v any, where, k string) (*int, error) {
	num, ok := v.(json.Number)
	if !ok {
		return nil, fmt.Errorf("schema: %s/%s must be a whole number", where, k)
	}
	i, err := num.Int64()
	if err != nil || i < 0 || i > 1<<30 {
		return nil, fmt.Errorf("schema: %s/%s must be a whole number", where, k)
	}
	x := int(i)
	return &x, nil
}

// noProgressCycle refuses a loop of $ref, allOf, anyOf and oneOf edges. Such
// a loop asks the same question of the same value for ever. A loop through
// properties or items moves down into the value, so it is fine. Every loop
// passes a $ref target, and every target is in the cache.
func (c *compiler) noProgressCycle() error {
	const (
		visiting = 1
		done     = 2
	)
	state := map[*node]int{}
	var walk func(n *node) bool
	walk = func(n *node) bool {
		switch state[n] {
		case visiting:
			return true
		case done:
			return false
		}
		state[n] = visiting
		next := []*node{}
		if n.target != nil {
			next = append(next, n.target)
		}
		next = append(next, n.allOf...)
		next = append(next, n.anyOf...)
		next = append(next, n.oneOf...)
		for _, m := range next {
			if walk(m) {
				return true
			}
		}
		state[n] = done
		return false
	}
	refs := make([]string, 0, len(c.cache))
	for r := range c.cache {
		refs = append(refs, r)
	}
	sort.Strings(refs)
	for _, r := range refs {
		if walk(c.cache[r]) {
			return fmt.Errorf("schema: $ref %q loops with no progress", r)
		}
	}
	return nil
}

type collector struct {
	errs []string
}

func (c *collector) full() bool { return len(c.errs) >= maxErrors }

func (c *collector) add(path, msg string) {
	if c.full() {
		return
	}
	if path == "" {
		path = "/"
	}
	c.errs = append(c.errs, path+": "+msg)
}

// Validate returns the problems found, at most 100. Each is
// "<JSON pointer>: <problem>". An error never holds the checked value.
func (s *Schema) Validate(v any) []string {
	c := &collector{}
	validate(c, s.root, v, "", 0)
	return c.errs
}

func validate(c *collector, n *node, v any, path string, depth int) {
	if c.full() {
		return
	}
	if depth > maxRecursion {
		c.add(path, "schema recursion is too deep")
		return
	}
	if n.target != nil {
		validate(c, n.target, v, path, depth+1)
	}
	if len(n.types) > 0 && !typeMatches(n.types, v) {
		c.add(path, "has the wrong type, want "+strings.Join(n.types, " or "))
		return
	}
	if n.hasConst && !equal(n.konst, v) {
		c.add(path, "does not equal the required constant")
	}
	if n.enum != nil {
		found := false
		for _, e := range n.enum {
			if equal(e, v) {
				found = true
				break
			}
		}
		if !found {
			c.add(path, "is not one of the allowed values")
		}
	}
	switch x := v.(type) {
	case map[string]any:
		validateObject(c, n, x, path, depth)
	case []any:
		validateArray(c, n, x, path, depth)
	case string:
		if n.minLength != nil && utf8.RuneCountInString(x) < *n.minLength {
			c.add(path, fmt.Sprintf("is shorter than %d characters", *n.minLength))
		}
		if n.pattern != nil && !n.pattern.MatchString(x) {
			c.add(path, "does not match the required pattern")
		}
		if n.visible && !hasVisible(x) {
			c.add(path, "has no visible content")
		}
	case json.Number:
		if n.minimum != nil {
			if r, ok := new(big.Rat).SetString(x.String()); ok && r.Cmp(n.minimum) < 0 {
				c.add(path, "is below the minimum")
			}
		}
		if n.maximum != nil {
			if r, ok := new(big.Rat).SetString(x.String()); ok && r.Cmp(n.maximum) > 0 {
				c.add(path, "is above the maximum")
			}
		}
	}
	validateCombiners(c, n, v, path, depth)
}

func validateObject(c *collector, n *node, m map[string]any, path string, depth int) {
	for _, r := range n.required {
		if _, ok := m[r]; !ok {
			c.add(path, fmt.Sprintf("is missing the required key %q", r))
		}
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		child := path + "/" + escapePointer(k)
		if p, ok := n.props[k]; ok {
			validate(c, p, m[k], child, depth+1)
			continue
		}
		if !n.addSet {
			continue
		}
		if n.addSchema != nil {
			validate(c, n.addSchema, m[k], child, depth+1)
		} else if !n.addAllowed {
			c.add(child, "is a key the schema does not allow")
		}
	}
}

func validateArray(c *collector, n *node, a []any, path string, depth int) {
	if n.minItems != nil && len(a) < *n.minItems {
		c.add(path, fmt.Sprintf("has fewer than %d items", *n.minItems))
	}
	if n.items != nil {
		for i, it := range a {
			validate(c, n.items, it, fmt.Sprintf("%s/%d", path, i), depth+1)
		}
	}
	if n.unique {
		seen := make(map[string]int, len(a))
		for i, it := range a {
			key := canonical(it)
			if first, dup := seen[key]; dup {
				c.add(path, fmt.Sprintf("has items %d and %d that are equal", first, i))
				break
			}
			seen[key] = i
		}
	}
}

// canonical is a text form of a value: sorted keys, numbers by value. Equal
// values give equal text, so a map finds duplicates in linear time.
func canonical(v any) string {
	var b strings.Builder
	writeCanonical(&b, v)
	return b.String()
}

func writeCanonical(b *strings.Builder, v any) {
	switch x := v.(type) {
	case nil:
		b.WriteString("n")
	case bool:
		if x {
			b.WriteString("t")
		} else {
			b.WriteString("f")
		}
	case string:
		b.WriteString(strconv.Quote(x))
	case json.Number:
		if r, ok := new(big.Rat).SetString(x.String()); ok {
			b.WriteString("#" + r.String())
		} else {
			b.WriteString("?" + x.String())
		}
	case []any:
		b.WriteByte('[')
		for _, it := range x {
			writeCanonical(b, it)
			b.WriteByte(',')
		}
		b.WriteByte(']')
	case map[string]any:
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		b.WriteByte('{')
		for _, k := range keys {
			b.WriteString(strconv.Quote(k))
			b.WriteByte(':')
			writeCanonical(b, x[k])
			b.WriteByte(',')
		}
		b.WriteByte('}')
	}
}

func validateCombiners(c *collector, n *node, v any, path string, depth int) {
	for _, s := range n.allOf {
		validate(c, s, v, path, depth+1)
	}
	if n.anyOf != nil && matches(n.anyOf, v, path, depth) == 0 {
		c.add(path, "matches none of the anyOf schemas")
	}
	if n.oneOf != nil {
		if k := matches(n.oneOf, v, path, depth); k != 1 {
			c.add(path, fmt.Sprintf("matches %d of the oneOf schemas, want exactly 1", k))
		}
	}
}

func matches(list []*node, v any, path string, depth int) int {
	k := 0
	for _, s := range list {
		sub := &collector{}
		validate(sub, s, v, path, depth+1)
		if len(sub.errs) == 0 {
			k++
		}
	}
	return k
}

func typeMatches(types []string, v any) bool {
	for _, t := range types {
		switch t {
		case "null":
			if v == nil {
				return true
			}
		case "boolean":
			if _, ok := v.(bool); ok {
				return true
			}
		case "string":
			if _, ok := v.(string); ok {
				return true
			}
		case "object":
			if _, ok := v.(map[string]any); ok {
				return true
			}
		case "array":
			if _, ok := v.([]any); ok {
				return true
			}
		case "number", "integer":
			num, ok := v.(json.Number)
			if !ok {
				continue
			}
			r, ok := new(big.Rat).SetString(num.String())
			if ok && (t == "number" || r.IsInt()) {
				return true
			}
		}
	}
	return false
}

// equal compares two decoded values. Numbers compare by value.
func equal(a, b any) bool {
	switch x := a.(type) {
	case json.Number:
		y, ok := b.(json.Number)
		if !ok {
			return false
		}
		rx, ok1 := new(big.Rat).SetString(x.String())
		ry, ok2 := new(big.Rat).SetString(y.String())
		return ok1 && ok2 && rx.Cmp(ry) == 0
	case map[string]any:
		y, ok := b.(map[string]any)
		if !ok || len(x) != len(y) {
			return false
		}
		for k, xv := range x {
			yv, ok := y[k]
			if !ok || !equal(xv, yv) {
				return false
			}
		}
		return true
	case []any:
		y, ok := b.([]any)
		if !ok || len(x) != len(y) {
			return false
		}
		for i := range x {
			if !equal(x[i], y[i]) {
				return false
			}
		}
		return true
	default:
		return a == b
	}
}

// hasVisible reports whether s has a rune a reader can see: not white space,
// not a control or format rune, not an ignorable code point, not a variation
// selector.
func hasVisible(s string) bool {
	for _, r := range s {
		if unicode.Is(unicode.White_Space, r) || unicode.Is(unicode.Cc, r) ||
			unicode.Is(unicode.Cf, r) || unicode.Is(unicode.Other_Default_Ignorable_Code_Point, r) ||
			unicode.Is(unicode.Variation_Selector, r) {
			continue
		}
		return true
	}
	return false
}
