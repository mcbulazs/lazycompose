FROM golang:1.27-alpine AS dev
RUN apk add --no-cache git && mkdir -p /cache && chmod 777 /cache
ENV GOCACHE=/cache/build GOMODCACHE=/cache/mod CGO_ENABLED=0
WORKDIR /src

FROM dev AS build
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN go build -trimpath -ldflags="-s -w" -o /out/lazycompose ./cmd/lazycompose

FROM scratch AS bin
COPY --from=build /out/lazycompose /lazycompose
