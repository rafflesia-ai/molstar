package job

import "testing"

// FuzzJobDecode asserts the job decoder never panics on arbitrary bytes — a job
// is decoded from an untrusted /render or /validate HTTP body.
func FuzzJobDecode(f *testing.F) {
	f.Add([]byte(`{"version":1,"inputs":{"a":{"id":"1abc"}},"scene":{"structures":[{"source":"a"}]},"outputs":[{"type":"image","path":"o.png"}]}`))
	f.Add([]byte("version: 1\n"))
	f.Add([]byte("{"))
	f.Add([]byte(""))
	f.Fuzz(func(t *testing.T, data []byte) {
		if j, err := Decode(data, "fuzz.json"); err == nil {
			// A decoded job must also survive validation without panicking.
			_ = j.ValidateRender()
		}
	})
}
