# The hosted profile image: one static binary, one process. TLS terminates at
# the platform edge; the service answers plain HTTP on $PORT and requires
# TEAROOM_DSN and TEAROOM_OWNER_TOKEN (see docs/operations/serving.md).
FROM golang:1.22 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /tearoom ./cmd/tearoom

FROM alpine:3.20
# ca-certificates: outbound PostgreSQL connections (e.g. Neon) verify TLS.
RUN apk add --no-cache ca-certificates
COPY --from=build /tearoom /tearoom
USER nobody
CMD sh -c 'exec /tearoom server start --address "0.0.0.0:${PORT:-8787}"'
