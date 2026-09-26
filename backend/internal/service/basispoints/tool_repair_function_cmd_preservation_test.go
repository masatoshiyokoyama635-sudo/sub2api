package basispoints

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestToolRepairPreservesRawFunctionCmdNamedJSON(t *testing.T) {
	for _, code := range []string{
		"{\"name\":\"exec_command\",\"arguments\":{\"cmd\":\"echo embedded\",\"description\":\"embedded payload\"}}",
		"{\"name\":\"not_a_catalog_tool\",\"arguments\":{\"cmd\":\"literal text\"}}",
	} {
		t.Run(code, func(t *testing.T) {
			source := testSource()
			source["tools"] = []any{functionCmdTestTool("exec_command")}
			_, bridge := mustPrepare(t, source, "raw-command-preservation", nil)
			broken := functionCmdTestNative(t, "exec_command", code, "invalid JSON metadata")
			repaired := functionCmdTestNative(t, "exec_command", code, "{}")
			require.True(t, bridge.preservesToolOperations([]object{broken}, []object{repaired}), "explicit FUNCTION_CMD code remains opaque even if it looks like a named envelope")
			changed := functionCmdTestNative(t, "exec_command", "echo embedded", "{}")
			require.False(t, bridge.preservesToolOperations([]object{broken}, []object{changed}), "a formatting repair must not reinterpret or rewrite executable text")
		})
	}
}
