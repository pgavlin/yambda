package yambda

import (
	"fmt"
	"math"
	"strconv"

	"github.com/goccy/go-yaml/ast"
	"github.com/goccy/go-yaml/parser"
	"github.com/goccy/go-yaml/token"
	"github.com/pgavlin/loom"
)

// mapValues returns a slice of MappingValueNodes from a MappingNode or MappingValueNode.
func mapValues(v ast.Node) []*ast.MappingValueNode {
	switch v := v.(type) {
	case *ast.MappingNode:
		return v.Values
	case *ast.MappingValueNode:
		return []*ast.MappingValueNode{v}
	default:
		panic(fmt.Errorf("expected a MappingNode or MappingValueNode, got a %T", v))
	}
}

type dictionary struct {
	pairs []*loom.Pair
}

func (d dictionary) MarshalSExp() loom.SExpression {
	var head loom.Value
	for i := len(d.pairs) - 1; i >= 0; i-- {
		head = loom.Cons(d.pairs[i], head)
	}
	return loom.Cons(loom.Symbol("dictionary"), head)
}

func dictionaryPred(args loom.Vector) loom.Value {
	if len(args) != 1 {
		return loom.Boolean(false)
	}
	_, ok := args[0].(dictionary)
	return loom.Boolean(ok)
}

func dictionaryConstructor(args loom.Vector) loom.Value {
	d := dictionary{pairs: make([]*loom.Pair, 0, len(args))}
	for _, v := range args {
		switch v := v.(type) {
		case dictionary:
			d.pairs = append(d.pairs, v.pairs...)
		case *loom.Pair:
			d.pairs = append(d.pairs, v)
		case loom.Boolean:
			if !v {
				continue
			}
			panic("arguments to dictionary must be dictionaries, pairs, or false")
		default:
			panic("arguments to dictionary must be dictionaries, pairs, or false")
		}
	}
	return d
}

func dictionaryRef(args loom.Vector) loom.Value {
	if len(args) != 2 {
		panic("dictionary-ref expects 2 arguments")
	}
	d, ok := args[0].(dictionary)
	if !ok {
		panic("the first argument to dictionary-ref must be a dictionary")
	}
	needle := args[1]
	for _, kvp := range d.pairs {
		if loom.Eq(loom.Vector{kvp.Car(), needle}).(loom.Boolean) {
			return kvp.Cdr().(*loom.Pair).Car()
		}
	}
	return loom.Boolean(false)
}

type elide struct {
	v loom.Value
}

func (i elide) MarshalSExp() loom.SExpression {
	return loom.Cons(loom.Symbol("yaml-elide"), loom.Cons(i.v, nil))
}

func elideConstructor(args loom.Vector) loom.Value {
	if len(args) != 1 {
		panic("yaml-elide expects 1 argument")
	}
	return elide{v: args[0]}
}

const (
	quote   = loom.Symbol("quote")
	qq      = loom.Symbol("quasiquote")
	unquote = loom.Symbol("unquote")
	cdr     = loom.Symbol("cdr")
	cons    = loom.Symbol("cons")
	define  = loom.Symbol("define")
)

func list(exprs ...loom.Value) loom.Value {
	return loom.Vector(exprs).ToList()
}

