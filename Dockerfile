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
COPY --from=builder /app/inboxbuffer /inboxbuffer

ENV IB_SERVER_HOST="0.0.0.0"

# default ports
EXPOSE 1081
EXPOSE 1025
EXPOSE 1082

# data and config dir
VOLUME ["/data"]

ENTRYPOINT ["/inboxbuffer"]
CMD ["-c", "/data"]
