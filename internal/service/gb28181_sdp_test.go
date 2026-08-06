package service

import (
	"strings"
	"testing"

	"CameraIO/internal/pkg"
)

func TestBuildInviteSDP_Hikvision(t *testing.T) {
	svc := NewGB28181Service(&pkg.Config{SIPRealm: "3402000000"}, nil, nil, nil)
	sdp := svc.buildInviteSDP(10001, "10.12.0.100")
	checks := []string{
		"a=recvonly",
		"a=encrypt:0",
		"y=",
		"m=video 10001 RTP/AVP 96",
		"a=rtpmap:96 PS/90000",
		"o=",
	}
	for _, c := range checks {
		if !strings.Contains(sdp, c) {
			t.Errorf("SDP missing %q:\n%s", c, sdp)
		}
	}
}
