package functions

import (
	"context"

	"github.com/dop251/goja"
	"github.com/j3ssie/osmedeus/v5/internal/attackchain"
)

// Usage: db_import_attack_chain(workspace, file_path, target?, run_uuid?, mermaid_path?, text_path?) -> map
func (vf *vmFunc) dbImportAttackChain(call goja.FunctionCall) goja.Value {
	if len(call.Arguments) < 2 {
		return vf.errorValue("db_import_attack_chain requires at least 2 arguments: workspace, file_path")
	}

	workspace := call.Argument(0).String()
	filePath := call.Argument(1).String()
	target := optionalStringArg(call.Arguments, 2)
	runUUID := optionalStringArg(call.Arguments, 3)
	mermaidPath := optionalStringArg(call.Arguments, 4)
	textPath := optionalStringArg(call.Arguments, 5)

	summary, err := attackchain.ImportFile(context.Background(), workspace, filePath, target, runUUID, mermaidPath, textPath)
	if err != nil {
		return vf.errorValue(err.Error())
	}

	return vf.vm.ToValue(summary)
}

func optionalStringArg(args []goja.Value, index int) string {
	if index >= len(args) || goja.IsUndefined(args[index]) || goja.IsNull(args[index]) {
		return ""
	}
	return args[index].String()
}
