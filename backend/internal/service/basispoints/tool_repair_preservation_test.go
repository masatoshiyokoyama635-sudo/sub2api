package basispoints

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// These fixtures are inert strings. The bridge only converts tool calls and
// must never evaluate either the original or corrected payload.
func TestToolRepairPreservesMalformedCustomEnvelopeInput(t *testing.T) {
	for _, field := range []string{"input", "args"} {
		for _, changed := range []bool{false, true} {
			name := field + "/preserved"
			if changed {
				name = field + "/changed"
			}
			t.Run(name, func(t *testing.T) {
				cache := new(ReplayCache)
				_, bridge := repairBridge(t, cache)
				original := nativeCall(object{"name": "functions.exec", field: "text(1)", "arguments": object{}})
				initial := repairResponse("resp_original", 10, 2, original)
				require.Error(t, bridge.validateToolResponse(initial), "the extra arguments field must require transport repair")
				code := "text(1)"
				if changed {
					code = "text(2)"
				}
				corrected := repairCall("fixed_custom", customTransportPrefix+"functions.exec", code)
				attempts := 0
				body := bridge.StreamWithToolRepair(context.Background(), io.NopCloser(strings.NewReader(sse(object{"type": "response.completed", "response": initial}))), func(context.Context, object, error) (object, error) {
					attempts++
					return repairResponse("resp_correction", 20, 3, corrected), nil
				})
				events := repairEvents(t, body)
				require.Equal(t, 1, attempts)
				last := events[len(events)-1]
				if changed {
					require.Equal(t, "response.failed", last["type"], "repair must not replace the existing custom operation")
					require.Len(t, events, 1, "a rejected batch must emit no client tool events")
					require.Nil(t, cache.get("repair-scope", "fixed_custom"))
					return
				}
				require.Equal(t, "response.completed", last["type"], "removing a wrong envelope field without changing input remains allowed")
				response := repairValue[object](t, last["response"])
				output := repairValue[[]any](t, response["output"])
				require.Equal(t, "text(1)", repairValue[object](t, output[0])["input"])
				require.NotNil(t, cache.get("repair-scope", "fixed_custom"))
			})
		}
	}
}

func TestToolRepairPreservesFunctionCodeJSONPayload(t *testing.T) {
	for _, changed := range []bool{false, true} {
		name := "preserved"
		if changed {
			name = "changed"
		}
		t.Run(name, func(t *testing.T) {
			source := testSource()
			source["tools"] = []any{functionCodeTestTool("run_code")}
			cache := new(ReplayCache)
			_, bridge := mustPrepare(t, source, "repair-scope", cache)
			const originalCode = "{\"name\":\"shell\",\"arguments\":{\"cmd\":\"pwd\"}}"
			original := functionCodeTestNative(t, "run_code", originalCode, "invalid metadata")
			initial := repairResponse("resp_original", 10, 2, original)
			require.Error(t, bridge.validateToolResponse(initial), "invalid metadata must require transport repair")
			code := originalCode
			if changed {
				code = "{\"name\":\"shell\",\"arguments\":{\"cmd\":\"echo changed\"}}"
			}
			corrected := functionCodeTestNative(t, "run_code", code, "{\"description\":\"Read fixture\"}")
			corrected["id"], corrected["call_id"] = "fc_fixed_code", "fixed_code"
			attempts := 0
			body := bridge.StreamWithToolRepair(context.Background(), io.NopCloser(strings.NewReader(sse(object{"type": "response.completed", "response": initial}))), func(context.Context, object, error) (object, error) {
				attempts++
				return repairResponse("resp_correction", 20, 3, corrected), nil
			})
			events := repairEvents(t, body)
			require.Equal(t, 1, attempts)
			last := events[len(events)-1]
			if changed {
				require.Equal(t, "response.failed", last["type"], "a JSON-shaped raw code parameter must still be preserved byte for byte")
				require.Len(t, events, 1, "a rejected batch must emit no client tool events")
				require.Nil(t, cache.get("repair-scope", "fixed_code"))
				return
			}
			require.Equal(t, "response.completed", last["type"], "correcting metadata without replacing raw code remains allowed")
			response := repairValue[object](t, last["response"])
			output := repairValue[[]any](t, response["output"])
			args := functionCodeTestArguments(t, repairValue[object](t, output[0]))
			require.Equal(t, originalCode, args["code"])
			require.NotNil(t, cache.get("repair-scope", "fixed_code"))
		})
	}
}
