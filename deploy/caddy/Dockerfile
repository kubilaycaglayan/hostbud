# hostbud-caddy: stock Caddy plus the Cloudflare DNS provider, for the TLS
# certificate of ${HOSTBUD_DOMAIN} via ACME DNS-01 (the domain resolves to a
# Tailscale IP, so HTTP-01/TLS-ALPN-01 challenges can't reach it).
# Keep both Caddy tags on the same version.
FROM --platform=linux/amd64 caddy:2.11.4-builder AS build
RUN xcaddy build --with github.com/caddy-dns/cloudflare@v0.2.4

FROM --platform=linux/amd64 caddy:2.11.4
COPY --from=build /usr/bin/caddy /usr/bin/caddy
