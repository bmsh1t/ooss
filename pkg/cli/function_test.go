package cli

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestProtectLLMInvokeCustomBodyPlaceholders_ScopedToSecondArgument(t *testing.T) {
	script := `var outside = "{{message}}"; llm_invoke_custom("hello", "{\"input\":\"{{message}}\",\"messages\":[{\"role\":\"user\",\"content\":\"{{target}}\"}]}");`

	protected := protectLLMInvokeCustomBodyPlaceholders(script)

	assert.Contains(t, protected, `var outside = "{{message}}"`)
	assert.Contains(t, protected, llmInvokeCustomMessageSentinel)
	assert.NotContains(t, protected, `\"input\":\"{{message}}\"`)
	assert.Contains(t, restoreLLMInvokeCustomBodyPlaceholders(protected), script)
}

func TestProtectLLMInvokeCustomBodyPlaceholders_MultipleCalls(t *testing.T) {
	script := `llm_invoke_custom("one", "{\"input\":\"{{message}}\"}"); llm_invoke_custom("two", "{\"input\":\"{{message}}\",\"meta\":{\"arr\":[1,2,3]}}");`

	protected := protectLLMInvokeCustomBodyPlaceholders(script)

	assert.Equal(t, 2, strings.Count(protected, llmInvokeCustomMessageSentinel))
	assert.Equal(t, script, restoreLLMInvokeCustomBodyPlaceholders(protected))
}
