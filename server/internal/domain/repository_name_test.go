package domain

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNewRepoDirName(t *testing.T) {
	tests := []struct {
		in      string
		want    string
		wantErr string
	}{
		{in: "My App", want: "my-app"},
		{in: "  api_v2.service  ", want: "api_v2.service"},
		{in: "../../etc/passwd", want: "etcpasswd"},
		{in: "-.leading and trailing._-", want: "leading-and-trailing"},
		{in: "", wantErr: "name is required"},
		{in: "..", wantErr: "no letters or digits"},
		{in: "çğü", wantErr: "no letters or digits"},
		{in: strings.Repeat("a", 101), wantErr: "too long"},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got, err := NewRepoDirName(tt.in)
			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}
