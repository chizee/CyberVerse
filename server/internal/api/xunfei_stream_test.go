package api

import "testing"

func TestDetectXunfeiStreamTransportUsesStreamURL(t *testing.T) {
	tests := []struct {
		name      string
		streamURL string
		want      xunfeiStreamTransport
	}{
		{name: "rtmp", streamURL: "rtmp://example.test/live/avatar", want: xunfeiStreamTransportRTMP},
		{name: "http", streamURL: "http://example.test/live/avatar.flv", want: xunfeiStreamTransportHTTPFLV},
		{name: "https", streamURL: "https://example.test/live/avatar", want: xunfeiStreamTransportHTTPFLV},
		{name: "unsupported", streamURL: "xrtc://example.test/live/avatar", want: xunfeiStreamTransportUnknown},
		{name: "invalid", streamURL: "://bad", want: xunfeiStreamTransportUnknown},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := detectXunfeiStreamTransport(tt.streamURL); got != tt.want {
				t.Fatalf("detectXunfeiStreamTransport(%q) = %q, want %q", tt.streamURL, got, tt.want)
			}
		})
	}
}
