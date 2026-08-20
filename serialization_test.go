package switchonyourcode

import "testing"

func TestCustomBucketStringsUseGoJSONEscaping(t *testing.T) {
	cases := []struct {
		name  string
		value string
		want  string
	}{
		{name: "html-sensitive characters", value: "<&>", want: `"\u003c\u0026\u003e"`},
		{name: "line separator", value: "\u2028", want: `"\u2028"`},
		{name: "paragraph separator", value: "\u2029", want: `"\u2029"`},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := scalarBucketValue(tc.value)
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.want {
				t.Fatalf("encoded = %q, want %q", got, tc.want)
			}
		})
	}
}
