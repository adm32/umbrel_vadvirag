package main

import "testing"

func TestMetaIP(t *testing.T) {
	for ip, want := range map[string]bool{"173.252.107.1": true, "66.220.149.20": true, "57.141.5.9": true, "2a03:2880:f0ff::1": true,
		"84.2.10.5": false, "": false, "10.0.0.2": false} {
		if isMetaIP(ip) != want {
			t.Errorf("%s want %v", ip, want)
		}
	}
}
