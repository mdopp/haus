FROM golang:1.27-alpine AS build
WORKDIR /src
COPY go.mod ./
COPY . .
RUN CGO_ENABLED=0 go build -ldflags "-s -w -X main.Version=${VERSION:-dev}" -o /haus ./cmd/haus
FROM scratch
COPY --from=build /haus /haus
EXPOSE 8650
ENTRYPOINT ["/haus"]