func parse(node ast.Node, quoted bool) loom.Value {
	if node == nil {
		return nil
	}

	switch node := node.(type) {
	case *ast.DocumentNode:
		return parse(node.Body, quoted)
	case *ast.SequenceNode:
		head := loom.Value(nil)
		for i := len(node.Values) - 1; i >= 0; i-- {
			if tag, ok := node.Values[i].(*ast.TagNode); ok && tag.Start.Value[1:] == "." {
				head = parse(tag.Value, quoted)
			} else {
				head = loom.Cons(parse(node.Values[i], quoted), head)
			}
		}
		return head
	case *ast.BoolNode:
		return loom.Boolean(node.Value)
	case *ast.NullNode:
		return nil
	case *ast.InfinityNode:
		return loom.NewFloat(node.Value)
	case *ast.NanNode:
		return loom.NewFloat(math.NaN())
	case *ast.FloatNode:
		return loom.NewFloat(node.Value)
	case *ast.IntegerNode:
		if i, ok := node.Value.(uint64); ok {
			return loom.NewUint(i)
		}
		return loom.NewInt(node.Value.(int64))
	case *ast.StringNode:
		return loom.Symbol(node.Value)
	case *ast.LiteralNode:
		return parse(node.Value, quoted)
	case *ast.MappingNode, *ast.MappingValueNode:
		values := mapValues(node)
		dictionary := append(make([]loom.Value, 0, len(values)+1), loom.Symbol("dictionary"))
		for _, kvp := range values {
			key := loom.Symbol(kvp.Key.(*ast.StringNode).Value)

			if tag, ok := kvp.Value.(*ast.TagNode); ok && tag.Start.Value[1:] == "=" {
				dictionary = append(dictionary, list(qq, parse(tag.Value, quoted)))
			} else {
				value := parse(kvp.Value, quoted)
				dictionary = append(dictionary, list(qq, loom.Cons(key, loom.Cons(value, nil))))
			}
		}
		if quoted {
			return list(unquote, list(dictionary...))
		}
		return list(dictionary...)
	case *ast.AnchorNode:
		// (cdr (cons (define sym value) sym))
		sym := loom.Symbol(node.Name.(*ast.StringNode).Value)

		val := parse(node.Value, quoted)
		if !quoted {
			def := list(define, sym, val)
			if pair, ok := val.(*loom.Pair); ok {
				if lam, ok := pair.Car().(loom.Symbol); ok && lam == "lambda" {
					if formals, ok := pair.Cdr().(*loom.Pair); ok {
						switch rest := formals.Car().(type) {
						case loom.Symbol, *loom.Pair:
							def = loom.Cons(define, loom.Cons(loom.Cons(sym, rest), formals.Cdr()))
						default:
							// no match, leave as-is
						}
					}
				}
			}
			return list(cdr, list(cons, def, sym))
		}
		return list(unquote, list(cdr, list(cons, list(define, sym, list(qq, val)), sym)))
	case *ast.AliasNode:
		if node.Value == nil {
			return loom.Symbol("*")
		}
		sym := loom.Symbol(node.Value.(*ast.StringNode).Value)
		if quoted {
			return list(unquote, sym)
		}
		return sym
	case *ast.TagNode:
		tag := node.Start.Value[1:]
		switch tag {
		case ",":
			return list(unquote, parse(node.Value, false))
		case "`":
			return list(qq, parse(node.Value, true))
		case "'":
			return list(quote, parse(node.Value, false))
		case "#":
			return list(unquote, list(loom.Symbol("yaml-elide"), parse(node.Value, false)))
		case "\"", "str":
			switch value := parse(node.Value, quoted).(type) {
			case loom.Symbol:
				return loom.String(string(value))
			case loom.String:
				return value
			default:
				return loom.String(loom.EncodeToString(value))
			}
		case "char":
			switch value := parse(node.Value, quoted).(type) {
			case loom.Symbol:
				return loom.Character(value[0])
			case loom.String:
				return loom.Character(value[0])
			default:
				return loom.Character(loom.EncodeToString(value)[0])
			}

		}

		seq, ok := node.Value.(*ast.SequenceNode)
		if !ok {
			panic("tag value must be a sequence")
		}

		if tag == "vec" {
			vec := make(loom.Vector, len(seq.Values))
			for i, v := range seq.Values {
				vec[i] = parse(v, quoted)
			}
			return vec
		}

		head := loom.Value(nil)
		for i := len(seq.Values) - 1; i >= 0; i-- {
			head = loom.Cons(parse(seq.Values[i], quoted), head)
		}
		return loom.Cons(loom.Symbol(tag), head)
	default:
		panic(fmt.Errorf("unexpected YAML node of type %T", node))
	}
}

