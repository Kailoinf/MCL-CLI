package main

import "testing"

func TestParsePropertyPort(t *testing.T) {
	cases := []struct {
		name string
		body string
		port int
		ok   bool
	}{
		{"normal", "motd=A Minecraft Server\nserver-port=25566\n", 25566, true},
		{"comments and spaces", "# server-port=1\nserver-port = 25567\n", 25567, true},
		{"absent", "motd=hi\nonline-mode=true\n", 0, false},
		{"not a number", "server-port=abc\n", 0, false},
		{"out of range", "server-port=99999\n", 0, false},
		{"commented out", "#server-port=25566\n", 0, false},
		{"empty", "", 0, false},
	}
	for _, c := range cases {
		port, ok := parsePropertyPort(c.body)
		if ok != c.ok || port != c.port {
			t.Errorf("%s: got %d %v, want %d %v", c.name, port, ok, c.port, c.ok)
		}
	}
}
