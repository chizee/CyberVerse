package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os/exec"
	"strings"
	"sync"
	"time"
)

type xunfeiStreamTransport string

const (
	xunfeiStreamTransportUnknown xunfeiStreamTransport = ""
	xunfeiStreamTransportHTTPFLV xunfeiStreamTransport = "http_flv"
	xunfeiStreamTransportRTMP    xunfeiStreamTransport = "rtmp"
)

type xunfeiDNSCacheEntry struct {
	ips     []string
	expires time.Time
}

var xunfeiStreamDNSCache = struct {
	sync.Mutex
	entries map[string]xunfeiDNSCacheEntry
}{
	entries: make(map[string]xunfeiDNSCacheEntry),
}

func (r *Router) handleXunfeiAvatarStream(w http.ResponseWriter, req *http.Request) {
	id := strings.TrimSpace(req.PathValue("id"))
	if id == "" {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "session id is required"})
		return
	}
	session, err := r.sessionMgr.Get(id)
	if err != nil {
		writeJSON(w, http.StatusNotFound, ErrorResponse{Error: err.Error()})
		return
	}
	if !r.authorizeKanshanSessionAccess(w, req, session) {
		return
	}
	if r.orch == nil {
		writeJSON(w, http.StatusServiceUnavailable, ErrorResponse{Error: "Xunfei stream proxy requires the orchestrator"})
		return
	}
	_, streamURL, ok := r.orch.XunfeiAvatarStream(id)
	if !ok {
		writeJSON(w, http.StatusNotFound, ErrorResponse{Error: "Xunfei avatar stream is not available"})
		return
	}
	switch detectXunfeiStreamTransport(streamURL) {
	case xunfeiStreamTransportRTMP:
		r.proxyXunfeiRTMPStream(w, req, id, streamURL)
		return
	case xunfeiStreamTransportHTTPFLV:
		r.proxyXunfeiHTTPStream(w, req, id, streamURL)
		return
	}
	writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "Xunfei stream proxy only supports RTMP or HTTP-FLV streams"})
}

func (r *Router) proxyXunfeiRTMPStream(w http.ResponseWriter, req *http.Request, id string, streamURL string) {
	ffmpegPath, err := exec.LookPath("ffmpeg")
	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, ErrorResponse{Error: "ffmpeg is required for Xunfei RTMP playback"})
		return
	}

	cmd := exec.CommandContext(
		req.Context(),
		ffmpegPath,
		"-hide_banner",
		"-loglevel", "error",
		"-i", streamURL,
		"-c", "copy",
		"-flvflags", "no_duration_filesize",
		"-f", "flv",
		"pipe:1",
	)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, ErrorResponse{Error: "failed to prepare Xunfei stream proxy"})
		return
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		writeJSON(w, http.StatusBadGateway, ErrorResponse{Error: "failed to start Xunfei stream proxy"})
		return
	}

	w.Header().Set("Content-Type", "video/x-flv")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Accel-Buffering", "no")
	copyXunfeiStream(w, req, id, stdout)
	waitErr := cmd.Wait()
	if req.Context().Err() != nil {
		return
	}
	if waitErr != nil {
		log.Printf("Xunfei stream proxy exited session=%s: %v stderr=%s", id, waitErr, strings.TrimSpace(stderr.String()))
	}
}

func (r *Router) proxyXunfeiHTTPStream(w http.ResponseWriter, req *http.Request, id string, streamURL string) {
	upstreamReq, err := http.NewRequestWithContext(req.Context(), http.MethodGet, streamURL, nil)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "invalid Xunfei HTTP-FLV stream URL"})
		return
	}
	upstreamReq.Header.Set("Accept", "video/x-flv,*/*")
	upstreamReq.Header.Set("User-Agent", "CyberVerse")

	resp, err := xunfeiStreamHTTPClient().Do(upstreamReq)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, ErrorResponse{Error: "failed to connect Xunfei HTTP-FLV stream: " + err.Error()})
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		writeJSON(w, http.StatusBadGateway, ErrorResponse{Error: fmt.Sprintf("Xunfei HTTP-FLV stream returned %s", resp.Status)})
		return
	}

	w.Header().Set("Content-Type", "video/x-flv")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Accel-Buffering", "no")
	copyXunfeiStream(w, req, id, resp.Body)
}

