FROM golang:1.23-alpine AS builder

RUN apk add --no-cache ca-certificates git tzdata

WORKDIR /src

COPY go.mod ./
RUN go mod download

COPY . .

ARG VERSION="Development version"
RUN printf '%s' "${VERSION}" > app/VERSION

RUN CGO_ENABLED=0 go build -trimpath -o /out/embytool ./cmd/embytool

FROM alpine:3.20

RUN apk add --no-cache ca-certificates tzdata

WORKDIR /app

RUN mkdir -p /data

COPY --from=builder /out/embytool /usr/local/bin/embytool
COPY --from=builder /src/app/VERSION ./app/VERSION
COPY --from=builder /src/app/static ./app/static
COPY --from=builder /src/fonts ./fonts
COPY --from=builder /src/images ./images

ENV TZ=Asia/Shanghai

EXPOSE 8055

ENTRYPOINT ["/usr/local/bin/embytool"]
