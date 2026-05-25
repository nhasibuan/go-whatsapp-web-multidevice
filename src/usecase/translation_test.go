package usecase

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestHashSource_StableAndWhitespaceTolerant(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		a, b string
		want bool // true when a and b should hash to the same value
	}{
		{
			name: "identical strings collide",
			a:    "hello world",
			b:    "hello world",
			want: true,
		},
		{
			name: "leading/trailing whitespace is normalized",
			a:    "  hello world  ",
			b:    "hello world",
			want: true,
		},
		{
			name: "different content does not collide",
			a:    "hello world",
			b:    "hello there",
			want: false,
		},
		{
			name: "empty strings collide",
			a:    "",
			b:    "",
			want: true,
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ha, hb := hashSource(tc.a), hashSource(tc.b)
			if tc.want {
				assert.Equal(t, ha, hb, "expected matching hashes")
			} else {
				assert.NotEqual(t, ha, hb, "expected differing hashes")
			}
			// hex sha256 is 64 chars
			assert.Len(t, ha, 64)
		})
	}
}
