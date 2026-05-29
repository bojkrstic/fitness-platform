FROM golang:1.25-bookworm AS builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -buildvcs=false -o /out/fitness-platform .

FROM gcr.io/distroless/base-debian12

WORKDIR /app
COPY --from=builder /out/fitness-platform /app/fitness-platform
COPY templates /app/templates

EXPOSE 8080

CMD ["/app/fitness-platform"]
