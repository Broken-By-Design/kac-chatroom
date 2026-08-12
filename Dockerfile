# syntax=docker/dockerfile:1

FROM golang:1.26 AS builder
WORKDIR /src

COPY go.mod go.sum* ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags='-s -w' -o /out/chatroom .

FROM alpine:3.20
RUN apk add --no-cache ca-certificates
WORKDIR /app

COPY --from=builder /out/chatroom /usr/local/bin/chatroom
COPY --from=builder /src/templates ./templates
COPY --from=builder /src/static ./static
COPY --from=builder /src/jumpscare ./jumpscare
COPY --from=builder /src/Whitman ./Whitman
COPY --from=builder /src/ai_personality.txt ./ai_personality.txt
COPY --from=builder /src/badwords.txt ./badwords.txt

EXPOSE 5000

ENV PORT=5000
ENTRYPOINT ["/usr/local/bin/chatroom"]
