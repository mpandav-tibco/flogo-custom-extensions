package evaluator

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"sync"

	"github.com/mpandav-tibco/flogo-custom-extensions/activity/ruleengine/engine/parser"
)

// ─────────────────────────────────────────────────────────────────────────────
// Condition DSL
//
// A small, dependency-free expression language used by the `condition:` field of
// a rule. It is evaluated read-only against a parsed Document scope and returns
// whether the condition matched.
//
// Grammar (precedence low → high):
//
//	expr      := orExpr
//	orExpr    := andExpr ( OR andExpr )*
//	andExpr   := predicate ( AND predicate )*
//	predicate := '(' expr ')'
//	           | operand '==' operand        | operand '!=' operand
//	           | operand ('>'|'<'|'>='|'<=') operand
//	           | operand 'is' ['not'] 'null'
//	           | operand ['not'|'NOT'] 'in' list
//	           | operand ('matches'|'match') regex
//	           | operand ('not' ('matches'|'match') | 'does' 'not' 'match') regex
//	           | 'any' 'field' 'contains' list
//	operand   := 'length' '(' field ')'
//	           | field [ 'domain' ]
//	           | string | number
//	list      := '[' ( string | number ) ( ',' (string|number) )* ']'
//	regex     := /pattern/
//
// Keywords (AND, OR, IS, NOT, IN, NULL, MATCHES, MATCH, DOES, CONTAINS, DOMAIN,
// ANY, FIELD, LENGTH) are matched case-insensitively. Field names are bare
// identifiers; string literals are always double-quoted, so the two never clash.
// ─────────────────────────────────────────────────────────────────────────────

// EvalConditionDSL parses and evaluates a condition expression against a scope
// object from doc. Parsed ASTs are cached per expression string.
func EvalConditionDSL(expr string, doc parser.Document, scope interface{}) (bool, error) {
	ast, err := parseCondition(expr)
	if err != nil {
		return false, err
	}
	return ast.eval(&condCtx{doc: doc, scope: scope})
}

// ── Lexer ─────────────────────────────────────────────────────────────────────

type tokKind int

const (
	tkWord tokKind = iota
	tkString
	tkRegex
	tkNumber
	tkOp // == != > < >= <=
	tkLBracket
	tkRBracket
	tkLParen
	tkRParen
	tkComma
	tkEOF
)

type condTok struct {
	kind tokKind
	val  string
}

func lexCondition(s string) ([]condTok, error) {
	var toks []condTok
	runes := []rune(s)
	i := 0
	n := len(runes)
	for i < n {
		c := runes[i]
		switch {
		case c == ' ' || c == '\t' || c == '\n' || c == '\r':
			i++
		case c == '"':
			i++
			start := i
			var sb strings.Builder
			for i < n && runes[i] != '"' {
				if runes[i] == '\\' && i+1 < n {
					sb.WriteRune(runes[i+1])
					i += 2
					continue
				}
				sb.WriteRune(runes[i])
				i++
			}
			if i >= n {
				return nil, fmt.Errorf("unterminated string literal at offset %d", start)
			}
			i++ // closing quote
			toks = append(toks, condTok{tkString, sb.String()})
		case c == '/':
			i++
			start := i
			var sb strings.Builder
			for i < n && runes[i] != '/' {
				if runes[i] == '\\' && i+1 < n {
					// keep the escape, but normalise \/ → / for the Go regexp engine
					if runes[i+1] == '/' {
						sb.WriteRune('/')
					} else {
						sb.WriteRune('\\')
						sb.WriteRune(runes[i+1])
					}
					i += 2
					continue
				}
				sb.WriteRune(runes[i])
				i++
			}
			if i >= n {
				return nil, fmt.Errorf("unterminated regex literal at offset %d", start)
			}
			i++ // closing slash
			toks = append(toks, condTok{tkRegex, sb.String()})
		case c >= '0' && c <= '9':
			start := i
			for i < n && ((runes[i] >= '0' && runes[i] <= '9') || runes[i] == '.') {
				i++
			}
			toks = append(toks, condTok{tkNumber, string(runes[start:i])})
		case isIdentStart(c):
			start := i
			for i < n && isIdentPart(runes[i]) {
				i++
			}
			toks = append(toks, condTok{tkWord, string(runes[start:i])})
		case c == '[':
			toks = append(toks, condTok{tkLBracket, "["})
			i++
		case c == ']':
			toks = append(toks, condTok{tkRBracket, "]"})
			i++
		case c == '(':
			toks = append(toks, condTok{tkLParen, "("})
			i++
		case c == ')':
			toks = append(toks, condTok{tkRParen, ")"})
			i++
		case c == ',':
			toks = append(toks, condTok{tkComma, ","})
			i++
		case c == '=':
			if i+1 < n && runes[i+1] == '=' {
				toks = append(toks, condTok{tkOp, "=="})
				i += 2
			} else {
				return nil, fmt.Errorf("unexpected '=' at offset %d (use '==')", i)
			}
		case c == '!':
			if i+1 < n && runes[i+1] == '=' {
				toks = append(toks, condTok{tkOp, "!="})
				i += 2
			} else {
				return nil, fmt.Errorf("unexpected '!' at offset %d (use '!=')", i)
			}
		case c == '>' || c == '<':
			if i+1 < n && runes[i+1] == '=' {
				toks = append(toks, condTok{tkOp, string(c) + "="})
				i += 2
			} else {
				toks = append(toks, condTok{tkOp, string(c)})
				i++
			}
		default:
			return nil, fmt.Errorf("unexpected character %q at offset %d", string(c), i)
		}
	}
	toks = append(toks, condTok{tkEOF, ""})
	return toks, nil
}

