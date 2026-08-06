package service

import (
	"net/http"
	"testing"
)

func TestEnumerateCIDR(t *testing.T) {
	ips, err := enumerateCIDR("192.168.1.0/30")
	if err != nil {
		t.Fatal(err)
	}
	// /30 = 4 IPs: .0 (net), .1, .2, .3
	// We exclude .0 (network) and .255-style broadcast, but for /30
	// .3 is actually the broadcast. Our simple filter only checks for .0/.255,
	// so .3 is included (it will just fail to connect during scan).
	if len(ips) != 3 {
		t.Fatalf("expected 3 IPs, got %d: %v", len(ips), ips)
	}
	if ips[0] != "192.168.1.1" {
		t.Errorf("first IP should be .1, got %s", ips[0])
	}
}

func TestEnumerateCIDR24(t *testing.T) {
	ips, err := enumerateCIDR("10.0.0.0/24")
	if err != nil {
		t.Fatal(err)
	}
	// /24 = 256 IPs, minus .0 and .255 = 254
	if len(ips) != 254 {
		t.Fatalf("expected 254 IPs, got %d", len(ips))
	}
	if ips[0] != "10.0.0.1" {
		t.Errorf("first IP should be 10.0.0.1, got %s", ips[0])
	}
	if ips[len(ips)-1] != "10.0.0.254" {
		t.Errorf("last IP should be 10.0.0.254, got %s", ips[len(ips)-1])
	}
}

func TestExtractHost(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"http://192.168.1.1/onvif/device_service", "192.168.1.1"},
		{"http://192.168.1.1:8080/onvif", "192.168.1.1"},
		{"https://10.0.0.1:443/ws", "10.0.0.1"},
		{"http://localhost/test", "localhost"},
	}
	for _, tt := range tests {
		got := extractHost(tt.input)
		if got != tt.want {
			t.Errorf("extractHost(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestCountProfileChannels(t *testing.T) {
	body := `
		<trt:Profiles><tt:Name>MediaProfile_Channel1_MainStream</tt:Name></trt:Profiles>
		<trt:Profiles><tt:Name>MediaProfile_Channel1_SubStream1</tt:Name></trt:Profiles>
		<trt:Profiles><tt:Name>MediaProfile_Channel2_MainStream</tt:Name></trt:Profiles>
		<trt:Profiles><tt:Name>MediaProfile_Channel3_MainStream</tt:Name></trt:Profiles>
	`
	got := countProfileChannels(body)
	if got != 3 {
		t.Errorf("expected 3 channels, got %d", got)
	}
}

func TestIsUniviewSignature(t *testing.T) {
	if !isUniviewSignature(http.Header{"Server": []string{"Uniview-Web"}}, "") {
		t.Fatal("Uniview Server header should be recognized")
	}
	if !isUniviewSignature(http.Header{}, "<title>UNV NVR</title>") {
		t.Fatal("UNV response body should be recognized")
	}
	if isUniviewSignature(http.Header{"WWW-Authenticate": []string{"Basic realm=\"Protected\""}}, "") {
		t.Fatal("generic 401 must not be labelled Uniview")
	}
}
