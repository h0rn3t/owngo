package build

import "testing"

func TestApplyOwngoTag(t *testing.T) {
	tests := []struct {
		name string
		tags string
		want string
	}{
		{name: "empty", tags: "", want: ""},
		{name: "other", tags: "purego", want: "purego"},
		{name: "owngo", tags: "owngo", want: "owngo,nogc"},
		{name: "owngo among others", tags: "foo,owngo,bar", want: "foo,owngo,bar,nogc"},
		{name: "already nogc", tags: "owngo,nogc", want: "owngo,nogc"},
		{name: "nogc before owngo", tags: "nogc,owngo", want: "nogc,owngo"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			conf := &Config{Tags: test.tags}
			applyOwngoTag(conf)
			if conf.Tags != test.want {
				t.Errorf("applyOwngoTag(%q) = %q, want %q", test.tags, conf.Tags, test.want)
			}
		})
	}
}
