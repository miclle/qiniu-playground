package handler

import "testing"

func TestNextLinkFindsRelNextInAnyParameterPosition(t *testing.T) {
	header := `<https://api.github.com/app/installations?page=2>; type="application/json"; rel="next", <https://api.github.com/app/installations?page=3>; rel="last"`
	got := nextLink(header)
	want := "https://api.github.com/app/installations?page=2"
	if got != want {
		t.Fatalf("nextLink() = %q, want %q", got, want)
	}
}
