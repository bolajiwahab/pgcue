package manual

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFormatDefault(t *testing.T) {
	require.Equal(t, "null", FormatDefault(nil))
	require.Equal(t, "20", FormatDefault(20))
}