func Marshal(v loom.Value) (interface{}, bool) {
	if v == nil {
		return nil, true
	}

	switch v := v.(type) {
	case loom.Boolean:
		return bool(v), true
	case loom.Number:
		if x, ok := v.Int(); ok {
			return x, true
		}
		x, _ := v.Float64()
		return x, true
	case loom.String:
		return string(v), true
	case loom.Symbol:
		return string(v), true
	case loom.Vector:
		values := make([]interface{}, 0, len(v))
		for _, v := range v {
			if v, ok := Marshal(v); ok {
				values = append(values, v)
			}
		}
		return values, true
	case dictionary:
		result := map[string]interface{}{}
		for _, kvp := range v.pairs {
			key := ""
			if str, ok := kvp.Car().(loom.String); ok {
				key = string(str)
			} else {
				key = loom.EncodeToString(kvp.Car())
			}
			if v, ok := Marshal(kvp.Cdr().(*loom.Pair).Car()); ok {
				result[key] = v
			}
		}
		return result, true
	case *loom.Pair:
		var values []interface{}
		for {
			if tail, ok := v.Cdr().(*loom.Pair); ok {
				if e, ok := Marshal(v.Car()); ok {
					values = append(values, e)
				}
				v = tail
				continue
			}

			if e, ok := Marshal(v.Car()); ok {
				values = append(values, e)
			}
			if v.Cdr() != nil {
				if e, ok := Marshal(v.Cdr()); ok {
					values = append(values, e)
				}
			}
			return values, true
		}
	case loom.Procedure, elide:
		return nil, false
	default:
		return Marshal(v.MarshalSExp())
	}
}

type yamlMarshaler struct {
	flowStyle bool
	pos       token.Position
}

func (m *yamlMarshaler) nl() {
	m.pos.Line++
	m.pos.Column = m.pos.IndentLevel*2 + 1
}

func (m *yamlMarshaler) indent() {
	m.pos.IndentLevel++
	m.pos.Column = m.pos.IndentLevel*2 + 1
}

func (m *yamlMarshaler) unindent() {
	m.pos.IndentLevel--
	m.pos.Column = m.pos.IndentLevel*2 + 1
}

func (m *yamlMarshaler) tok(value string) *token.Token {
	pos := m.pos
	m.pos.Column += len(value)
	return token.New(value, value, &pos)
}

func (m *yamlMarshaler) marshal(v loom.Value) (ast.Node, bool) {
	if v == nil {
		return ast.Null(m.tok("null")), true
	}

	switch v := v.(type) {
	case loom.Boolean:
		if bool(v) {
			return ast.Bool(m.tok("true")), true
		}
		return ast.Bool(m.tok("false")), true
	case loom.Number:
		if x, ok := v.Int(); ok {
			return ast.Integer(m.tok(strconv.FormatInt(int64(x), 10))), true
		}
		x, _ := v.Float64()
		return ast.Float(m.tok(strconv.FormatFloat(float64(x), 'g', -1, 64))), true
	case loom.String:
		return ast.String(m.tok(string(v))), true
	case loom.Symbol:
		return ast.String(m.tok(string(v))), true
	case loom.Vector:
		sequence := ast.Sequence(m.tok("["), true)
		values := make([]ast.Node, 0, len(v))
		for _, v := range v {
			if v, ok := m.marshal(v); ok {
				values = append(values, v)
			}
		}
		sequence.Values = values
		tag := ast.Tag(m.tok("vec"))
		tag.Value = sequence
		return tag, true
	case dictionary:
		start := m.tok("")

		var pairs []*ast.MappingValueNode
		for _, kvp := range v.pairs {
			var key ast.Node
			if str, ok := kvp.Car().(loom.String); ok {
				key = ast.String(m.tok(string(str)))
			} else {
				key = ast.String(m.tok(loom.EncodeToString(kvp.Car())))
			}
			marker := m.tok(":")
			m.indent()
			if v, ok := m.marshal(kvp.Cdr().(*loom.Pair).Car()); ok {
				m.nl()
				pairs = append(pairs, ast.MappingValue(marker, key, v))
			}
			m.unindent()
		}
		return ast.Mapping(start, false, pairs...), true
	case *loom.Pair:
		sequence := ast.Sequence(m.tok("-"), m.flowStyle)
		m.indent()

		var values []ast.Node
		for {
			if tail, ok := v.Cdr().(*loom.Pair); ok {
				if e, ok := m.marshal(v.Car()); ok {
					values = append(values, e)
				}
				v = tail
				continue
			}

			if e, ok := m.marshal(v.Car()); ok {
				values = append(values, e)
			}
			if v.Cdr() != nil {
				if e, ok := m.marshal(v.Cdr()); ok {
					m.nl()
					values = append(values, e)
				}
			}
			sequence.Values = values
			m.unindent()
			return sequence, true
		}
	case loom.Procedure, elide:
		return nil, false
	default:
		return MarshalYAML(v.MarshalSExp(), m.flowStyle)
	}
}

