package notify

import "testing"

func TestCallbackAcknowledgementIsExplicit(t *testing.T) {
	for _, v := range []string{"success", "ok", " SUCCESS\n"} {
		if !acceptedAck([]byte(v)) {
			t.Fatal("valid ack rejected")
		}
	}
	for _, v := range []string{"", "fail", "not success", "{\"message\":\"ok\"}", "gateway timeout"} {
		if acceptedAck([]byte(v)) {
			t.Fatal("invalid ack accepted", v)
		}
	}
}
