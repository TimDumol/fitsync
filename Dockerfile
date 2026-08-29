FROM golang:1.27.0-alpine@sha256:4c9fe60190a2a3350ddc51de80d0224b8a6698d12bdfc999fee45ea9d6c46dbc AS build

WORKDIR /src
COPY go.mod go.sum ./
COPY cmd ./cmd
COPY internal ./internal
COPY migrations ./migrations
RUN go mod download
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags='-s -w' -o /fitnessd ./cmd/fitnessd

FROM alpine:3.21.3@sha256:a8560b36e8b8210634f77d9f7f9efd7ffa463e380b75e2e74aff4511df3ef88c
RUN apk add --no-cache ca-certificates && addgroup -S -g 65532 fitnessd && adduser -S -D -H -u 65532 -G fitnessd fitnessd
COPY --from=build /fitnessd /usr/local/bin/fitnessd
USER 65532:65532
ENTRYPOINT ["/usr/local/bin/fitnessd"]
