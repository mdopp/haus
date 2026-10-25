FROM golang:1.27-alpine AS build
RUN apk add --no-cache ca-certificates
WORKDIR /src
COPY go.mod ./
COPY . .
RUN CGO_ENABLED=0 go build -ldflags "-s -w -X main.Version=${VERSION:-dev}" -o /haus ./cmd/haus
FROM scratch
# the roots a page in the internet is checked against (Aus Link merken,
# Share → Haus, the podcasts' Apple lookup): without them every https
# address was "Das Zertifikat der Seite stimmt nicht"
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=build /haus /haus
EXPOSE 8650
ENTRYPOINT ["/haus"]