func isIdentStart(c rune) bool {
	return c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

func isIdentPart(c rune) bool {
	return isIdentStart(c) || (c >= '0' && c <= '9')
}

// ── AST ───────────────────────────────────────────────────────────────────────

type condNode interface {
	eval(ctx *condCtx) (bool, error)
}

type condCtx struct {
	doc   parser.Document
	scope interface{}
}

type orNode struct{ left, right condNode }

func (o *orNode) eval(ctx *condCtx) (bool, error) {
	l, err := o.left.eval(ctx)
	if err != nil {
		return false, err
	}
	if l {
		return true, nil // short-circuit
	}
	return o.right.eval(ctx)
}

type andNode struct{ left, right condNode }

func (a *andNode) eval(ctx *condCtx) (bool, error) {
	l, err := a.left.eval(ctx)
	if err != nil {
		return false, err
	}
	if !l {
		return false, nil // short-circuit
	}
	return a.right.eval(ctx)
}

// operand kinds
type opKind int

const (
	opField opKind = iota
	opLength
	opDomain
	opString
	opNumber
	opAnyField
)

type operand struct {
	kind opKind
	name string // field name (field/length/domain)
	str  string // string literal
	num  float64
}

type compareNode struct {
	left  operand
	op    string
	right operand
}

func (c *compareNode) eval(ctx *condCtx) (bool, error) {
	switch c.op {
	case "==", "!=":
		ls := ctx.operandString(c.left)
		rs := ctx.operandString(c.right)
		eq := ls == rs
		if c.op == "==" {
			return eq, nil
		}
		return !eq, nil
	case ">", "<", ">=", "<=":
		ln, err := ctx.operandNumber(c.left)
		if err != nil {
			return false, err
		}
		rn, err := ctx.operandNumber(c.right)
		if err != nil {
			return false, err
		}
		switch c.op {
		case ">":
			return ln > rn, nil
		case "<":
			return ln < rn, nil
		case ">=":
			return ln >= rn, nil
		default:
			return ln <= rn, nil
		}
	}
	return false, fmt.Errorf("unsupported operator %q", c.op)
}

type isNullNode struct {
	operand operand
	negate  bool // true => "is not null"
}

func (i *isNullNode) eval(ctx *condCtx) (bool, error) {
	val, found := ctx.doc.ResolvePath(ctx.scope, i.operand.name)
	isNull := !found || val == nil
	if i.negate {
		return !isNull, nil
	}
	return isNull, nil
}

type inNode struct {
	operand operand
	list    []string
	negate  bool
}

func (n *inNode) eval(ctx *condCtx) (bool, error) {
	v := ctx.operandString(n.operand)
	found := false
	for _, item := range n.list {
		if item == v {
			found = true
			break
		}
	}
	if n.negate {
		return !found, nil
	}
	return found, nil
}

type regexNode struct {
	operand operand
	re      *regexp.Regexp
	negate  bool
}

func (n *regexNode) eval(ctx *condCtx) (bool, error) {
	v := ctx.operandString(n.operand)
	m := n.re.MatchString(v)
	if n.negate {
		return !m, nil
	}
	return m, nil
}

// containsNode implements `any field contains [list]`: true when any string leaf
// anywhere in the scope contains (case-insensitively) any listed substring.
type containsNode struct {
	list []string
}

func (n *containsNode) eval(ctx *condCtx) (bool, error) {
	lowered := make([]string, len(n.list))
	for i, s := range n.list {
		lowered[i] = strings.ToLower(s)
	}
	matched := false
	walkStrings(ctx.scope, func(s string) bool {
		ls := strings.ToLower(s)
		for _, sub := range lowered {
			if sub != "" && strings.Contains(ls, sub) {
				matched = true
				return false // stop walking
			}
		}
		return true
	})
	return matched, nil
}

// ── operand resolution ────────────────────────────────────────────────────────

func (ctx *condCtx) operandString(o operand) string {
	switch o.kind {
	case opString:
		return o.str
	case opNumber:
		return formatNumber(o.num)
	case opField:
		val, _ := ctx.doc.ResolvePath(ctx.scope, o.name)
		return valueToString(val)
	case opLength:
		val, _ := ctx.doc.ResolvePath(ctx.scope, o.name)
		return strconv.Itoa(valueLength(val))
	case opDomain:
		val, _ := ctx.doc.ResolvePath(ctx.scope, o.name)
		s := valueToString(val)
		if at := strings.LastIndex(s, "@"); at >= 0 {
			return s[at+1:]
		}
		return ""
	}
	return ""
}

func (ctx *condCtx) operandNumber(o operand) (float64, error) {
	switch o.kind {
	case opNumber:
		return o.num, nil
	case opLength:
		val, _ := ctx.doc.ResolvePath(ctx.scope, o.name)
		return float64(valueLength(val)), nil
	case opField:
		val, _ := ctx.doc.ResolvePath(ctx.scope, o.name)
		switch v := val.(type) {
		case float64:
			return v, nil
		case int:
			return float64(v), nil
		case string:
			f, err := strconv.ParseFloat(strings.TrimSpace(v), 64)
			if err != nil {
				return 0, fmt.Errorf("field %q is not numeric: %q", o.name, v)
			}
			return f, nil
		}
		return 0, fmt.Errorf("field %q is not numeric", o.name)
	case opString:
		f, err := strconv.ParseFloat(strings.TrimSpace(o.str), 64)
		if err != nil {
			return 0, fmt.Errorf("value %q is not numeric", o.str)
		}
		return f, nil
	}
	return 0, fmt.Errorf("operand is not numeric")
}

func valueToString(val interface{}) string {
	if val == nil {
		return ""
	}
	switch v := val.(type) {
	case string:
		return v
	case bool:
		if v {
			return "true"
		}
		return "false"
	case float64:
		return formatNumber(v)
	case int:
		return strconv.Itoa(v)
	}
	return fmt.Sprintf("%v", val)
}

func valueLength(val interface{}) int {
	switch v := val.(type) {
	case string:
		return len(v)
	case []interface{}:
		return len(v)
	case map[string]interface{}:
		return len(v)
	case nil:
		return 0
	}
	return len(fmt.Sprintf("%v", val))
}

func formatNumber(f float64) string {
	if f == float64(int64(f)) {
		return strconv.FormatInt(int64(f), 10)
	}
	return strconv.FormatFloat(f, 'g', -1, 64)
}

// walkStrings recursively visits every string leaf in v, calling fn for each.
// Walking stops early when fn returns false.
func walkStrings(v interface{}, fn func(string) bool) bool {
	switch t := v.(type) {
	case string:
		return fn(t)
	case map[string]interface{}:
		for _, val := range t {
			if !walkStrings(val, fn) {
				return false
			}
		}
	case []interface{}:
		for _, val := range t {
			if !walkStrings(val, fn) {
				return false
			}
		}
	}
	return true
}

// ── Parser (recursive descent) ────────────────────────────────────────────────

var condASTCache sync.Map // expr string → condNode (or error)

func parseCondition(expr string) (condNode, error) {
	if cached, ok := condASTCache.Load(expr); ok {
		switch c := cached.(type) {
		case condNode:
			return c, nil
		case error:
			return nil, c
		}
	}
	node, err := parseConditionUncached(expr)
	if err != nil {
		condASTCache.Store(expr, err)
		return nil, err
	}
	condASTCache.Store(expr, node)
	return node, nil
}

func parseConditionUncached(expr string) (condNode, error) {
	toks, err := lexCondition(expr)
	if err != nil {
		return nil, fmt.Errorf("condition lex error: %w", err)
	}
	p := &condParser{toks: toks}
	node, err := p.parseOr()
	if err != nil {
		return nil, err
	}
	if p.peek().kind != tkEOF {
		return nil, fmt.Errorf("condition parse error: unexpected trailing token %q", p.peek().val)
	}
	return node, nil
}

type condParser struct {
	toks []condTok
	pos  int
}

func (p *condParser) peek() condTok { return p.toks[p.pos] }
func (p *condParser) next() condTok {
	t := p.toks[p.pos]
	if p.pos < len(p.toks)-1 {
		p.pos++
	}
	return t
}

func (p *condParser) isKeyword(t condTok, kw string) bool {
	return t.kind == tkWord && strings.EqualFold(t.val, kw)
}

func (p *condParser) parseOr() (condNode, error) {
	left, err := p.parseAnd()
	if err != nil {
		return nil, err
	}
	for p.isKeyword(p.peek(), "or") {
		p.next()
		right, err := p.parseAnd()
		if err != nil {
			return nil, err
		}
		left = &orNode{left, right}
	}
	return left, nil
}

func (p *condParser) parseAnd() (condNode, error) {
	left, err := p.parsePredicate()
	if err != nil {
		return nil, err
	}
	for p.isKeyword(p.peek(), "and") {
		p.next()
		right, err := p.parsePredicate()
		if err != nil {
			return nil, err
		}
		left = &andNode{left, right}
	}
	return left, nil
}

func (p *condParser) parsePredicate() (condNode, error) {
	if p.peek().kind == tkLParen {
		p.next()
		node, err := p.parseOr()
		if err != nil {
			return nil, err
		}
		if p.peek().kind != tkRParen {
			return nil, fmt.Errorf("condition parse error: expected ')'")
		}
		p.next()
		return node, nil
	}

	left, err := p.parseOperand()
	if err != nil {
		return nil, err
	}

	// `any field contains [...]`
	if left.kind == opAnyField {
		if !p.isKeyword(p.peek(), "contains") {
			return nil, fmt.Errorf("condition parse error: expected 'contains' after 'any field'")
		}
		p.next()
		list, err := p.parseList()
		if err != nil {
			return nil, err
		}
		return &containsNode{list: list}, nil
	}

	t := p.peek()
	switch {
	case t.kind == tkOp:
		p.next()
		right, err := p.parseOperand()
		if err != nil {
			return nil, err
		}
		return &compareNode{left: left, op: t.val, right: right}, nil

	case p.isKeyword(t, "is"):
		p.next()
		negate := false
		if p.isKeyword(p.peek(), "not") {
			p.next()
			negate = true
		}
		if !p.isKeyword(p.peek(), "null") {
			return nil, fmt.Errorf("condition parse error: expected 'null' after 'is'")
		}
		p.next()
		return &isNullNode{operand: left, negate: negate}, nil

	case p.isKeyword(t, "in"):
		p.next()
		list, err := p.parseList()
		if err != nil {
			return nil, err
		}
		return &inNode{operand: left, list: list, negate: false}, nil

	case p.isKeyword(t, "not"):
		p.next()
		if p.isKeyword(p.peek(), "in") {
			p.next()
			list, err := p.parseList()
			if err != nil {
				return nil, err
			}
			return &inNode{operand: left, list: list, negate: true}, nil
		}
		if p.isKeyword(p.peek(), "matches") || p.isKeyword(p.peek(), "match") {
			p.next()
			re, err := p.parseRegex()
			if err != nil {
				return nil, err
			}
			return &regexNode{operand: left, re: re, negate: true}, nil
		}
		return nil, fmt.Errorf("condition parse error: expected 'in' or 'matches' after 'not'")

	case p.isKeyword(t, "does"):
		p.next()
		if !p.isKeyword(p.peek(), "not") {
			return nil, fmt.Errorf("condition parse error: expected 'not' after 'does'")
		}
		p.next()
		if !p.isKeyword(p.peek(), "match") && !p.isKeyword(p.peek(), "matches") {
			return nil, fmt.Errorf("condition parse error: expected 'match' after 'does not'")
		}
		p.next()
		re, err := p.parseRegex()
		if err != nil {
			return nil, err
		}
		return &regexNode{operand: left, re: re, negate: true}, nil

	case p.isKeyword(t, "matches") || p.isKeyword(t, "match"):
		p.next()
		re, err := p.parseRegex()
		if err != nil {
			return nil, err
		}
		return &regexNode{operand: left, re: re, negate: false}, nil
	}

	return nil, fmt.Errorf("condition parse error: expected an operator after operand, got %q", t.val)
}

func (p *condParser) parseOperand() (operand, error) {
	t := p.peek()
	switch t.kind {
	case tkString:
		p.next()
		return operand{kind: opString, str: t.val}, nil
	case tkNumber:
		p.next()
		f, err := strconv.ParseFloat(t.val, 64)
		if err != nil {
			return operand{}, fmt.Errorf("invalid number %q", t.val)
		}
		return operand{kind: opNumber, num: f}, nil
	case tkWord:
		// keyword-led operands
		if strings.EqualFold(t.val, "length") {
			p.next()
			if p.peek().kind != tkLParen {
				return operand{}, fmt.Errorf("condition parse error: expected '(' after length")
			}
			p.next()
			fld := p.peek()
			if fld.kind != tkWord {
				return operand{}, fmt.Errorf("condition parse error: expected field name in length(...)")
			}
			p.next()
			if p.peek().kind != tkRParen {
				return operand{}, fmt.Errorf("condition parse error: expected ')' after length(%s", fld.val)
			}
			p.next()
			return operand{kind: opLength, name: fld.val}, nil
		}
		if strings.EqualFold(t.val, "any") {
			p.next()
			if !p.isKeyword(p.peek(), "field") {
				return operand{}, fmt.Errorf("condition parse error: expected 'field' after 'any'")
			}
			p.next()
			return operand{kind: opAnyField}, nil
		}
		// bare field, with optional postfix `domain`
		p.next()
		if p.isKeyword(p.peek(), "domain") {
			p.next()
			return operand{kind: opDomain, name: t.val}, nil
		}
		return operand{kind: opField, name: t.val}, nil
	}
	return operand{}, fmt.Errorf("condition parse error: expected an operand, got %q", t.val)
}

func (p *condParser) parseList() ([]string, error) {
	if p.peek().kind != tkLBracket {
		return nil, fmt.Errorf("condition parse error: expected '[' to start a list")
	}
	p.next()
	var items []string
	for p.peek().kind != tkRBracket {
		t := p.peek()
		switch t.kind {
		case tkString:
			items = append(items, t.val)
			p.next()
		case tkNumber:
			items = append(items, t.val)
			p.next()
		default:
			return nil, fmt.Errorf("condition parse error: expected string/number in list, got %q", t.val)
		}
		if p.peek().kind == tkComma {
			p.next()
		}
	}
	p.next() // ]
	return items, nil
}

func (p *condParser) parseRegex() (*regexp.Regexp, error) {
	t := p.peek()
	if t.kind != tkRegex {
		return nil, fmt.Errorf("condition parse error: expected /regex/, got %q", t.val)
	}
	p.next()
	re, err := getCompiledRegex(t.val)
	if err != nil {
		return nil, fmt.Errorf("invalid regex /%s/: %w", t.val, err)
	}
	return re, nil
}

// getCompiledRegex compiles (and caches) a regex pattern, reusing the shared
// compiledRegexps cache declared in matcher.go.
func getCompiledRegex(pattern string) (*regexp.Regexp, error) {
	if cached, ok := compiledRegexps.Load(pattern); ok {
		return cached.(*regexp.Regexp), nil
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil, err
	}
	compiledRegexps.Store(pattern, re)
	return re, nil
}
