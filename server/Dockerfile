# syntax=docker/dockerfile:1
# Go version follows go.mod (dependencies currently require 1.26). [CẦN XÁC NHẬN: D-12]
FROM golang:1.26 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/server ./cmd/server

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/server /app/server
USER nonroot:nonroot
EXPOSE 8080 9100
ENTRYPOINT ["/app/server"]
