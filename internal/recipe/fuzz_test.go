package recipe

import "testing"

// FuzzRecipeDecode asserts the recipe decoder never panics on arbitrary bytes.
func FuzzRecipeDecode(f *testing.F) {
	f.Add([]byte("version: 1\n"))
	f.Add([]byte(`{"version":1}`))
	f.Add([]byte("{"))
	f.Add([]byte(""))
	f.Fuzz(func(t *testing.T, data []byte) {
		_, _ = Decode(data, "fuzz.yaml")
	})
}
