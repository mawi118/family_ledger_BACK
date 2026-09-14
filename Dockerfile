FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /out/family_ledger ./cmd/family_ledger

FROM alpine:3.21
RUN apk add --no-cache ca-certificates
WORKDIR /app
COPY --from=build /out/family_ledger ./family_ledger
EXPOSE 5050
ENTRYPOINT ["./family_ledger"]