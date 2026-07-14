package website

import "testing"

func TestIsAPIRoute(t *testing.T) {
	tests := []struct {
		route string
		want  bool
	}{
		{route: "/api", want: true},
		{route: "/api/v1/auth/me", want: true},
		{route: "/workspaces", want: false},
		{route: "/code-runner", want: false},
		{route: "/code-runner/crs_01HX", want: false},
		{route: "/credentials", want: false},
		{route: "/codebases", want: false},
		{route: "/sandboxes", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.route, func(t *testing.T) {
			if got := isAPIRoute(tt.route); got != tt.want {
				t.Fatalf("isAPIRoute(%q) = %v, want %v", tt.route, got, tt.want)
			}
		})
	}
}
