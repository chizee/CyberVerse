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

	xunfeiStreamStatsInterval = 5 * time.Second
	xunfeiStreamSlowReadGap   = 500 * time.Millisecond
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
		logXunfeiStreamError(id, xunfeiStreamTransportUnknown, fmt.Errorf("orchestrator unavailable"))
		writeJSON(w, http.StatusServiceUnavailable, ErrorResponse{Error: "Xunfei stream proxy requires the orchestrator"})
		return
	}
	_, streamURL, ok := r.orch.XunfeiAvatarStream(id)
	if !ok {
		logXunfeiStreamError(id, xunfeiStreamTransportUnknown, fmt.Errorf("stream unavailable"))
		writeJSON(w, http.StatusNotFound, ErrorResponse{Error: "Xunfei avatar stream is not available"})
		return
	}
	transport := detectXunfeiStreamTransport(streamURL)
	switch transport {
	case xunfeiStreamTransportRTMP:
		r.proxyXunfeiRTMPStream(w, req, id, streamURL)
		return
	case xunfeiStreamTransportHTTPFLV:
		r.proxyXunfeiHTTPStream(w, req, id, streamURL)
		return
	}
	logXunfeiStreamError(id, transport, fmt.Errorf("unsupported stream transport"))
	writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "Xunfei stream proxy only supports RTMP or HTTP-FLV streams"})
}

func (r *Router) proxyXunfeiRTMPStream(w http.ResponseWriter, req *http.Request, id string, streamURL string) {
	ffmpegPath, err := exec.LookPath("ffmpeg")
	if err != nil {
		logXunfeiStreamError(id, xunfeiStreamTransportRTMP, err)
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
		logXunfeiStreamError(id, xunfeiStreamTransportRTMP, err)
		writeJSON(w, http.StatusInternalServerError, ErrorResponse{Error: "failed to prepare Xunfei stream proxy"})
		return
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		logXunfeiStreamError(id, xunfeiStreamTransportRTMP, err)
		writeJSON(w, http.StatusBadGateway, ErrorResponse{Error: "failed to start Xunfei stream proxy"})
		return
	}
	logXunfeiStreamConnected(id, xunfeiStreamTransportRTMP)

	w.Header().Set("Content-Type", "video/x-flv")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Accel-Buffering", "no")
	copyXunfeiStream(w, req, id, xunfeiStreamTransportRTMP, stdout)
	waitErr := cmd.Wait()
	if req.Context().Err() != nil {
		return
	}
	if waitErr != nil {
		errText := strings.TrimSpace(stderr.String())
		if errText != "" {
			waitErr = fmt.Errorf("%w: %s", waitErr, errText)
		}
		logXunfeiStreamError(id, xunfeiStreamTransportRTMP, waitErr)
	}
}

func (r *Router) proxyXunfeiHTTPStream(w http.ResponseWriter, req *http.Request, id string, streamURL string) {
	upstreamReq, err := http.NewRequestWithContext(req.Context(), http.MethodGet, streamURL, nil)
	if err != nil {
		logXunfeiStreamError(id, xunfeiStreamTransportHTTPFLV, err)
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "invalid Xunfei HTTP-FLV stream URL"})
		return
	}
	upstreamReq.Header.Set("Accept", "video/x-flv,*/*")
	upstreamReq.Header.Set("User-Agent", "CyberVerse")

	resp, err := xunfeiStreamHTTPClient().Do(upstreamReq)
	if err != nil {
		logXunfeiStreamError(id, xunfeiStreamTransportHTTPFLV, err)
		writeJSON(w, http.StatusBadGateway, ErrorResponse{Error: "failed to connect Xunfei HTTP-FLV stream: " + err.Error()})
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		logXunfeiStreamError(id, xunfeiStreamTransportHTTPFLV, fmt.Errorf("upstream returned %s", resp.Status))
		writeJSON(w, http.StatusBadGateway, ErrorResponse{Error: fmt.Sprintf("Xunfei HTTP-FLV stream returned %s", resp.Status)})
		return
	}
	logXunfeiStreamConnected(id, xunfeiStreamTransportHTTPFLV)

	w.Header().Set("Content-Type", "video/x-flv")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Accel-Buffering", "no")
	copyXunfeiStream(w, req, id, xunfeiStreamTransportHTTPFLV, resp.Body)
}

