FROM golang:1.22-alpine AS builder

WORKDIR /app

COPY go.mod ./
RUN go mod download

COPY main.go ./
COPY internal/ ./internal/

RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /app/server .

FROM alpine:3.20 AS runner

WORKDIR /app

ENV PORT=3000

COPY --from=builder /app/server ./server

EXPOSE 3000

CMD ["./server"]
