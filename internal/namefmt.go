package sqlok

import (
	"fmt"
	"strings"
	"unicode"
)

func FirstUpper(s string) string {
	if s == "" {
		return s
	}

	runes := []rune(s)
	runes[0] = unicode.ToUpper(runes[0])
	return string(runes)
}

func CamelCase(n string) string {
	nx := strings.Split(n, "_")
	cc := ""
	for _, part := range nx {
		part = FirstUpper(strings.ToLower(part))
		cc = fmt.Sprintf("%s%s", cc, part)
	}
	return cc
}
