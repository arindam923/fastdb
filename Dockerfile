FROM golang:1.24-alpine AS builder

RUN apk add --no-cache git

WORKDIR /app

COPY server/go.mod server/go.sum ./
RUN go mod download

COPY server/*.go ./
RUN CGO_ENABLED=0 GOOS=linux go build -o flashdb

FROM alpine:latest

RUN apk add --no-cache ca-certificates

WORKDIR /app

COPY --from=builder /app/flashdb .

EXPOSE 8080

ENV PORT=8080
ENV PERSIST_INTERVAL=5s

CMD ["./flashdb"]
