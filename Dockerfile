FROM golang:1.25.0-alpine3.22 AS build-stage

WORKDIR /app

COPY go.mod go.sum ./

RUN --mount=type=cache,target=/root/go-build go mod download -x

COPY . .

ENV CGO_ENABLED=0 GOOS=linux GOARCH=amd64

RUN go test -v ./... && go build -ldflags="-s -w" -o ./smallci main.go

FROM alpine:3.21.5 AS build-release-stage

COPY --from=build-stage /app/smallci /usr/local/bin/smallci

RUN chmod +x /usr/local/bin/smallci

ENTRYPOINT ["/usr/local/bin/smallci"]
