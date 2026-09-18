package root

import "testing"

func TestResearchEnvelopePreservesValidUnicodeAndRejectsReplacement(t *testing.T) {
	for _, tc := range []struct {
		name  string
		body  []byte
		valid bool
	}{
		{"literal Unicode", []byte(`{"content":"📍 café"}`), true},
		{"surrogate pair", []byte(`{"content":"\ud83d\udccd café"}`), true},
		{"literal replacement character", []byte(`{"content":"�"}`), true},
		{"escaped backslash", []byte(`{"content":"\\ud800"}`), true},
		{"escaped quote", []byte(`{"content":"\"\ud83d\udccd"}`), true},
		{"raw invalid UTF8", append([]byte(`{"content":"`), 255, '"', '}'), false},
		{"high surrogate alone", []byte(`{"content":"\ud800"}`), false},
		{"low surrogate alone", []byte(`{"content":"\udc00"}`), false},
		{"wrong second surrogate", []byte(`{"content":"\ud800\u0041"}`), false},
		{"separated surrogates", []byte(`{"content":"\ud800 \udc00"}`), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := validResearchUnicode(tc.body); got != tc.valid {
				t.Fatalf("valid=%t want=%t", got, tc.valid)
			}
		})
	}
}
