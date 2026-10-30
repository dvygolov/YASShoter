//go:build windows

package main

import "strings"

// shorten clips a window title so the overlay label stays readable.
func shorten(s string, max int) string {
	s = strings.TrimSpace(s)
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max-1]) + "…"
}
