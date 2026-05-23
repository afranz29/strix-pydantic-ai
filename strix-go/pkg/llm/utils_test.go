package llm

import (
	"reflect"
	"testing"
)

func TestNormalizeToolFormat(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "clean invoke format",
			input:    `<invoke name="test_tool"><parameter name="arg">val</parameter></invoke>`,
			expected: `<function=test_tool><parameter=arg>val</parameter></function>`,
		},
		{
			name:     "extra tags removed",
			input:    `<function_calls><invoke name="my_fn"><parameter name="p">1</parameter></invoke></function_calls>`,
			expected: `<function=my_fn><parameter=p>1</parameter></function>`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := NormalizeToolFormat(tt.input)
			if got != tt.expected {
				t.Errorf("NormalizeToolFormat() = %q, expected %q", got, tt.expected)
			}
		})
	}
}

func TestParseToolInvocations(t *testing.T) {
	input := `<function=run_nmap>
<parameter=target>127.0.0.1</parameter>
<parameter=ports>80,443</parameter>
</function>`

	expected := []ToolInvocation{
		{
			ToolName: "run_nmap",
			Kwargs: map[string]interface{}{
				"target": "127.0.0.1",
				"ports":  "80,443",
			},
		},
	}

	got := ParseToolInvocations(input)
	if !reflect.DeepEqual(got, expected) {
		t.Errorf("ParseToolInvocations() = %+v, expected %+v", got, expected)
	}
}