func marshalYAMLDocument(body loom.Value, flowStyle bool) (*ast.DocumentNode, bool) {
	m := yamlMarshaler{flowStyle: flowStyle}
	m.nl()
	start := m.tok("")
	if bodyNode, ok := m.marshal(body); ok {
		return ast.Document(start, bodyNode), true
	}
	return nil, false
}

func MarshalYAML(v loom.Value, flowStyle bool) (ast.Node, bool) {
	m := yamlMarshaler{flowStyle: flowStyle}
	m.nl()
	return m.marshal(v)
}

func MarshalYAMLFile(v loom.Value, flowStyle bool) *ast.File {
	switch v := v.(type) {
	case loom.Vector:
		docs := make([]*ast.DocumentNode, 0, len(v))
		for _, v := range v {
			if doc, ok := marshalYAMLDocument(v, flowStyle); ok {
				docs = append(docs, doc)
			}
		}
		return &ast.File{Docs: docs}
	case *loom.Pair:
		var docs []*ast.DocumentNode
		for {
			if tail, ok := v.Cdr().(*loom.Pair); ok {
				if doc, ok := marshalYAMLDocument(v.Car(), flowStyle); ok {
					docs = append(docs, doc)
				}
				v = tail
				continue
			}

			if doc, ok := marshalYAMLDocument(v.Car(), flowStyle); ok {
				docs = append(docs, doc)
			}
			if v.Cdr() != nil {
				if doc, ok := marshalYAMLDocument(v.Cdr(), flowStyle); ok {
					docs = append(docs, doc)
				}
			}
			return &ast.File{Docs: docs}
		}
	default:
		docs := make([]*ast.DocumentNode, 0, 1)
		if doc, ok := marshalYAMLDocument(v, flowStyle); ok {
			docs = append(docs, doc)
		}
		return &ast.File{Docs: docs}
	}
}

func ParseYAML(file *ast.File) loom.Value {
	exprs := make([]loom.Value, len(file.Docs))
	for i, doc := range file.Docs {
		exprs[i] = parse(doc, true)
	}

	return list(qq, list(exprs...))
}

func Eval(env *loom.Env, file *ast.File) loom.Value {
	v := ParseYAML(file)
	return env.With(map[loom.Symbol]loom.Value{
		"dictionary?":    loom.ProcedureFunc(dictionaryPred),
		"dictionary":     loom.ProcedureFunc(dictionaryConstructor),
		"dictionary-ref": loom.ProcedureFunc(dictionaryRef),
		"yaml-elide":     loom.ProcedureFunc(elideConstructor),
	}).Eval(v)
}

func EvalFile(env *loom.Env, path string) (loom.Value, error) {
	file, err := parser.ParseFile(path, 0)
	if err != nil {
		return nil, fmt.Errorf("failed to parse %s: %w", path, err)
	}
	return Eval(env, file), nil
}
