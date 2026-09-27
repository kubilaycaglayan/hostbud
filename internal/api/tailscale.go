package api

import (
	"net"
	"net/http"
	"net/netip"
	"strings"

	"hostbud/internal/auth"
	"hostbud/internal/tsauth"
)

const (
	tsVerifyError = "Your Tailscale identity couldn't be verified"
	tsDeniedError = "This tailnet user isn't allowed"
)

func tailscaleIdentity(cfg Config, next http.Handler) http.Handler {
	if strings.TrimSpace(cfg.AllowedTSUsers) == "" {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/health" {
			next.ServeHTTP(w, r)
			return
		}
		via := "domain"
		if peer, ok := requestPeer(r); ok && trustedPeer(peer, cfg.TrustedProxies) && r.Header.Get("X-Hostbud-Via") == "local" {
			via = "local"
		}
		if via == "local" {
			next.ServeHTTP(w, r)
			return
		}
		ip := auth.ClientIP(r, cfg.TrustedProxies)
		if cfg.TSLogin == nil {
			cfg.Log.Info("tailscale identity rejected", "reason", "whois_unavailable")
			writeTSForbidden(w, r, tsVerifyError, "Check that tailscaled is running on the host.")
			return
		}
		login, err := cfg.TSLogin(r.Context(), ip)
		if err != nil {
			cfg.Log.Info("tailscale identity rejected", "reason", "whois_error")
			writeTSForbidden(w, r, tsVerifyError, "Check that tailscaled is running on the host.")
			return
		}
		if !tsauth.Allowed(cfg.AllowedTSUsers, login) {
			cfg.Log.Info("tailscale identity rejected", "reason", "not_allowed")
			writeTSForbidden(w, r, tsDeniedError, "Ask the owner to add your Tailscale login to HOSTBUD_ALLOWED_TS_USERS.")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func requestPeer(r *http.Request) (netip.Addr, bool) {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	a, err := netip.ParseAddr(host)
	if err != nil {
		return netip.Addr{}, false
	}
	return a.Unmap(), true
}

func trustedPeer(peer netip.Addr, proxies []netip.Prefix) bool {
	for _, p := range proxies {
		if p.Contains(peer) {
			return true
		}
	}
	return false
}

func writeTSForbidden(w http.ResponseWriter, r *http.Request, message, hint string) {
	if strings.HasPrefix(r.URL.Path, "/api/") || strings.HasPrefix(r.URL.Path, "/ws/") {
		writeError(w, 403, message, hint)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(403)
	_, _ = w.Write([]byte("<!doctype html><html><head><meta charset=\"utf-8\"><title>Access denied</title></head><body><h1>" + message + "</h1><p>" + hint + "</p></body></html>"))
}
