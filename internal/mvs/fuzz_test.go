package mvs

import "testing"

// FuzzExplainSelector asserts the MVS selector compiler never panics on
// arbitrary selector strings — scene selectors arrive from untrusted /render
// requests.
func FuzzExplainSelector(f *testing.F) {
	f.Add("all")
	f.Add("chain:A")
	f.Add("resi:1-10")
	f.Add("within:5:chain:A")
	f.Add("")
	f.Add(":::")
	f.Add("resi:-")
	f.Fuzz(func(t *testing.T, value string) {
		_, _ = ExplainSelector(value)
	})
}
