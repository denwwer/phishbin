FROM golang:alpine AS builder

RUN apk add --no-cache make

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY src ./src
COPY main.go ./

COPY Makefile ./

RUN make build

FROM scratch

COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=builder /app/phishbin /phishbin

# default ports
EXPOSE 4535

# data dir
VOLUME ["/data"]

ENTRYPOINT ["/phishbin"]
CMD ["-c", "/data"]
