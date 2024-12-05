FROM rust:1.79-alpine AS librespot-builder

RUN mkdir -p /librespot /output
WORKDIR /librespot

RUN apk add git musl-dev pkgconfig 
RUN git clone https://github.com/librespot-org/librespot.git .
RUN cargo build --release --no-default-features && \
    cp target/release/librespot /output/librespot

FROM golang:1.23.4-alpine3.20 AS app-builder

RUN apk add --no-cache gcc musl-dev opus-dev pkgconfig
ENV CGO_ENABLED=1

RUN mkdir -p /app /output
WORKDIR /app

COPY cmd /app/cmd
COPY internal /app/internal
COPY go.mod /app
COPY go.sum /app

RUN go build -o /output/spotify-discord /app/cmd/bot

FROM alpine:3.20.3

RUN apk add --no-cache opus sox

RUN mkdir -p /app
WORKDIR /app

COPY --from=librespot-builder /output/librespot /usr/bin/librespot
COPY --from=app-builder /output/spotify-discord /app/spotify-discord
COPY init-container.sh /app/init-container.sh

RUN mkfifo /tmp/librespot.out

ENTRYPOINT ["sh", "/app/init-container.sh"]
