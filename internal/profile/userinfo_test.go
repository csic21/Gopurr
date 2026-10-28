package profile

import "testing"

func TestUserinfoDecimalScientificAndBounds(t *testing.T) {
	info := parseUserinfo("UPLOAD=1.5e9; Download=123.0; TOTAL=1125899906842624; EXPIRE=1.8e9")
	if info != (Info{1500000000, 123, 1125899906842624, 1800000000}) {
		t.Fatalf("lost valid usage fields: %+v", info)
	}
	for _, invalid := range []string{"-1", "NaN", "+Inf", "9223372036854775808", "1e999", "1/2"} {
		if got := parseUserinfo("upload=" + invalid); got.Upload != 0 {
			t.Fatalf("accepted invalid usage %q: %+v", invalid, got)
		}
	}
	if got := parseUserinfo("total=9223372036854775807"); got.Total != 9223372036854775807 {
		t.Fatal("integer precision lost")
	}
}