func copyXunfeiStream(w http.ResponseWriter, req *http.Request, id string, transport xunfeiStreamTransport, reader io.Reader) {
	flusher, canFlush := w.(http.Flusher)
	if canFlush {
		flusher.Flush()
	}

	var copyErr error
	buf := make([]byte, 16*1024)
	startedAt := time.Now()
	lastReadAt := startedAt
	lastStatsAt := startedAt
	var totalBytes int64
	var totalChunks int64
	var windowBytes int64
	var windowChunks int64
	var windowSlowReads int
	var windowMaxReadGap time.Duration
	for {
		n, readErr := reader.Read(buf)
		now := time.Now()
		if n > 0 {
			readGap := now.Sub(lastReadAt)
			lastReadAt = now
			if readGap > windowMaxReadGap {
				windowMaxReadGap = readGap
			}
			if readGap >= xunfeiStreamSlowReadGap {
				windowSlowReads++
			}
			totalBytes += int64(n)
			totalChunks++
			windowBytes += int64(n)
			windowChunks++
			written, writeErr := w.Write(buf[:n])
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
		if now.Sub(lastStatsAt) >= xunfeiStreamStatsInterval {
			logXunfeiStreamStats(id, transport, false, totalBytes, totalChunks, windowBytes, windowChunks, startedAt, lastStatsAt, now, windowMaxReadGap, windowSlowReads)
			lastStatsAt = now
			windowBytes = 0
			windowChunks = 0
			windowSlowReads = 0
			windowMaxReadGap = 0
		}
		if readErr != nil {
			if readErr != io.EOF {
				copyErr = readErr
			}
			break
		}
	}
	logXunfeiStreamStats(id, transport, true, totalBytes, totalChunks, windowBytes, windowChunks, startedAt, lastStatsAt, time.Now(), windowMaxReadGap, windowSlowReads)
	if req.Context().Err() != nil {
		return
	}
	if copyErr != nil {
		logXunfeiStreamError(id, transport, copyErr)
	}
}

func logXunfeiStreamConnected(sessionID string, transport xunfeiStreamTransport) {
	log.Printf("Xunfei stream proxy connected session=%s transport=%s", sessionID, transport)
}

func logXunfeiStreamError(sessionID string, transport xunfeiStreamTransport, err error) {
	if err == nil {
		return
	}
	log.Printf("Xunfei stream proxy error session=%s transport=%s err=%v", sessionID, transport, err)
}

func logXunfeiStreamStats(sessionID string, transport xunfeiStreamTransport, final bool, totalBytes, totalChunks, windowBytes, windowChunks int64, startedAt, windowStartedAt, now time.Time, windowMaxReadGap time.Duration, windowSlowReads int) {
	elapsed := now.Sub(startedAt)
	windowElapsed := now.Sub(windowStartedAt)
	if elapsed <= 0 || (windowBytes == 0 && !final) {
		return
	}
	log.Printf(
		"Xunfei stream proxy stats session=%s transport=%s final=%t total_bytes=%d total_chunks=%d total_kbps=%d window_bytes=%d window_chunks=%d window_kbps=%d window_max_read_gap_ms=%d window_slow_reads=%d elapsed_ms=%d",
		sessionID,
		transport,
		final,
		totalBytes,
		totalChunks,
		xunfeiStreamKbps(totalBytes, elapsed),
		windowBytes,
		windowChunks,
		xunfeiStreamKbps(windowBytes, windowElapsed),
		windowMaxReadGap.Milliseconds(),
		windowSlowReads,
		elapsed.Milliseconds(),
	)
}

func xunfeiStreamKbps(bytes int64, elapsed time.Duration) int64 {
	if bytes <= 0 || elapsed <= 0 {
		return 0
	}
	return int64((float64(bytes) * 8 / elapsed.Seconds()) / 1000)
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
