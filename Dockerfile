# Viya container image.
#
# Build from the repository root after placing the target-architecture
# binaries in bin/ (e.g. cp bin/viya-linux-amd64 bin/viya):
#
#   docker build --build-arg ARCH=amd64 -t viya-proxy .
#
# Or, with the release archive extracted into ./viya-linux-amd64:
#
#   docker build --build-arg ARCH=amd64 -t viya-proxy -f Dockerfile viya-linux-amd64

FROM alpine:3.20

# ca-certificates for ACME/HTTPS, tzdata for correct log timestamps
RUN apk add --no-cache ca-certificates tzdata

ARG ARCH=amd64
COPY bin/viya-linux-${ARCH} /opt/viya/bin/viya
COPY bin/caddy-linux-${ARCH} /opt/viya/bin/caddy
RUN chmod +x /opt/viya/bin/viya /opt/viya/bin/caddy

ENV VIYA_LISTEN=0.0.0.0:8181 \
    VIYA_DATA_DIR=/var/lib/viya \
    VIYA_MASTER_KEY_FILE=/etc/viya/secrets/master.key \
    VIYA_CADDY_BIN=/opt/viya/bin/caddy \
    VIYA_CADDY_ADMIN=http://127.0.0.1:2019 \
    HOME=/var/lib/viya

WORKDIR /var/lib/viya

VOLUME ["/var/lib/viya", "/etc/viya/secrets"]

# 8181 admin UI/API, 80/443 for HTTP/HTTPS sites
EXPOSE 8181 80 443

ENTRYPOINT ["/opt/viya/bin/viya"]
