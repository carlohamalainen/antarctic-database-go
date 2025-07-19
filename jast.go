package ats

import (
	"fmt"
	"strings"

	"github.com/dop251/goja/ast"
	"github.com/dop251/goja/unistring"
)

func WalkAst(node ast.Node, depth int, fn func(string)) {
	if node == nil {
		return
	}

	indent := strings.Repeat("  ", depth)

	fmt.Printf("%s%T\n", indent, node)

	switch n := node.(type) {
	case *ast.Program:
		for _, stmt := range n.Body {
			WalkAst(stmt, depth+1, fn)
		}

	case *ast.VariableStatement:
		for _, decl := range n.List {
			WalkAst(decl, depth+1, fn)
		}

	case *ast.Binding:
		fmt.Printf("%s  Name: %s\n", indent, n.Target)
		WalkAst(n.Initializer, depth+1, fn)

	case *ast.StringLiteral:
		fmt.Printf("%s  Value: %q\n", indent, n.Value)
		fmt.Printf("%s  Idx: %d\n", indent, n.Idx)

	case *ast.AssignExpression:
		fmt.Printf("%s  Operator: %s\n", indent, n.Operator)
		fmt.Printf("%s  Left:\n", indent)
		WalkAst(n.Left, depth+1, fn)
		fmt.Printf("%s  Right:\n", indent)
		WalkAst(n.Right, depth+1, fn)

	case *ast.Identifier:
		fmt.Printf("%s  Name: %s\n", indent, n.Name)
		fmt.Printf("%s  Idx: %d\n", indent, n.Idx)

	case *ast.IfStatement:
		fmt.Printf("%s  Test:\n", indent)
		WalkAst(n.Test, depth+1, fn)
		fmt.Printf("%s  Consequent:\n", indent)
		WalkAst(n.Consequent, depth+1, fn)
		if n.Alternate != nil {
			fmt.Printf("%s  Alternate:\n", indent)
			WalkAst(n.Alternate, depth+1, fn)
		}

	case *ast.CallExpression:
		fmt.Printf("%s Callee arg list %v", indent, n.ArgumentList)
		WalkAst(n.Callee, depth+1, fn)

	case *ast.FunctionLiteral:
		name := fmt.Sprintf("%v", n.Name)
		if n.Name != nil {
			name = n.Name.Name.String()
		}

		fmt.Printf("%s FunctionLiteral: %v", indent, name)
		fmt.Printf("%s ParameterList: %v", indent, n.ParameterList)
		WalkAst(n.Body, depth+1, fn)

	case *ast.FunctionDeclaration:
		fmt.Printf("%s FunctionDeclaration: %v", indent, n.Function.Name)

		if n.Function.Name.Name == "getRptEndpoint" {
			fn(n.Function.Source)
		}

		WalkAst(n.Function, depth+1, fn)

	case *ast.BlockStatement:
		for _, s := range n.List {
			WalkAst(s, depth+1, fn)
		}

	default:
		fmt.Printf("%s SKIPPING %v", indent, n)
	}

}

func MatchFunction(node ast.Node, matchFnName string) *string {
	if node == nil {
		return nil
	}

	switch n := node.(type) {

	case *ast.FunctionDeclaration:
		if n.Function.Name.Name == unistring.String(matchFnName) {
			return &n.Function.Source
		}

		return MatchFunction(n.Function, matchFnName)

	case *ast.Program:
		for _, stmt := range n.Body {
			x := MatchFunction(stmt, matchFnName)

			if x != nil {
				return x
			}
		}
		return nil

	case *ast.VariableStatement:
		for _, decl := range n.List {
			x := MatchFunction(decl, matchFnName)

			if x != nil {
				return x
			}
		}
		return nil

	case *ast.Binding:
		return MatchFunction(n.Initializer, matchFnName)

	case *ast.StringLiteral:
		return nil

	case *ast.AssignExpression:
		x := MatchFunction(n.Left, matchFnName)
		if x != nil {
			return x
		}

		x = MatchFunction(n.Right, matchFnName)
		if x != nil {
			return x
		}

		return nil

	case *ast.Identifier:
		return nil

	case *ast.IfStatement:
		x := MatchFunction(n.Test, matchFnName)
		if x != nil {
			return x
		}

		x = MatchFunction(n.Consequent, matchFnName)
		if x != nil {
			return x
		}

		if n.Alternate != nil {
			x = MatchFunction(n.Alternate, matchFnName)
			if x != nil {
				return x
			}
		}

		return nil

	case *ast.CallExpression:
		return MatchFunction(n.Callee, matchFnName)

	case *ast.FunctionLiteral:
		return MatchFunction(n.Body, matchFnName)

	case *ast.BlockStatement:
		for _, s := range n.List {
			x := MatchFunction(s, matchFnName)
			if x != nil {
				return x
			}
		}
		return nil
	}

	return nil
}
