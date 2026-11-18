package cli

import "testing"

func TestSlugFoldsVietnamese(t *testing.T) {
	cases := map[string]string{
		"Đọc chương 4":          "doc-chuong-4",
		"  Ghi chú -- riêng!! ": "ghi-chu-rieng",
	}
	for in, want := range cases {
		if got := slug(in, 24); got != want {
			t.Errorf("slug(%q) = %q, want %q", in, got, want)
		}
	}
	if got := slug("một hai ba bốn năm sáu bảy tám chín mười", 10); len(got) > 10 || got[len(got)-1] == '-' {
		t.Errorf("slug not cut cleanly: %q", got)
	}
}

func TestCheckID(t *testing.T) {
	for _, ok := range []string{"ME_20261118_001", "A.b-c"} {
		if e := checkID(ok); e != nil {
			t.Errorf("checkID(%q) = %v", ok, e)
		}
	}
	for _, bad := range []string{"", ".x", "a b", "board", "tiếng"} {
		if e := checkID(bad); e == nil {
			t.Errorf("checkID(%q) accepted", bad)
		}
	}
}

func TestParseArgsRepeatedFlagsAndDoubleDash(t *testing.T) {
	p, err := parseArgs([]string{"x", "--env", "A=1", "--env=B=2", "--", "--env", "y"}, map[string]bool{"env": true})
	if err != nil {
		t.Fatal(err)
	}
	if len(p.flags["env"]) != 2 || p.flags["env"][1] != "B=2" {
		t.Fatalf("env: %v", p.flags["env"])
	}
	if len(p.pos) != 1 || len(p.rest) != 2 {
		t.Fatalf("pos=%v rest=%v", p.pos, p.rest)
	}
}
