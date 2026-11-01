package traceback

import "testing"

func TestParse(t *testing.T) {
	negative := int(-1)
	wantNegative := All
	if uint64(^uint(0)) == 0xffffffff {
		wantNegative |= uint64(uint32(negative) << Shift)
	}
	for _, tc := range []struct {
		text string
		want uint64
	}{
		{"none", 0}, {"single", 4}, {"", 4}, {"all", 6},
		{"system", 10}, {"crash", 11}, {"wer", 2},
		{"0", 2}, {"1", 6}, {"2", 10}, {"3", 14}, {"+2", 10},
		{"-0", 2}, {"-1", wantNegative}, {"+", 2}, {"- ", 2}, {"bad", 2},
		{"-2147483648", 2}, {"-2147483649", 2},
		{" 1", 2}, {"1 ", 2}, {"4294967296", 2},
		{"18446744073709551616", 2},
	} {
		if got := Parse(tc.text, false); got != tc.want {
			t.Errorf("Parse(%q) = %d, want %d", tc.text, got, tc.want)
		}
	}
}

func TestCombine(t *testing.T) {
	for _, tc := range []struct {
		name, environment, setting string
		want                       uint64
	}{
		{"higher setting", "single", "system", 10},
		{"higher environment", "system", "single", 10},
		{"independent flags", "all", "crash", 11},
		{"numeric levels", "1", "2", 10},
	} {
		t.Run(tc.name, func(t *testing.T) {
			environment := Parse(tc.environment, false)
			setting := Parse(tc.setting, false)
			if got := Combine(environment, setting); got != tc.want {
				t.Fatalf("Combine(%#x, %#x) = %#x, want %#x", environment, setting, got, tc.want)
			}
		})
	}
}
