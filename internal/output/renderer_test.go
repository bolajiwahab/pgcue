package output

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestOutputValues(t *testing.T) {
	require.Equal(t,
		[]string{"NULL", "hello", "42", "true"},
		displayRow([]any{nil, "hello", int64(42), true}),
	)
}