func copyXunfeiStream(w http.ResponseWriter, req *http.Request, id string, reader io.Reader) {
	flusher, canFlush := w.(http.Flusher)
	if canFlush {
		flusher.Flush()
	}

	startedAt := time.Now()
	var bytesCopied int64
	var copyErr error
	buf := make([]byte, 32*1024)
	for {
		n, readErr := reader.Read(buf)
		if n > 0 {
			written, writeErr := w.Write(buf[:n])
			bytesCopied += int64(written)
			if canFlush {
				flusher.Flush()
			}
			if writeErr != nil {
				copyErr = writeErr
				break
			}
			if written != n {
				copyErr = io.ErrShortWrite
				break
			}
		}
		if readErr != nil {
			if readErr != io.EOF {
				copyErr = readErr
			}
			break
		}
	}
	if req.Context().Err() != nil {
		return
	}
	log.Printf("Xunfei stream proxy ended session=%s bytes=%d elapsed_ms=%d", id, bytesCopied, time.Since(startedAt).Milliseconds())
	if copyErr != nil {
		log.Printf("Xunfei stream proxy copy failed session=%s: %v", id, copyErr)
	}
}

func detectXunfeiStreamTransport(streamURL string) xunfeiStreamTransport {
	parsed, err := url.Parse(strings.TrimSpace(streamURL))
	if err != nil {
		return xunfeiStreamTransportUnknown
	}
	switch strings.ToLower(parsed.Scheme) {
	case "rtmp":
		return xunfeiStreamTransportRTMP
	case "http", "https":
		return xunfeiStreamTransportHTTPFLV
	default:
		return xunfeiStreamTransportUnknown
	}
}

func xunfeiStreamHTTPClient() *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	dialer := &net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}
	transport.Proxy = nil
	transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil {
			return dialer.DialContext(ctx, network, address)
		}
		ips := lookupXunfeiStreamIPs(ctx, host)
		for _, ip := range ips {
			conn, dialErr := dialer.DialContext(ctx, network, net.JoinHostPort(ip, port))
			if dialErr == nil {
				return conn, nil
			}
		}
		return dialer.DialContext(ctx, network, address)
	}
	return &http.Client{Transport: transport}
}

func lookupXunfeiStreamIPs(ctx context.Context, host string) []string {
	host = strings.TrimSpace(host)
	if host == "" {
		return nil
	}

	now := time.Now()
	xunfeiStreamDNSCache.Lock()
	if entry, ok := xunfeiStreamDNSCache.entries[host]; ok && now.Before(entry.expires) {
		ips := append([]string(nil), entry.ips...)
		xunfeiStreamDNSCache.Unlock()
		return ips
	}
	xunfeiStreamDNSCache.Unlock()

	ips := lookupXunfeiStreamIPsViaDoH(ctx, host)
	if len(ips) == 0 {
		return nil
	}

	xunfeiStreamDNSCache.Lock()
	xunfeiStreamDNSCache.entries[host] = xunfeiDNSCacheEntry{ips: append([]string(nil), ips...), expires: now.Add(1 * time.Minute)}
	xunfeiStreamDNSCache.Unlock()
	return ips
}

func lookupXunfeiStreamIPsViaDoH(ctx context.Context, host string) []string {
	type dnsAnswer struct {
		Type int    `json:"type"`
		Data string `json:"data"`
	}
	type dnsResponse struct {
		Answer []dnsAnswer `json:"Answer"`
	}

	endpoints := []string{
		"https://dns.alidns.com/resolve?name=" + url.QueryEscape(host) + "&type=A",
		"https://dns.google/resolve?name=" + url.QueryEscape(host) + "&type=A",
	}
	client := &http.Client{Timeout: 5 * time.Second}
	for _, endpoint := range endpoints {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
		if err != nil {
			continue
		}
		resp, err := client.Do(req)
		if err != nil {
			continue
		}
		var decoded dnsResponse
		err = json.NewDecoder(resp.Body).Decode(&decoded)
		_ = resp.Body.Close()
		if err != nil || resp.StatusCode < 200 || resp.StatusCode >= 300 {
			continue
		}
		ips := make([]string, 0, len(decoded.Answer))
		for _, answer := range decoded.Answer {
			if answer.Type != 1 {
				continue
			}
			ip := net.ParseIP(strings.TrimSpace(answer.Data))
			if ip == nil || ip.To4() == nil {
				continue
			}
			ips = append(ips, ip.String())
		}
		if len(ips) > 0 {
			return ips
		}
	}
	return nil
}
