FROM golang:1.27.2-alpine@sha256:f92b6ef800e499660581efdabdf25d9d817a9d124eaf900924f0504e7e27e12d AS build

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
