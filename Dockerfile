FROM golang:1.25-alpine AS builder

RUN apk add --no-cache ca-certificates git tzdata

WORKDIR /src

COPY go.mod ./

COPY . .

RUN go mod download all

ARG VERSION="Development version"
RUN printf '%s' "${VERSION}" > VERSION

RUN CGO_ENABLED=0 go build -mod=mod -trimpath -o /out/embytool ./cmd/embytool

FROM alpine:3.20

RUN apk add --no-cache ca-certificates tzdata

WORKDIR /app

RUN mkdir -p /data

COPY --from=builder /out/embytool /usr/local/bin/embytool
COPY --from=builder /src/VERSION ./VERSION
COPY --from=builder /src/static ./static
COPY --from=builder /src/fonts ./fonts
COPY --from=builder /src/images ./images

ENV TZ=Asia/Shanghai

EXPOSE 8055

ENTRYPOINT ["/usr/local/bin/embytool"]
