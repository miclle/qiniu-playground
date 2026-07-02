package website

import "strings"

func isAPIRoute(route string) bool {
	return route == "/api" || strings.HasPrefix(route, "/api/")
}
