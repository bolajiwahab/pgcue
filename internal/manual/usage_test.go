package manual

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFlagNames(t *testing.T) {
	for _, test := range []struct{ parameter, flag string }{
		{"minimum_duration", "minimum-duration"},
		{"database", "database"},
	} {
		t.Run(test.parameter, func(t *testing.T) {
			require.Equal(t, test.flag, FlagName(test.parameter))
		})
	}
}
