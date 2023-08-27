FROM golang:1.21.0-alpine as builder
WORKDIR /app
COPY main.go .
COPY go.mod .
COPY go.sum .
RUN go mod download
RUN go build -o sse-go-fiber ./main.go

FROM alpine:latest AS runner
WORKDIR /app
COPY --from=builder /app/sse-go-fiber .
EXPOSE 8099
ENTRYPOINT ["./sse-go-fiber"]