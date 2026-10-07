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
		{in: "CON", want: "con-repo"},
		{in: "nul", want: "nul-repo"},
		{in: "Aux.txt", want: "aux-repo.txt"},
		{in: "prn.tar.gz", want: "prn-repo.tar.gz"},
		{in: "com1", want: "com1-repo"},
		{in: "LPT9", want: "lpt9-repo"},
		{in: "com0", want: "com0-repo"},
		{in: "-nul-", want: "nul-repo"},
		{in: "con-", want: "con-repo"},
		{in: "console", want: "console"},
		{in: "com10", want: "com10"},
		{in: "comx", want: "comx"},
		{in: "my.con", want: "my.con"},
		{in: "trailing dot.", want: "trailing-dot"},
		{in: "trailing space ", want: "trailing-space"},
		{in: "dots and spaces . .", want: "dots-and-spaces"},
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
