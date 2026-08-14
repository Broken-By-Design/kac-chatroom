# syntax=docker/dockerfile:1

FROM golang:1.26 AS builder
WORKDIR /src

COPY go.mod go.sum* ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags='-s -w' -o /out/chatroom .

FROM alpine:3.20
RUN apk add --no-cache ca-certificates python3 py3-pip \
 && pip install --break-system-packages --no-cache-dir yt-dlp
WORKDIR /app

COPY --from=builder /out/chatroom /usr/local/bin/chatroom
COPY --from=builder /src/templates ./templates
COPY --from=builder /src/static ./static
COPY --from=builder /src/jumpscare ./jumpscare
COPY --from=builder /src/Whitman ./Whitman
COPY --from=builder /src/ai_personality.txt ./ai_personality.txt
COPY --from=builder /src/badwords.txt ./badwords.txt

# Optional YouTube credentials for more reliable stream resolution.
# Cookie files are gitignored secrets: present locally when you have them,
# absent in clean CI checkouts. Mount the whole build context (always present)
# and copy each credential file only if it exists, so the build never fails
# for a missing secret. If you get a stale-cache "not found", rebuild with
# `docker buildx build --no-cache`.
RUN --mount=type=bind,src=.,target=/ctx,ro \
    cp /ctx/cookies_johndimi.txt /app/cookies_johndimi.txt 2>/dev/null || true
RUN --mount=type=bind,src=.,target=/ctx,ro \
    cp /ctx/cookies_johndrop.txt /app/cookies_johndrop.txt 2>/dev/null || true

EXPOSE 5000

ENV PORT=5000
COPY docker-entrypoint.sh /usr/local/bin/docker-entrypoint.sh
RUN chmod +x /usr/local/bin/docker-entrypoint.sh
ENTRYPOINT ["/usr/local/bin/docker-entrypoint.sh"]
