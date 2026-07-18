FROM golang:1.25-alpine AS builder
WORKDIR /app

COPY go.work go.work.sum ./

# Nur die go.mod/go.sum der relevanten Module für Layer-Caching
COPY auth/go.mod auth/go.mod
COPY auth/go.sum auth/go.sum
COPY social/go.mod social/go.mod
COPY social/go.sum social/go.sum
COPY travel-info/go.mod travel-info/go.mod
COPY travel-info/go.sum travel-info/go.sum
COPY shared/authclient/go.mod shared/authclient/go.mod
COPY shared/authclient/go.sum shared/authclient/go.sum
COPY shared/email/go.mod shared/email/go.mod
COPY shared/email/go.sum shared/email/go.sum
COPY shared/middleware/go.mod shared/middleware/go.mod
COPY shared/middleware/go.sum shared/middleware/go.sum
COPY shared/userclient/go.mod shared/userclient/go.mod
COPY shared/userclient/go.sum shared/userclient/go.sum
COPY shared/firebaseclient/go.mod shared/firebaseclient/go.mod
COPY shared/firebaseclient/go.sum shared/firebaseclient/go.sum
COPY shared/tenantdb/go.mod shared/tenantdb/go.mod
COPY shared/tenantdb/go.sum shared/tenantdb/go.sum
COPY shared/db/go.mod shared/db/go.mod
COPY shared/db/go.sum shared/db/go.sum
COPY shared/otel/go.mod shared/otel/go.mod
COPY shared/otel/go.sum shared/otel/go.sum
COPY users/go.mod users/go.mod
COPY users/go.sum users/go.sum
COPY trips/go.mod trips/go.mod
COPY trips/go.sum trips/go.sum
COPY feed/go.mod feed/go.mod
COPY feed/go.sum feed/go.sum
COPY newsletter/go.mod newsletter/go.mod
COPY newsletter/go.sum newsletter/go.sum

RUN go work sync

# Source Code
COPY shared/ shared/
COPY users/ users/

# Aus dem users-Verzeichnis bauen
RUN CGO_ENABLED=0 GOOS=linux go build -o /bin/users ./users/cmd/api

# Runtime stage
FROM alpine:3.19
RUN addgroup -S app && adduser -S -G app app
USER app
WORKDIR /app

COPY --from=builder /bin/users /bin/users

HEALTHCHECK --interval=30s --timeout=10s --start-period=5s --retries=3 \
    CMD wget --no-verbose --tries=1 --spider http://localhost:8001/health || exit 1

EXPOSE 8001
CMD ["/bin/users"]