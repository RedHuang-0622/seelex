package architecture

import "go/ast"

// FunctionHasReceiver reports whether the declaration is a method on the named
// receiver type.
func FunctionHasReceiver(function *ast.FuncDecl, name string) bool {
	if function == nil || function.Recv == nil || len(function.Recv.List) != 1 {
		return false
	}
	receiver := function.Recv.List[0].Type
	if pointer, ok := receiver.(*ast.StarExpr); ok {
		receiver = pointer.X
	}
	identifier, ok := receiver.(*ast.Ident)
	return ok && identifier.Name == name
}
